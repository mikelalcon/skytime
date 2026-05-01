package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/validator"
)

// errSilent is the sentinel returned by validate's RunE on validation
// failure. SilenceErrors is true on root, so cobra does NOT re-print
// this — but it DOES exit with a non-zero status code, which is the
// D4-18 contract: "renderer owns output; cobra owns exit status".
var errSilent = errors.New("validation failed")

// newValidateCommand returns the skytime validate subcommand.
//
// Surface:
//   - CLI-01: runs static validation and exits with structured errors.
//   - VAL-03: errors are formatted "<file>:<line>:<col> [flow > step > action]: <msg>"
//     (the renderer does the work; this RunE just routes errors to it).
//   - D4-16: unknown-extension errors append a build-your-own-CLI hint.
//
// Args: cobra.ExactArgs(1) — the .star file path. Cobra surfaces the
// arg-count error via the renderer (no special-casing here).
func newValidateCommand(cfg *config) *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file.star>",
		Short: "Statically validate a .star flow file",
		Long: "Validates a Starlark flow file against registered extension schemas, " +
			"lambda free-vars-reference-state checks, and DSL invariants. " +
			"Exits non-zero on any validation failure; --debug reveals Go internals.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := args[0]
			errs := validator.Validate(file,
				validator.WithExtensions(cfg.exts...),
				validator.WithCredentialHandler(cfg.credHandler),
			)
			if len(errs) == 0 {
				return nil
			}

			stderr := cmd.ErrOrStderr()
			renderErrors(stderr, errs, cfg.debug)
			appendUnknownExtensionHint(stderr, errs)

			return errSilent
		},
	}
}

// appendUnknownExtensionHint scans errs for *dag.ParseError messages
// matching the unknown-extension pattern and prints the D4-16 hint.
//
// The match is heuristic on the parser's wrapped-starlark-error wording:
//   - "undefined: <name>" — Starlark resolver error (the typical surface
//     when a .star file references a name not in predeclared globals)
//   - "unknown extension" — a future parser-emitted message; we accept
//     either spelling so this code is forward-compatible.
//
// Either pattern means the user referenced an extension that wasn't
// registered with this binary's pkg/cli.NewRootCommand call.
func appendUnknownExtensionHint(out io.Writer, errs []error) {
	for _, e := range errs {
		var pe *dag.ParseError
		if !errors.As(e, &pe) {
			continue
		}
		m := strings.ToLower(pe.Msg)
		if strings.Contains(m, "undefined:") || strings.Contains(m, "unknown extension") {
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "hint: This binary doesn't have the referenced extension registered.")
			fmt.Fprintln(out, "      Build a custom Skytime CLI binary that registers your extensions.")
			fmt.Fprintln(out, "      See: docs/cli-binary.md")
			return
		}
	}
}
