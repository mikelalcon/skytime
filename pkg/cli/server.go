package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelalcon/skytime/pkg/extension/receiver"
	"github.com/mikelalcon/skytime/pkg/worker"
)

// testDrainHook is the package-private test seam (per § Pitfall 9 of
// 07-RESEARCH.md). Production: nil. Tests assign to observe drain
// progression without subprocess plumbing.
//
// Stage names (LOCKED — tests pin against these strings):
// worker_started, listener_started, signal_received,
// listener_shutdown_started, listener_shutdown_complete, drain_started,
// drain_completed, drain_timeout, drain_forced.
//
// Phase 7 Plan 05 locked the original 6: worker_started, signal_received,
// drain_started, drain_completed, drain_timeout, drain_forced. Phase 7.1
// Plan 06 adds 3 more for the HTTP listener lifecycle: listener_started,
// listener_shutdown_started, listener_shutdown_complete.
var testDrainHook func(stage string)

// testForceExit is the package-private test seam for the second-signal
// os.Exit(1) escalation path. Production calls os.Exit; tests override
// to record the call without terminating the test process.
var testForceExit = func(code int) { os.Exit(code) }

// testWorkerOptions is the package-private test seam for injecting
// worker.Option values into NewWorker calls during black-box tests
// (D-7.1-13). Production: nil. Tests assign to inject
// worker.WithSDKFactory(fakeFactory) so the signal-loop end-to-end
// tests (TestServerCmd_DrainOnSIGTERM / DrainTimeoutExpiry /
// SecondSignalForceExit) can run without a real Temporal SDK worker.
//
// Phase 7 Plan 05 named these tests with their final targets so this
// seam wiring is the only late addition.
var testWorkerOptions []worker.Option

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
			//    testWorkerOptions is nil in production; tests inject
			//    worker.WithSDKFactory(fakeFactory) here (D-7.1-13).
			w, err := worker.NewWorker(c, worker.WorkerOptions{
				RootDir:           rootdir,
				TaskQueue:         taskQueue,
				Extensions:        cfg.exts,
				CredentialHandler: cfg.credHandler,
				Logger:            cfg.sdkLogger,
				WorkerStopTimeout: drainTimeout,
			}, testWorkerOptions...)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "worker init: %s\n", err.Error())
				return errSilent
			}

			// 6. Sorted startup banner BEFORE Start so operators see
			//    what's about to come online. Three slog records:
			//    "starting server", "registered flows", "registered
			//    triggers". Trigger entries now include mount paths
			//    for HTTP-shaped sources (D-7.1 §Reusable Assets).
			printStartupBanner(logger, w, rootdir, taskQueue, addr)

			// 7. Two-signal escalation via signal.Notify (NOT
			//    NotifyContext per § Pitfall 5 — NotifyContext is
			//    single-shot, but we need to receive a SECOND signal
			//    while drain is in-flight to escalate to forced exit).
			//    Installed BEFORE hookStage("worker_started") so the
			//    Phase 7.1 black-box drain tests can use that hook
			//    stage as a deterministic sync point — the handler
			//    must be live before any test-side syscall.Kill fires.
			sigCh := make(chan os.Signal, 2)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)

			// 8. D-7.1-10 boot order: worker first (poll task queue),
			//    THEN HTTP listener. K8s readinessProbe = TCP connect
			//    to --addr is honest only if the listener binding
			//    implies a polling worker.
			if err := w.Start(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "worker start: %s\n", err.Error())
				return errSilent
			}
			hookStage("worker_started")

			// 9. Mount HTTP receiver. receiver.Mount (Plan 04) walks
			//    worker.Triggers().All(), groups by (kind, path,
			//    method), and registers ONE handler per path that
			//    method-dispatches internally. Plan 04b's per-request
			//    pipeline is fully wired inside makeHandler.
			mux := http.NewServeMux()
			receiver.Mount(mux, w, receiver.Deps{
				Client:            c,
				CredentialHandler: cfg.credHandler,
				TaskQueue:         taskQueue,
				Logger:            logger,
			})

			// 10. Pitfall 9: pre-bind the listener synchronously so
			//     "address already in use" surfaces inline (BEFORE the
			//     listener goroutine swallows it). Tests that race two
			//     processes on the same port get a deterministic error
			//     here, not a delayed log line from a background
			//     goroutine.
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "listener bind %s: %s\n", addr, err.Error())
				return errSilent
			}

			// 11. D-7.1-12 HTTP server defaults. Body-size limit (25MB)
			//     is enforced per-handler via http.MaxBytesReader inside
			//     receiver.makeHandler (Plan 04b), NOT at the server
			//     level — server-level body caps would conflict with
			//     the per-source GitHub 25MB ceiling.
			srv := &http.Server{
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      30 * time.Second,
				IdleTimeout:       60 * time.Second,
				MaxHeaderBytes:    64 * 1024,
			}
			go func() {
				if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
					logger.Error("listener exited unexpectedly", "err", err)
				}
			}()
			hookStage("listener_started")
			logger.Info("HTTP listener bound", "addr", addr)
			logger.Info("worker started; SIGTERM/SIGINT to drain", "drain-timeout", drainTimeout)

			<-sigCh
			hookStage("signal_received")
			logger.Info("server draining; second SIGINT/SIGTERM forces immediate exit")

			drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
			defer cancel()

			// 12. D-7.1-11 listener-first shutdown. http.Server.Shutdown
			//     refuses new requests immediately and lets in-flight
			//     HTTP requests finish (which means in-flight
			//     ExecuteWorkflow dispatch calls finish — the workflow
			//     itself is now durable on the Temporal server). Shared
			//     drainCtx caps total drain budget across listener +
			//     worker stages.
			hookStage("listener_shutdown_started")
			if err := srv.Shutdown(drainCtx); err != nil {
				logger.Warn("listener shutdown returned error", "err", err)
			}
			hookStage("listener_shutdown_complete")

			done := make(chan struct{})
			var stopOnce sync.Once
			go func() {
				stopOnce.Do(func() {
					hookStage("drain_started")
					w.Stop() // SDK Stop blocks up to WorkerStopTimeout.
					close(done)
				})
			}()

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
	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "HTTP listener address for webhook deliveries (e.g. :8080)")
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
// Trigger entries are shaped {source: <kind>, flow: <flow-name>, mount:
// "<METHOD> <path>"} for HTTP-shaped sources (D-7.1 §Reusable Assets);
// non-HTTP sources (cron in 7.2, queue in v1.44+) omit the "mount" key.
// Sort order is supplied by w.Triggers().All() which Plan 04's Freeze
// sorts by (Source.Kind, FlowName, Pos).
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
		entry := map[string]string{
			"source": t.Source.Kind(),
			"flow":   t.FlowName,
		}
		// D-7.1 §Reusable Assets: extend trigger lines with mount
		// path for HTTP-shaped sources. Cron sources (Phase 7.2)
		// and queue sources (v1.44+) won't satisfy receiver.HTTPMounter
		// and their entries omit the "mount" key.
		if mounter, ok := t.Source.(receiver.HTTPMounter); ok {
			path, method := mounter.HTTPMount()
			entry["mount"] = method + " " + path
		}
		triggerLines[i] = entry
	}
	logger.Info("registered triggers", "count", len(trigs), "triggers", triggerLines)
}
