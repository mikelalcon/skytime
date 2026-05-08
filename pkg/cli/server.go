package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelalcon/skytime/pkg/worker"
)

// testDrainHook is the package-private test seam (per § Pitfall 9 of
// 07-RESEARCH.md). Production: nil. Tests assign to observe drain
// progression without subprocess plumbing.
//
// Stage names (LOCKED — tests pin against these strings): worker_started,
// signal_received, drain_started, drain_completed, drain_timeout,
// drain_forced.
//
// Phase 7.1 will reuse the same hook surface once the worker.WithSDKFactory
// Option lands, allowing the currently-skipped TestServerCmd_DrainOnSIGTERM
// / TestServerCmd_DrainTimeoutExpiry / TestServerCmd_SecondSignalForceExit
// tests to drop their t.Skip and exercise the full signal loop.
var testDrainHook func(stage string)

// testForceExit is the package-private test seam for the second-signal
// os.Exit(1) escalation path. Production calls os.Exit; tests override
// to record the call without terminating the test process.
var testForceExit = func(code int) { os.Exit(code) }

// hookStage is a nil-safe wrapper around testDrainHook.
func hookStage(stage string) {
	if testDrainHook != nil {
		testDrainHook(stage)
	}
}

// newServerCommand returns the skytime server subcommand. SERVER-01..03.
//
// Long-running Skytime worker against a configured Temporal cluster.
// Drains in-flight workflows up to --drain-timeout on first SIGINT/SIGTERM
// and forces immediate exit on a second signal. Phase 7 ships the worker
// + signal handling shell; Phase 7.1 adds the HTTP receiver that mounts
// trigger handlers.
func newServerCommand(cfg *config) *cobra.Command {
	var (
		rootdir      string
		taskQueue    string
		addr         string
		credfilePath string
		drainTimeout time.Duration
		jsonLog      bool
	)

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run a long-lived Skytime worker (drain-on-SIGTERM)",
		Long: "Boots a Temporal worker against the configured task queue, registers " +
			"flows + triggers from --rootdir, and stays up until SIGTERM/SIGINT. " +
			"Drains in-flight workflows up to --drain-timeout (default 30s) on " +
			"first signal; second signal forces immediate exit.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Range validation BEFORE side effects (per § Pitfall 6
			//    of 07-RESEARCH.md — pflag.Duration accepts 0s and
			//    negatives syntactically; reject explicitly here so the
			//    error surfaces before we connect to Temporal or boot
			//    the worker).
			if drainTimeout < minDrainTimeout {
				return fmt.Errorf("--drain-timeout must be at least %s; got %s (use 30s default if unsure)", minDrainTimeout, drainTimeout)
			}

			// 2. Switch slog handler for --json-log per § Pitfall 7.
			//    The server uses plain slog handlers (charm-log or JSON),
			//    NOT the buildRoutedSlogLogger pattern from `skytime run`
			//    — server startup events are not flow events.
			logger := setupServerLogging(cfg.debug, jsonLog)
			if drainTimeout > maxDrainTimeout {
				logger.Warn("drain-timeout exceeds 1h; large drain windows may delay rolling deploys",
					"value", drainTimeout)
			}

			// 3. credfile sanity check (D-07-19): a --credfile flag
			//    without a binary-side credential handler is always a
			//    misconfiguration; surface a friendly error pointing at
			//    cli.WithCredentialHandler.
			if credfilePath != "" && cfg.credHandler == nil {
				return fmt.Errorf("--credfile=%s requires the binary to be built with cli.WithCredentialHandler (see docs/cli-binary.md); current binary has no credential handler wired", credfilePath)
			}

			// 4. Connect Temporal via D4-08 variant routing. Reuses
			//    pkg/cli/connect.go::connectClient so cloud / mTLS /
			//    dev variants behave identically across run + server.
			c, err := connectClient(cfg)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "connect: %s\n", err.Error())
				return errSilent
			}
			defer c.Close()

			// 5. Build worker with WorkerStopTimeout threaded through
			//    so worker.Stop() blocks for at most --drain-timeout.
			w, err := worker.NewWorker(c, worker.WorkerOptions{
				RootDir:           rootdir,
				TaskQueue:         taskQueue,
				Extensions:        cfg.exts,
				CredentialHandler: cfg.credHandler,
				Logger:            cfg.sdkLogger,
				WorkerStopTimeout: drainTimeout,
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "worker init: %s\n", err.Error())
				return errSilent
			}

			// 6. Sorted startup banner BEFORE Start so operators see
			//    what's about to come online. Three slog records:
			//    "starting server", "registered flows", "registered
			//    triggers".
			printStartupBanner(logger, w, rootdir, taskQueue, addr)
			if cmd.Flags().Changed("addr") {
				logger.Warn("note: --addr has no effect until Phase 7.1 ships the HTTP receiver",
					"addr", addr)
			}

			// 7. Start (non-blocking per D3-18).
			if err := w.Start(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "worker start: %s\n", err.Error())
				return errSilent
			}
			hookStage("worker_started")
			logger.Info("worker started; SIGTERM/SIGINT to drain", "drain-timeout", drainTimeout)

			// 8. Two-signal escalation via signal.Notify (NOT
			//    NotifyContext per § Pitfall 5 — NotifyContext is
			//    single-shot, but we need to receive a SECOND signal
			//    while drain is in-flight to escalate to forced exit).
			sigCh := make(chan os.Signal, 2)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)

			<-sigCh
			hookStage("signal_received")
			logger.Info("server draining; second SIGINT/SIGTERM forces immediate exit")

			done := make(chan struct{})
			var stopOnce sync.Once
			go func() {
				stopOnce.Do(func() {
					hookStage("drain_started")
					w.Stop() // SDK Stop blocks up to WorkerStopTimeout.
					close(done)
				})
			}()

			drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
			defer cancel()

			select {
			case <-done:
				hookStage("drain_completed")
				logger.Info("drain complete")
				return nil
			case <-sigCh:
				hookStage("drain_forced")
				logger.Error("drain interrupted by second signal; forcing exit (workflows resume on next worker start from event history)")
				testForceExit(1)
				return nil // unreachable in production
			case <-drainCtx.Done():
				hookStage("drain_timeout")
				logger.Error("drain-timeout exceeded; restart resumes from event history",
					"timeout", drainTimeout)
				return errSilent
			}
		},
	}

	cmd.Flags().StringVar(&rootdir, "rootdir", "", "directory containing .star files (required)")
	cmd.Flags().StringVar(&taskQueue, "task-queue", "skytime", "Temporal task queue")
	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "HTTP listener address (Phase 7.1+; ignored in Phase 7)")
	cmd.Flags().StringVar(&credfilePath, "credfile", "", "credential file path (Phase 7.4+; rejected when binary has no credential handler)")
	cmd.Flags().DurationVar(&drainTimeout, "drain-timeout", defaultDrainTimeout,
		"max time to wait for in-flight workflows to complete on SIGTERM/SIGINT (1s..1h)")
	cmd.Flags().BoolVar(&jsonLog, "json-log", false, "emit logs as JSON instead of charm-log Bazel-style")
	_ = cmd.MarkFlagRequired("rootdir")
	return cmd
}

// printStartupBanner emits the SERVER-03 sorted banner — three slog
// records:
//
//	"starting server"        (rootdir, task-queue, addr)
//	"registered flows"       (count, flows []string sorted)
//	"registered triggers"    (count, triggers []map[string]string)
//
// Trigger entries are shaped {source: <kind>, flow: <flow-name>}; sort
// order is supplied by w.Triggers().All() which Plan 04's Freeze sorts
// by (Source.Kind, FlowName, Pos).
func printStartupBanner(logger *slog.Logger, w *worker.Worker, rootdir, taskQueue, addr string) {
	logger.Info("starting server",
		"rootdir", rootdir,
		"task-queue", taskQueue,
		"addr", addr)

	flowNames := w.FlowNames()
	out := make([]string, len(flowNames))
	copy(out, flowNames)
	sort.Strings(out)
	logger.Info("registered flows", "count", len(out), "flows", out)

	trigs := w.Triggers().All() // already sorted by Plan 04's Freeze.
	triggerLines := make([]map[string]string, len(trigs))
	for i, t := range trigs {
		triggerLines[i] = map[string]string{
			"source": t.Source.Kind(),
			"flow":   t.FlowName,
		}
	}
	logger.Info("registered triggers", "count", len(trigs), "triggers", triggerLines)
}
