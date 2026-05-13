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
	"go.temporal.io/sdk/client"

	web "github.com/mikelalcon/skytime/pkg/cli/server/web"
	"github.com/mikelalcon/skytime/pkg/cli/server/web/deliveries"
	skycore "github.com/mikelalcon/skytime/pkg/extension/builtin/core"
	"github.com/mikelalcon/skytime/pkg/extension/receiver"
	"github.com/mikelalcon/skytime/pkg/extension/schedules"
	"github.com/mikelalcon/skytime/pkg/worker"
)

// testDrainHook is the package-private test seam (per § Pitfall 9 of
// 07-RESEARCH.md). Production: nil. Tests assign to observe drain
// progression without subprocess plumbing.
//
// Stage names (LOCKED — tests pin against these strings):
// worker_started, cron_reconcile_complete, listener_started,
// signal_received, listener_shutdown_started,
// listener_shutdown_complete, drain_started, drain_completed,
// drain_timeout, drain_forced.
//
// Phase 7 Plan 05 locked the original 6: worker_started, signal_received,
// drain_started, drain_completed, drain_timeout, drain_forced. Phase 7.1
// Plan 06 added 3 more for the HTTP listener lifecycle: listener_started,
// listener_shutdown_started, listener_shutdown_complete. Phase 7.2 Plan
// 03 adds 1 more: cron_reconcile_complete (only emitted when
// --cron-reconcile is set; sits between worker_started and
// listener_started).
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

// testListenerAddrFn is the package-private test seam for surfacing the
// actual listener address (after net.Listen resolves "127.0.0.1:0" to a
// concrete free port) to tests that need to make HTTP requests against
// the live server. Production: nil. Tests assign to capture the bound
// addr so http.Get("http://"+addr+"/api/events") can target the right
// port — Phase 07.3 Plan 05 strict SSE-shutdown-frame assertion.
var testListenerAddrFn func(addr string)

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
		rootdir                string
		taskQueue              string
		addr                   string
		credfilePath           string
		drainTimeout           time.Duration
		jsonLog                bool
		cronReconcile          bool
		replayHistoryThreshold int64
		temporalWebUI          string
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

			// Plan 07.3-04 (m3 from checker): reject negative
			// --replay-history-threshold BEFORE side effects so the
			// error surfaces before we connect to Temporal or boot
			// the worker. 0 is allowed and falls back to the default
			// (events.DefaultReplayHistoryThreshold = 50) inside
			// events.PollerConfig.applyDefaults.
			if replayHistoryThreshold < 0 {
				return fmt.Errorf("--replay-history-threshold must be >= 0; got %d (use 0 for the default of 50)", replayHistoryThreshold)
			}

			// Plan 07.3-04: env binding for --temporal-web-ui. cobra
			// honors the flag's explicit value when set; otherwise
			// fall back to SKYTIME_TEMPORAL_WEB_UI before the default
			// kicks in via the StringVar registration below.
			if !cmd.Flags().Lookup("temporal-web-ui").Changed {
				if v := os.Getenv("SKYTIME_TEMPORAL_WEB_UI"); v != "" {
					temporalWebUI = v
				}
			}

			// 2. Switch slog handler for --json-log per § Pitfall 7.
			//    The server uses plain slog handlers (charm-log or JSON),
			//    NOT the buildRoutedSlogLogger pattern from `skytime run`
			//    — server startup events are not flow events.
			logger := setupServerLogging(cfg.debug, jsonLog)

			// Surface SDK + workflow lifecycle events in server stdout.
			// The default cfg.sdkLogger (from buildSDKSlogLogger) routes
			// to io.Discard so `skytime run` keeps CLI output clean. For
			// long-running `skytime server`, operators NEED to see
			// workflow.GetLogger output ("skytime workflow start",
			// flow_start, flow_complete, etc.) and SDK chatter (task
			// queue connectivity, schedule fires). Otherwise cron-fired
			// workflow failures are invisible — operators only learn
			// about them via `temporal workflow list`.
			cfg.sdkLogger = logger
			if drainTimeout > maxDrainTimeout {
				logger.Warn("drain-timeout exceeds 1h; large drain windows may delay rolling deploys",
					"value", drainTimeout)
			}

			// 3. credfile path resolution (D-7.4-03 — owned entirely by
			//    applyCredfileFlag in pkg/cli/credfile.go to avoid
			//    duplicating ~9 lines of type-assertion + friendly-error
			//    plumbing across every subcommand). The flag is the
			//    highest-precedence layer of the resolution chain:
			//      --credfile > SKYTIME_CREDFILE_PATH > WithCredfile(arg) > $HOME/.skytime-credentials
			//    The helper short-circuits on empty flagValue, so the
			//    outer `if credfilePath != ""` guard is unnecessary.
			if err := applyCredfileFlag(cfg, credfilePath); err != nil {
				return err
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

			// 8a. Phase 7.2: --cron-reconcile. Designated replica only
			//     (D-7.2-10 single-flag opt-in). Failure exits non-zero
			//     (D-7.2-11 fail-loud); no in-process retry (D-7.2-12 —
			//     K8s CrashLoopBackoff is the retry layer). Boot-order
			//     position (D-7.2-16): between worker.Start (the polling
			//     worker must be live so workflows dispatched by
			//     newly-created Schedules can be picked up) and listener
			//     bind (failed reconcile must NOT bind the listener; K8s
			//     readinessProbe = TCP connect to --addr stays "not
			//     ready" until reconcile succeeds).
			if cronReconcile {
				var sc client.ScheduleClient
				if cfg.scheduleFactory != nil {
					sc = cfg.scheduleFactory(c)
				} else {
					sc = c.ScheduleClient()
				}
				plan, err := schedules.ReconcileCronSchedules(
					cmd.Context(),
					sc,
					w.Triggers().All(),
					w.Registry(),
					taskQueue,
					true, // apply
					logger,
				)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cron-reconcile failed: %s\n", err.Error())
					return errSilent
				}
				logger.Info("cron-reconcile applied",
					"creates", len(plan.Creates),
					"updates", len(plan.Updates),
					"deletes", len(plan.Deletes))
				hookStage("cron_reconcile_complete")
			}

			// 8b. Plan 07.3-04 (UI-02): delivery ring buffer for the
			//     dashboard's "Recent webhook deliveries" panel.
			//     Constructed BEFORE the mux so receiver.Deps and
			//     web.Mount share the same buffer instance.
			deliveryBuf := deliveries.NewRingBuffer(deliveries.DefaultCap)

			// 9. Mount HTTP receiver. receiver.Mount (Phase 7.1) walks
			//    worker.Triggers().All(), groups by (kind, path,
			//    method), and registers ONE handler per path that
			//    method-dispatches internally. Phase 7.3 Plan 02
			//    extended receiver.Deps with DeliveryBuffer +
			//    OnDelivery; OnDelivery is filled in once web.Mount
			//    returns it (it's the broadcaster fan-out callback).
			mux := http.NewServeMux()
			deps := receiver.Deps{
				Client:            c,
				CredentialHandler: cfg.credHandler,
				TaskQueue:         taskQueue,
				Logger:            logger,
				FlowRegistry:      w.Registry(),
				DeliveryBuffer:    deliveryBuf,
			}

			// 9b. Plan 07.3-04 (UI-01..04): mount dashboard routes +
			//     start the SSE broadcaster + workflow poller. The
			//     returned MountResult carries the OnDelivery
			//     callback we splice into deps before receiver.Mount
			//     runs.
			//
			//     B3 (Phase 7.3 checker): MountResult.Shutdown MUST
			//     run BEFORE srv.Shutdown(drainCtx) so SSE clients
			//     receive an event: shutdown frame before the
			//     listener drain cancels their request contexts.
			webResult := web.Mount(cmd.Context(), mux, web.MountOptions{
				Client:                 c,
				TaskQueue:              taskQueue,
				Registry:               w.Registry(),
				Buffer:                 deliveryBuf,
				Addr:                   addr,
				TemporalWebUI:          temporalWebUI,
				Logger:                 logger,
				Namespace:              cfg.namespace,
				PollInterval:           2 * time.Second,
				ReplayHistoryThreshold: replayHistoryThreshold,
			})
			deps.OnDelivery = webResult.OnDelivery
			receiver.Mount(mux, w, deps)

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
			// Surface the resolved addr to tests that pass --addr=127.0.0.1:0
			// and need to dial the live server. Fires AFTER bind succeeds
			// and BEFORE the listener goroutine begins serving so the test
			// always sees a concrete addr it can dial.
			if testListenerAddrFn != nil {
				testListenerAddrFn(ln.Addr().String())
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

			// 12a. Plan 07.3-04 B3 (Phase 7.3 checker): SSE shutdown
			//      sequence MUST publish the final "event: shutdown"
			//      frame BEFORE the listener drains. Order is:
			//
			//        webResult.Shutdown()          // close broadcaster → SSE handlers see channel close → write "event: shutdown\n\n" + Flush
			//        time.Sleep(50ms)              // grace window so the SSE flush lands on the wire before the listener cancels request contexts
			//        srv.Shutdown(drainCtx)        // D-7.1-11 listener-first drain (now strict listener-only, SSE shutdown already delivered)
			//        worker.Stop()                 // existing Phase 7.1 worker drain
			//
			//      Without this ordering, srv.Shutdown(drainCtx)
			//      would cancel SSE request contexts FIRST and the
			//      handlers would exit before the broadcaster's
			//      shutdown event reached the wire — browsers would
			//      see only TCP close (no orderly shutdown frame),
			//      breaking D-7.3-07.
			// Note: no new hookStage() calls — keeping the 7-stage
			// sequence stable for TestServerCmd_DrainOnSIGTERM. Plan
			// 07.3-05 will extend the test with strict SSE event
			// assertions (must observe "event: shutdown" before EOF)
			// without needing a new drain hook stage.
			webResult.Shutdown()
			time.Sleep(50 * time.Millisecond) // SSE flush grace window

			// 12b. D-7.1-11 listener-first shutdown. http.Server.Shutdown
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
	cmd.Flags().StringVar(&credfilePath, "credfile", "", "credential file path (overrides binary's build-time default; handler must implement SetCredfilePath)")
	cmd.Flags().DurationVar(&drainTimeout, "drain-timeout", defaultDrainTimeout,
		"max time to wait for in-flight workflows to complete on SIGTERM/SIGINT (1s..1h)")
	cmd.Flags().BoolVar(&jsonLog, "json-log", false, "emit logs as JSON instead of charm-log Bazel-style")
	cmd.Flags().BoolVar(&cronReconcile, "cron-reconcile", false,
		"perform cron Schedule reconciliation against the connected Temporal cluster at boot (one replica only; see docs/walkthroughs/cron-schedules.md)")
	// Plan 07.3-04 (Open Q 2 + D-7.3-15): dashboard knobs. The
	// poller publishes a workflow_replayed event when HistoryLength
	// delta per poll-cycle exceeds the threshold; the deep-link
	// prefix is the Temporal Web UI URL rendered against workflow
	// IDs in the dashboard table.
	cmd.Flags().Int64Var(&replayHistoryThreshold, "replay-history-threshold", 50,
		"HistoryLength delta per poll-cycle above which the dashboard marks a workflow as 'replayed' (best-effort heuristic; see Research Open Q 2). Must be >= 0; 0 means use the default (50).")
	cmd.Flags().StringVar(&temporalWebUI, "temporal-web-ui", "http://localhost:8233",
		"URL prefix for Temporal Web UI deep-links on dashboard workflow IDs (env SKYTIME_TEMPORAL_WEB_UI; set to empty string to render plain text)")
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
// "<METHOD> <path>"} for HTTP-shaped sources (D-7.1 §Reusable Assets) or
// `cron @ {schedule} ({timezone})` for cron sources (Phase 7.2 Plan 03).
// Queue sources (v1.44+) will omit the "mount" key unless they declare
// their own rendering branch.
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
		// path for HTTP-shaped sources.
		if mounter, ok := t.Source.(receiver.HTTPMounter); ok {
			path, method := mounter.HTTPMount()
			entry["mount"] = method + " " + path
		} else if src, ok := t.Source.(*skycore.CronSource); ok {
			// Phase 7.2 Plan 03: cron triggers render schedule +
			// timezone in the mount field for at-a-glance scanning.
			// Format mirrors 07.2-CONTEXT.md Specifics:
			// `cron @ 0 9 * * 1 (America/New_York) → weekly_digest`.
			entry["mount"] = fmt.Sprintf("cron @ %s (%s)", src.Schedule(), src.Timezone())
		}
		// Future v1.44+ queue sources will add their own branch here.
		triggerLines[i] = entry
	}
	logger.Info("registered triggers", "count", len(trigs), "triggers", triggerLines)
}
