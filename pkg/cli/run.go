package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	sdkclient "go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/cli/server/web/flowlaunch"
	"github.com/mikelalcon/skytime/pkg/validator"
	"github.com/mikelalcon/skytime/pkg/worker"
)

// newRunCommand returns the skytime run subcommand.
//
// CLI-02: parses, validates, fires SkytimeWorkflow, blocks for result.
// D4-05: embedded transient worker per RESEARCH §Pattern 4 recipe.
// D4-07: --input JSON validated through validator.Validate (same source
//
//	of truth as `skytime validate`).
//
// RunE order (D4-05 eight-step recipe):
//  1. Static validate via validator.Validate — surface errors via the
//     renderer and return errSilent on failure.
//  2. Parse --input JSON into map[string]any; bad JSON → friendly error
//     before any client connection.
//  3. connectClient(cfg) — variant routing per D4-08.
//  4. worker.NewWorker against filepath.Dir(file) as RootDir.
//  5. Resolve content_hash from the worker's frozen registry.
//  6. ExecuteWorkflow with WorkflowInput{FlowName, ContentHash, InitState}.
//  7. run.Get blocks on completion.
//  8. On context.Canceled (Ctrl-C): print "interrupted; workflow continues
//     on Temporal as runID=X" and return errSilent (cobra exits non-zero).
//     v1 accepts exit 1 vs 130 — promoting to 130 requires os.Exit from
//     main, which cobra patterns discourage.
func newRunCommand(cfg *config) *cobra.Command {
	var flowName string
	var inputJSON string

	cmd := &cobra.Command{
		Use:   "run <file.star>",
		Short: "Trigger a workflow on a configured Temporal cluster",
		Long: "Validates the .star file, connects to Temporal (variant chosen by which flags " +
			"are present per D4-08), boots an embedded worker against the file's directory, " +
			"and triggers SkytimeWorkflow with the parsed input. Blocks for the result.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := args[0]
			ctx := cmd.Context()

			// 1. Static validate first (D4-07 — same checks as `validate`).
			if errs := validator.Validate(file,
				validator.WithExtensions(cfg.exts...),
				validator.WithCredentialHandler(cfg.credHandler),
			); len(errs) > 0 {
				renderErrors(cmd.ErrOrStderr(), errs, cfg.debug)
				return errSilent
			}

			// 2. Parse --input JSON.
			initState, err := parseInputJSON(inputJSON)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "invalid --input JSON: %s\n", err.Error())
				return errSilent
			}

			// Quick 260502-guu Fix B: REPLACE cfg.sdkLogger with a
			// routedSlog whose handler is a *progressHandler in front
			// of cfg.sdkLogger's existing handler. This is the
			// load-bearing wiring step:
			//   - SDK client + worker.GetLogger emit through THIS
			//     logger (the SDK worker inherits the client's
			//     Logger; the client gets cfg.sdkLogger).
			//   - The progressHandler intercepts `event=*` records
			//     and renders them Bazel-style on stderr (NOT stdout
			//     — stdout is reserved for the JSON workflow result).
			//   - Other records pass through cfg.sdkLogger's wrapped
			//     handler (charm-log when --verbose, discard otherwise).
			//
			// Phase 04.1-06 Task 3: capture the *progressHandler so
			// the live-block render goroutine (D4.1-17) drains
			// cleanly on flow completion. The deferred Close is a
			// no-op for static-mode handlers (verbose / non-TTY).
			routedLogger, progHandler := buildRoutedSlogLoggerWithHandle(cfg, cmd.ErrOrStderr())
			cfg.sdkLogger = routedLogger
			defer progHandler.Close()

			// 3. Connect — variant routing per D4-08. (uses cfg.sdkLogger)
			c, err := connectClient(cfg)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "connect: %s\n", err.Error())
				return errSilent
			}
			defer c.Close()

			// 4. Embedded worker against the file's directory.
			//    NewWorker requires a CredentialHandler — surface a
			//    friendly error if the binary was constructed without
			//    one.
			if cfg.credHandler == nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "skytime run requires a CredentialHandler "+
					"(supply via cli.WithCredentialHandler when constructing the binary)")
				return errSilent
			}
			w, err := worker.NewWorker(c, worker.WorkerOptions{
				RootDir:           filepath.Dir(file),
				Extensions:        cfg.exts,
				CredentialHandler: cfg.credHandler,
				Logger:            cfg.sdkLogger,
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "worker init: %s\n", err.Error())
				return errSilent
			}
			if err := w.Start(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "worker start: %s\n", err.Error())
				return errSilent
			}
			defer w.Stop()

			// 5. Resolve content hash from the worker's frozen registry.
			contentHash, ok := w.Registry().ContentHashFor(flowName)
			if !ok {
				fmt.Fprintf(cmd.ErrOrStderr(), "flow %q not found in %s\n", flowName, filepath.Dir(file))
				return errSilent
			}

			// 6. Trigger the workflow. Worker default task queue applies
			//    when the flow does not declare one (D3-19 hierarchy).
			//    workflowInput shape comes from flowlaunch.BuildWorkflowInput
			//    (UI-04 single seam). The ExecuteWorkflow call stays here
			//    because skytime run is synchronous and needs the
			//    *WorkflowRun handle for run.Get below —
			//    flowlaunch.Execute returns the ID only.
			workflowInput := flowlaunch.BuildWorkflowInput(flowName, contentHash, initState)
			run, err := c.ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{
				TaskQueue: "skytime",
			}, "SkytimeWorkflow", workflowInput)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "execute workflow: %s\n", err.Error())
				return errSilent
			}

			// 7. Block on completion.
			var result map[string]any
			if err := run.Get(ctx, &result); err != nil {
				if errors.Is(err, context.Canceled) {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"interrupted; workflow continues on Temporal as runID=%s\n",
						run.GetRunID())
					return errSilent
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "workflow error: %s\n", err.Error())
				return errSilent
			}

			// 8. Print result on stdout.
			out, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "marshal result: %s\n", err.Error())
				return errSilent
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}

	cmd.Flags().StringVar(&flowName, "flow", "", "name of the flow to execute (required)")
	cmd.Flags().StringVar(&inputJSON, "input", "{}", "JSON-encoded workflow input (default: {})")
	_ = cmd.MarkFlagRequired("flow")

	return cmd
}

// parseInputJSON decodes the --input string into a map[string]any.
// Empty string is treated as "{}". A nil map (e.g., from `null` input)
// is normalized to an empty map so downstream consumers (validator,
// interpreter) never have to nil-check.
func parseInputJSON(s string) (map[string]any, error) {
	if s == "" {
		s = "{}"
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}
