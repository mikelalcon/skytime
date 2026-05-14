---
phase: 07-trigger-primitive-server-shell
plan: 05
type: execute
wave: 4
depends_on: [04]
priority: high
estimated_tasks: 7
autonomous: true
requirements:
  - SERVER-01
  - SERVER-02
  - SERVER-03
files_modified:
  - pkg/cli/server.go
  - pkg/cli/server_test.go
  - pkg/cli/flags.go
  - pkg/cli/root.go
  - pkg/cli/root_test.go
  - pkg/cli/render.go
  - pkg/interpreter/registry.go
  - pkg/interpreter/registry_test.go
  - pkg/worker/worker.go
  - pkg/worker/worker_test.go
must_haves:
  truths:
    - "skytime server is registered as a top-level cobra subcommand alongside validate / run / dev-server / info / test"
    - "skytime server flags: --rootdir (required), --task-queue (default 'skytime'), --addr (default ':8080', warns 'no effect until 7.1'), --credfile (no-op-when-no-handler), --drain-timeout (1s..1h with range validation, default 30s, warn-but-accept above 1h), --json-log (default false)"
    - "skytime server reuses connectClient(cfg) from pkg/cli/run.go for D4-08 variant routing (cloud / mTLS / dev)"
    - "Worker is built with WorkerStopTimeout = --drain-timeout via worker.WorkerOptions{WorkerStopTimeout: drainTimeout}"
    - "Startup banner emits sorted flows + triggers via slog: 'starting server', 'registered flows', 'registered triggers' with sorted slices; trigger entries shaped as {source: kind, flow: flowName}"
    - "Two-signal escalation via signal.Notify (NOT signal.NotifyContext): first SIGINT/SIGTERM = drain via worker.Stop(); second = forced testForceExit(1) (overridable in tests; defaults to os.Exit) with 'drain interrupted' diagnostic log"
    - "Drain-timeout expiry → returns errSilent for cobra exit code 1 + 'drain-timeout exceeded; restart resumes from event history' log"
    - "testDrainHook test seam: package-private func(stage string) lets tests observe drain stages: worker_started / signal_received / drain_started / drain_completed / drain_timeout / drain_forced"
    - "--addr emits warning 'note: --addr=X has no effect until Phase 7.1 ships the HTTP receiver' when explicitly set OR at default if user passed --addr explicitly"
    - "--credfile rejected with clear error pointing at binary configuration when --credfile is provided but cfg.credHandler is nil (D-07-19)"
    - "Worker.FlowNames() and FlowRegistry.FlowNames() exist as sorted-slice accessors for the startup banner — additions made in this plan to keep the banner non-leaky"
    - "worker.NewWorkerForTest test-only constructor in pkg/worker enables pkg/cli's banner test to bypass the SDK without subprocess plumbing"
  artifacts:
    - path: pkg/cli/server.go
      provides: "newServerCommand(cfg) cobra.Command with full RunE: flag validation, banner, signal loop, drain semantics, testDrainHook + testForceExit seams"
      contains: "func newServerCommand"
    - path: pkg/cli/server_test.go
      provides: "TestServerCmd_Flags, TestServerCmd_DrainTimeoutRangeCheck_*, TestServerCmd_ConnectClient, TestServerCmd_DrainOnSIGTERM (skipped TODO), TestServerCmd_DrainTimeoutExpiry (skipped TODO), TestServerCmd_SecondSignalForceExit (skipped TODO), TestServerCmd_BannerSorted, TestServerCmd_JSONLog"
      contains: "TestServerCmd_"
    - path: pkg/cli/flags.go
      provides: "Server-subcommand flag constants — minDrainTimeout, maxDrainTimeout, defaultDrainTimeout, defaultAddr"
      contains: "minDrainTimeout"
    - path: pkg/cli/root.go
      provides: "root.AddCommand(newServerCommand(cfg)) registration"
      contains: "newServerCommand(cfg)"
    - path: pkg/cli/render.go
      provides: "setupServerLogging(debug, jsonMode bool) *slog.Logger — branches charm-log vs slog.NewJSONHandler"
      contains: "setupServerLogging"
    - path: pkg/interpreter/registry.go
      provides: "FlowRegistry.FlowNames() []string sorted-slice accessor"
      contains: "func (r *FlowRegistry) FlowNames()"
    - path: pkg/worker/worker.go
      provides: "Worker.FlowNames() pass-through + NewWorkerForTest test-only constructor"
      contains: "NewWorkerForTest"
  key_links:
    - from: pkg/cli/server.go (newServerCommand RunE)
      to: pkg/cli/connect.go (connectClient)
      via: "c, err := connectClient(cfg)"
      pattern: "connectClient\\(cfg\\)"
    - from: pkg/cli/server.go (newServerCommand RunE)
      to: pkg/worker/worker.go (NewWorker)
      via: "worker.NewWorker(c, worker.WorkerOptions{WorkerStopTimeout: drainTimeout, ...})"
      pattern: "WorkerStopTimeout"
    - from: pkg/cli/server.go (printStartupBanner)
      to: pkg/worker/worker.go (Worker.Triggers + Worker.FlowNames)
      via: "w.Triggers().All() and w.FlowNames()"
      pattern: "w\\.Triggers\\(\\)\\.All\\(\\)"
    - from: pkg/cli/root.go (NewRootCommand)
      to: pkg/cli/server.go (newServerCommand)
      via: "root.AddCommand(newServerCommand(cfg))"
      pattern: "root\\.AddCommand\\(newServerCommand"
---

<objective>
Land the `skytime server` subcommand: long-running Temporal worker with sorted-banner startup, signal-driven drain, configurable drain-timeout, and optional JSON log output (SERVER-01..03). Wave 4: depends on Plan 04 (`Worker.Triggers()` + `WorkerOptions.WorkerStopTimeout`).

Purpose: This is the only Phase 7 surface that runs as a real long-lived process. Other plans ship pure data + parser/registry plumbing; Plan 05 wires those into a process with proper signal handling, deterministic banner output, and SDK-aligned drain semantics. Phase 7.1's HTTP receiver builds on top of this skeleton (adds the actual listener + handler mounting).

Output: `pkg/cli/server.go` (~250 LOC), `pkg/cli/server_test.go` (~400 LOC across 8 tests — three skipped with TODO pointing to Phase 7.1's worker.WithSDKFactory follow-up), `pkg/cli/flags.go` extension (~30 LOC), `pkg/cli/render.go` extension (~40 LOC), `pkg/cli/root.go` one-line edit, plus FlowNames + NewWorkerForTest accessors in pkg/interpreter / pkg/worker (~40 LOC).

LOAD-BEARING CONSTRAINTS:
1. **signal.Notify (NOT signal.NotifyContext)** per § Pitfall 5 — NotifyContext is single-shot; two-signal escalation requires direct Notify.
2. **`testDrainHook` test seam** — behavioral tests observe drain stages without subprocess plumbing. Stage names LOCKED: `worker_started`, `signal_received`, `drain_started`, `drain_completed`, `drain_forced`, `drain_timeout`.
3. **Bypass progressHandler routing** per § Pitfall 7 — server logging uses plain slog.Default() with the chosen handler (charm-log or JSON), NOT the buildRoutedSlogLogger pattern from `skytime run`.
4. **Range validation BEFORE worker construction** per § Pitfall 6 — `--drain-timeout=0` and negative values are invalid even though pflag.Duration accepts them syntactically.
5. **testForceExit seam** — replaces direct `os.Exit(1)` so tests can verify the second-signal escalation path without terminating the test process.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md
@.planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md
@.planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md
@.planning/phases/07-trigger-primitive-server-shell/07-04-SUMMARY.md
@CLAUDE.md
@pkg/cli/dev_server.go
@pkg/cli/run.go
@pkg/cli/connect.go
@pkg/cli/options.go
@pkg/cli/root.go
@pkg/cli/render.go
@pkg/cli/flags.go
@pkg/worker/worker.go
@pkg/worker/options.go
@pkg/interpreter/registry.go
@pkg/dag/trigger.go

<interfaces>
<!-- Concrete code patterns the executor MUST replicate. Module path: github.com/mikelalcon/skytime (verified via `head -1 go.mod`). -->

The full skytime server skeleton THIS PLAN must produce (paste into pkg/cli/server.go AS THE ENTIRE FILE):

```go
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

// testDrainHook is the package-private test seam (per § Pitfall 9).
// Production: nil. Tests assign to observe drain progression.
//
// Stage names (LOCKED — tests pin against these strings):
//   "worker_started", "signal_received", "drain_started",
//   "drain_completed", "drain_timeout", "drain_forced"
var testDrainHook func(stage string)

// testForceExit is the package-private test seam for the second-signal
// os.Exit(1) escalation path. Production calls os.Exit; tests override
// to record the call without terminating the test process.
var testForceExit = func(code int) { os.Exit(code) }

func hookStage(stage string) {
    if testDrainHook != nil {
        testDrainHook(stage)
    }
}

// newServerCommand returns the skytime server subcommand. SERVER-01..03.
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
        Long:  "Boots a Temporal worker against the configured task queue, registers flows + triggers from --rootdir, and stays up until SIGTERM/SIGINT. Drains in-flight workflows up to --drain-timeout (default 30s) on first signal; second signal forces immediate exit.",
        RunE: func(cmd *cobra.Command, args []string) error {
            // 1. Range validation BEFORE side effects.
            if drainTimeout < minDrainTimeout {
                return fmt.Errorf("--drain-timeout must be at least %s; got %s (use 30s default if unsure)", minDrainTimeout, drainTimeout)
            }

            // 2. Switch slog handler for --json-log per § Pitfall 7.
            logger := setupServerLogging(cfg.debug, jsonLog)
            if drainTimeout > maxDrainTimeout {
                logger.Warn("drain-timeout exceeds 1h; large drain windows may delay rolling deploys",
                    "value", drainTimeout)
            }

            // 3. credfile sanity check (D-07-19).
            if credfilePath != "" && cfg.credHandler == nil {
                return fmt.Errorf("--credfile=%s requires the binary to be built with cli.WithCredentialHandler (see docs/cli-binary.md); current binary has no credential handler wired", credfilePath)
            }

            // 4. Connect Temporal via D4-08 variant routing.
            c, err := connectClient(cfg)
            if err != nil {
                fmt.Fprintf(cmd.ErrOrStderr(), "connect: %s\n", err.Error())
                return errSilent
            }
            defer c.Close()

            // 5. Build worker with WorkerStopTimeout threaded through.
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

            // 6. Sorted startup banner BEFORE Start.
            printStartupBanner(logger, w, rootdir, taskQueue, addr)
            if cmd.Flags().Changed("addr") {
                logger.Warn("note: --addr has no effect until Phase 7.1 ships the HTTP receiver",
                    "addr", addr)
            }

            // 7. Start.
            if err := w.Start(); err != nil {
                fmt.Fprintf(cmd.ErrOrStderr(), "worker start: %s\n", err.Error())
                return errSilent
            }
            hookStage("worker_started")
            logger.Info("worker started; SIGTERM/SIGINT to drain", "drain-timeout", drainTimeout)

            // 8. Two-signal escalation via signal.Notify (NOT NotifyContext per § Pitfall 5).
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

// printStartupBanner — SERVER-03 sorted output. Three slog records:
//   "starting server" (rootdir, task-queue, addr)
//   "registered flows" (count, flows)
//   "registered triggers" (count, triggers)
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
```

Note: the `dag` and `extension` imports are NOT needed in server.go — `t.Source.Kind()` and `t.FlowName` use interface dispatch on values returned by `w.Triggers().All()`, and Go's type system propagates the underlying types without an explicit import.

The setupServerLogging extension (paste at end of pkg/cli/render.go):
```go
// setupServerLogging is the skytime server's logging entry point. Charm-log
// default; JSON via slog.NewJSONHandler when jsonMode is true. Server
// startup events are NOT flow events — bypasses the buildRoutedSlogLogger
// progressHandler routing per § Pitfall 7.
func setupServerLogging(debug, jsonMode bool) *slog.Logger {
    if !jsonMode {
        return setupLogging(debug)
    }
    level := slog.LevelInfo
    if debug {
        level = slog.LevelDebug
    }
    h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
    logger := slog.New(h)
    slog.SetDefault(logger)
    return logger
}
```

The flag constants (paste at end of pkg/cli/flags.go; add `"time"` import if missing):
```go
const (
    minDrainTimeout     = 1 * time.Second
    maxDrainTimeout     = 1 * time.Hour
    defaultDrainTimeout = 30 * time.Second
    defaultAddr         = ":8080"
)
```

The FlowRegistry.FlowNames() addition (paste into pkg/interpreter/registry.go after Lookup):
```go
// FlowNames returns a fresh sorted slice of all registered flow names.
// Used by pkg/cli/server.go's startup banner (Phase 7).
func (r *FlowRegistry) FlowNames() []string {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make([]string, 0, len(r.byFlow))
    for name := range r.byFlow {
        out = append(out, name)
    }
    sort.Strings(out)
    return out
}
```

The Worker.FlowNames() + NewWorkerForTest additions (paste into pkg/worker/worker.go):
```go
// FlowNames returns the sorted list of registered flow names.
func (w *Worker) FlowNames() []string { return w.registry.FlowNames() }

// NewWorkerForTest constructs a Worker from pre-built registries WITHOUT
// running boot or starting the SDK worker. Test-only — pkg/cli's banner
// test (TestServerCmd_BannerSorted) uses this because pkg/cli is a
// black-box consumer of pkg/worker and cannot reach the package-private
// sdkWorkerNew seam.
func NewWorkerForTest(reg *interpreter.FlowRegistry, trigs *interpreter.TriggerRegistry) *Worker {
    return &Worker{
        registry: reg,
        triggers: trigs,
    }
}
```

The root.go registration (insert in pkg/cli/root.go::NewRootCommand between dev-server and info):
```go
root.AddCommand(newDevServerCommand(cfg))
root.AddCommand(newServerCommand(cfg)) // NEW (Phase 7)
root.AddCommand(newInfoCommand(cfg))
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <id>07-05-01</id>
  <name>Task 1: Define server-subcommand flag constants and setupServerLogging branch</name>
  <read_first>
    - pkg/cli/flags.go (FULL file)
    - pkg/cli/render.go (FULL file — setupLogging + buildRoutedSlogLogger)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Pitfall 7)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (D-07-17, D-07-18, D-07-20)
  </read_first>
  <files>pkg/cli/flags.go, pkg/cli/render.go</files>
  <action>
    Step 1 — Edit `pkg/cli/flags.go`. Add `"time"` import if absent. APPEND the four constants from `<interfaces>` at end of file: minDrainTimeout, maxDrainTimeout, defaultDrainTimeout, defaultAddr.

    Step 2 — Edit `pkg/cli/render.go`. APPEND `setupServerLogging` from `<interfaces>` at end of file.

    Step 3 — Verify:
    ```bash
    go build ./pkg/cli/... && go vet ./pkg/cli/...
    ```

    Both must exit 0. No new tests in this task — Task 7 covers JSON log behavior.

    DO NOT add JSON branch to setupLogging itself; server-specific behavior stays in setupServerLogging.
    DO NOT add the constants to options.go.
  </action>
  <acceptance_criteria>
    - `grep -nE 'minDrainTimeout\s*=\s*1 \* time\.Second' pkg/cli/flags.go` returns exactly one match
    - `grep -nE 'maxDrainTimeout\s*=\s*1 \* time\.Hour' pkg/cli/flags.go` returns exactly one match
    - `grep -nE 'defaultDrainTimeout\s*=\s*30 \* time\.Second' pkg/cli/flags.go` returns exactly one match
    - `grep -nE 'defaultAddr\s*=\s*":8080"' pkg/cli/flags.go` returns exactly one match
    - `grep -n 'func setupServerLogging' pkg/cli/render.go` returns exactly one match
    - `grep -n 'slog.NewJSONHandler' pkg/cli/render.go` returns at least one match
    - `go build ./pkg/cli/...` exits 0
    - `go vet ./pkg/cli/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go build ./pkg/cli/... && go vet ./pkg/cli/... && grep -q 'minDrainTimeout' pkg/cli/flags.go && grep -q 'defaultDrainTimeout' pkg/cli/flags.go && grep -q 'func setupServerLogging' pkg/cli/render.go && grep -q 'slog.NewJSONHandler' pkg/cli/render.go</automated>
  </verify>
  <done>
    Flag constants and setupServerLogging defined; nothing yet calls them. Tasks 2-7 wire the rest.
  </done>
</task>

<task type="auto">
  <id>07-05-02</id>
  <name>Task 2: Add Worker.FlowNames(), FlowRegistry.FlowNames(), and NewWorkerForTest accessors</name>
  <read_first>
    - pkg/worker/worker.go (Plan 04 output — Worker struct + Triggers accessor)
    - pkg/interpreter/registry.go (FlowRegistry struct)
    - pkg/interpreter/registry_test.go
    - pkg/worker/worker_test.go (sdkWorkerNew + fakeSDKWorker harness from Plan 04)
  </read_first>
  <files>pkg/interpreter/registry.go, pkg/interpreter/registry_test.go, pkg/worker/worker.go, pkg/worker/worker_test.go</files>
  <action>
    Step 1 — Edit `pkg/interpreter/registry.go`. Insert the `FlowNames()` method on `*FlowRegistry` AFTER the existing `Lookup` method. Use the body from `<interfaces>`. Note: `sort` package is already imported.

    Step 2 — Edit `pkg/worker/worker.go`. Insert AFTER the existing `Triggers()` accessor (added by Plan 04):
    ```go
    func (w *Worker) FlowNames() []string { return w.registry.FlowNames() }
    ```
    Insert AFTER `buildDispatch` or near the end of the file:
    ```go
    // NewWorkerForTest constructs a Worker from pre-built registries WITHOUT
    // running boot or starting the SDK. Test-only — pkg/cli's
    // TestServerCmd_BannerSorted uses this because pkg/cli is a black-box
    // consumer of pkg/worker and cannot reach sdkWorkerNew.
    func NewWorkerForTest(reg *interpreter.FlowRegistry, trigs *interpreter.TriggerRegistry) *Worker {
        return &Worker{
            registry: reg,
            triggers: trigs,
        }
    }
    ```

    Step 3 — Add tests:

    `pkg/interpreter/registry_test.go`:
    ```go
    func TestFlowRegistry_FlowNames(t *testing.T) {
        r := NewRegistry()
        require.NoError(t, r.Register("zebra", "h1", &ParsedFlow{Flow: &dag.Flow{Name: "zebra"}}))
        require.NoError(t, r.Register("alpha", "h2", &ParsedFlow{Flow: &dag.Flow{Name: "alpha"}}))
        require.NoError(t, r.Register("middle", "h3", &ParsedFlow{Flow: &dag.Flow{Name: "middle"}}))
        r.Freeze()
        assert.Equal(t, []string{"alpha", "middle", "zebra"}, r.FlowNames())
        // Snapshot semantics
        names := r.FlowNames()
        names[0] = "MUTATED"
        assert.Equal(t, "alpha", r.FlowNames()[0])
    }

    func TestFlowRegistry_FlowNames_Empty(t *testing.T) {
        r := NewRegistry()
        r.Freeze()
        assert.Empty(t, r.FlowNames())
        assert.NotNil(t, r.FlowNames())
    }
    ```

    `pkg/worker/worker_test.go`:
    ```go
    func TestWorker_FlowNames(t *testing.T) {
        dir := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(dir, "flows.star"), []byte(`
flow(name = "z", steps = [])
flow(name = "a", steps = [])
flow(name = "m", steps = [])
`), 0o644))

        prev := sdkWorkerNew
        sdkWorkerNew = func(c client.Client, taskQueue string, opts sdkworker.Options) sdkworker.Worker {
            return fakeSDKWorker{}
        }
        t.Cleanup(func() { sdkWorkerNew = prev })

        w, err := NewWorker(fakeClient{}, WorkerOptions{
            RootDir:           dir,
            CredentialHandler: noopCredHandler{},
        })
        require.NoError(t, err)
        assert.Equal(t, []string{"a", "m", "z"}, w.FlowNames())
    }

    func TestNewWorkerForTest(t *testing.T) {
        flowReg := interpreter.NewRegistry()
        require.NoError(t, flowReg.Register("foo", "h1", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "foo"}}))
        flowReg.Freeze()
        trigReg := interpreter.NewTriggerRegistry()
        trigReg.Freeze()

        w := NewWorkerForTest(flowReg, trigReg)
        require.NotNil(t, w)
        assert.Equal(t, []string{"foo"}, w.FlowNames())
        assert.Empty(t, w.Triggers().All())
    }
    ```

    Step 4 — Run:
    ```bash
    go test ./pkg/interpreter/ -run TestFlowRegistry_FlowNames -count=1 -race
    go test ./pkg/worker/ -run 'TestWorker_FlowNames|TestNewWorkerForTest' -count=1 -race
    go test ./pkg/interpreter/... ./pkg/worker/... -count=1 -race
    ```

    All must exit 0.
  </action>
  <acceptance_criteria>
    - `grep -nE 'func \(r \*FlowRegistry\) FlowNames\(\) \[\]string' pkg/interpreter/registry.go` returns exactly one match
    - `grep -nE 'func \(w \*Worker\) FlowNames\(\) \[\]string' pkg/worker/worker.go` returns exactly one match
    - `grep -nE 'func NewWorkerForTest\(reg \*interpreter\.FlowRegistry, trigs \*interpreter\.TriggerRegistry\) \*Worker' pkg/worker/worker.go` returns exactly one match
    - `grep -nE 'func TestFlowRegistry_FlowNames\b' pkg/interpreter/registry_test.go` returns exactly one match
    - `grep -nE 'func TestWorker_FlowNames' pkg/worker/worker_test.go` returns exactly one match
    - `grep -nE 'func TestNewWorkerForTest' pkg/worker/worker_test.go` returns exactly one match
    - `go test ./pkg/interpreter/... ./pkg/worker/... -count=1 -race` exits 0
    - `go vet ./pkg/interpreter/... ./pkg/worker/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/interpreter/ -run TestFlowRegistry_FlowNames -count=1 -race && go test ./pkg/worker/ -run 'TestWorker_FlowNames|TestNewWorkerForTest' -count=1 -race && go test ./pkg/interpreter/... ./pkg/worker/... -count=1 -race && grep -q 'func (r \*FlowRegistry) FlowNames()' pkg/interpreter/registry.go && grep -q 'func (w \*Worker) FlowNames()' pkg/worker/worker.go && grep -q 'func NewWorkerForTest' pkg/worker/worker.go</automated>
  </verify>
  <done>
    Sorted flow-name accessors exist on both FlowRegistry and Worker. NewWorkerForTest test constructor enables Plan 05's banner test to bypass SDK setup.
  </done>
</task>

<task type="auto">
  <id>07-05-03</id>
  <name>Task 3: Implement pkg/cli/server.go (cobra command, RunE, signal loop, banner, testDrainHook + testForceExit seams)</name>
  <read_first>
    - pkg/cli/server.go (does NOT exist — this task creates it)
    - pkg/cli/run.go (FULL — connectClient + worker.NewWorker pattern)
    - pkg/cli/options.go (config struct)
    - pkg/cli/connect.go (connectClient signature)
    - pkg/cli/flags.go (Task 1 output)
    - pkg/cli/render.go (Task 1 output)
    - pkg/worker/worker.go (Plan 04 + Task 2 output — Worker.Triggers + Worker.FlowNames)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Code Examples Example 4 lines 967-1072 — verbatim signal loop; § Pitfall 5 + Pitfall 7 + Pitfall 9)
  </read_first>
  <files>pkg/cli/server.go</files>
  <action>
    Create `pkg/cli/server.go` (NEW file) using the FULL VERBATIM body from `<interfaces>`. Cover:
    - Package + minimal import block (10 imports: context, fmt, log/slog, os, os/signal, sort, sync, syscall, time, github.com/spf13/cobra, github.com/mikelalcon/skytime/pkg/worker).
    - `var testDrainHook func(stage string)` and `var testForceExit = func(code int) { os.Exit(code) }`.
    - `func hookStage(stage string)` helper.
    - `func newServerCommand(cfg *config) *cobra.Command` — full RunE with the 8-step recipe from `<interfaces>`.
    - `func printStartupBanner(logger, w, rootdir, taskQueue, addr)`.

    DO NOT import `dag` or `extension` directly — verify by attempting `go build` first; add only if compiler insists.

    DO NOT register the subcommand in root.go (Task 4).
    DO NOT write tests (Tasks 5-7).
    DO NOT add an HTTP listener (Phase 7.1).

    Verify:
    ```bash
    go build ./pkg/cli/...
    go vet ./pkg/cli/...
    go test ./tests/ -run TestNoCobraImportsOutsideAllowList -count=1
    ```

    All must exit 0.
  </action>
  <acceptance_criteria>
    - File `pkg/cli/server.go` exists
    - `grep -nE 'func newServerCommand\(cfg \*config\) \*cobra\.Command' pkg/cli/server.go` returns exactly one match
    - `grep -nE 'func printStartupBanner\(' pkg/cli/server.go` returns exactly one match
    - `grep -n 'var testDrainHook func(stage string)' pkg/cli/server.go` returns exactly one match
    - `grep -n 'var testForceExit' pkg/cli/server.go` returns exactly one match
    - `grep -n 'testForceExit(1)' pkg/cli/server.go` returns exactly one match
    - `grep -n 'signal.Notify(' pkg/cli/server.go` returns at least one match
    - `! grep -n 'signal.NotifyContext' pkg/cli/server.go` (must NOT use NotifyContext per § Pitfall 5)
    - `grep -n 'WorkerStopTimeout: drainTimeout' pkg/cli/server.go` returns exactly one match
    - `grep -n 'connectClient(cfg)' pkg/cli/server.go` returns exactly one match
    - `grep -nE 'cmd\.Flags\(\)\.DurationVar\(&drainTimeout, "drain-timeout"' pkg/cli/server.go` returns exactly one match
    - `grep -nE 'cmd\.Flags\(\)\.BoolVar\(&jsonLog, "json-log"' pkg/cli/server.go` returns exactly one match
    - `grep -nE 'MarkFlagRequired\("rootdir"\)' pkg/cli/server.go` returns exactly one match
    - `grep -n 'drain-timeout must be at least' pkg/cli/server.go` returns exactly one match
    - `grep -n 'drain-timeout exceeds 1h' pkg/cli/server.go` returns exactly one match
    - `grep -n '--addr has no effect until Phase 7.1' pkg/cli/server.go` returns exactly one match
    - `grep -nE '"worker_started"|"signal_received"|"drain_started"|"drain_completed"|"drain_timeout"|"drain_forced"' pkg/cli/server.go | wc -l` returns 6
    - `go build ./pkg/cli/...` exits 0
    - `go vet ./pkg/cli/...` exits 0
    - `go test ./tests/ -run TestNoCobraImportsOutsideAllowList -count=1` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go build ./pkg/cli/... && go vet ./pkg/cli/... && go test ./tests/ -run TestNoCobraImportsOutsideAllowList -count=1 && grep -q 'func newServerCommand' pkg/cli/server.go && grep -q 'signal.Notify(' pkg/cli/server.go && ! grep -q 'signal.NotifyContext' pkg/cli/server.go && grep -q 'WorkerStopTimeout: drainTimeout' pkg/cli/server.go && grep -q 'var testDrainHook func(stage string)' pkg/cli/server.go && grep -q 'var testForceExit' pkg/cli/server.go</automated>
  </verify>
  <done>
    pkg/cli/server.go exists with full RunE skeleton: range validation, setupServerLogging, credfile sanity, connectClient, NewWorker with WorkerStopTimeout, sorted banner, two-signal escalation via signal.Notify, drain via select. The six testDrainHook stages are LOCKED. testForceExit replaces direct os.Exit. File compiles; firewall test green.
  </done>
</task>

<task type="auto">
  <id>07-05-04</id>
  <name>Task 4: Register newServerCommand in pkg/cli/root.go and update root_test.go</name>
  <read_first>
    - pkg/cli/root.go (FULL file)
    - pkg/cli/root_test.go (FULL file — existing TestRoot_HasDevServerSubcommand or similar)
    - pkg/cli/server.go (Task 3 output)
  </read_first>
  <files>pkg/cli/root.go, pkg/cli/root_test.go</files>
  <action>
    Step 1 — Edit `pkg/cli/root.go::NewRootCommand`. Insert ONE LINE between the existing dev-server registration and the info registration:
    ```go
    root.AddCommand(newDevServerCommand(cfg))
    root.AddCommand(newServerCommand(cfg)) // NEW (Phase 7)
    root.AddCommand(newInfoCommand(cfg))
    ```

    Update the package doc comment at the top of root.go: change "the validate, run, and dev-server subcommands wired" → "the validate, run, dev-server, server, info, and test subcommands wired".

    Step 2 — Edit `pkg/cli/root_test.go`. Add:
    ```go
    func TestRoot_HasServerSubcommand(t *testing.T) {
        root, err := NewRootCommand()
        require.NoError(t, err)
        var found bool
        for _, c := range root.Commands() {
            if c.Name() == "server" {
                found = true
                break
            }
        }
        assert.True(t, found, "skytime server subcommand must be registered on root")
    }
    ```

    Step 3 — Verify:
    ```bash
    go build ./...
    go vet ./...
    go test ./pkg/cli/ -run TestRoot_HasServerSubcommand -count=1 -race
    go test ./pkg/cli/... -count=1 -race
    go run ./cmd/skytime --help 2>&1 | grep -q '^  server'
    ```

    All must exit 0.

    DO NOT touch examples/extbin/main.go — server subcommand inherited automatically.
  </action>
  <acceptance_criteria>
    - `grep -nE 'root\.AddCommand\(newServerCommand\(cfg\)\)' pkg/cli/root.go` returns exactly one match
    - `grep -nE 'root\.AddCommand\(newDevServerCommand\(cfg\)\)' pkg/cli/root.go` returns exactly one match (regression — dev-server still wired)
    - `grep -nE 'func TestRoot_HasServerSubcommand' pkg/cli/root_test.go` returns exactly one match
    - `go test ./pkg/cli/ -run TestRoot_HasServerSubcommand -count=1 -race` exits 0
    - `go test ./pkg/cli/... -count=1 -race` exits 0
    - `go build ./cmd/skytime/...` exits 0
    - `go run ./cmd/skytime --help 2>&1 | grep -q '^  server'` succeeds
  </acceptance_criteria>
  <verify>
    <automated>go build ./... && go test ./pkg/cli/ -run TestRoot_HasServerSubcommand -count=1 -race && go test ./pkg/cli/... -count=1 -race && grep -q 'root\.AddCommand(newServerCommand(cfg))' pkg/cli/root.go && go run ./cmd/skytime --help 2>&1 | grep -q '^  server'</automated>
  </verify>
  <done>
    skytime server registered on root; --help shows it; presence test passes; existing tests stay green.
  </done>
</task>

<task type="auto" tdd="true">
  <id>07-05-05</id>
  <name>Task 5: Author flag-validation tests in pkg/cli/server_test.go (Flags + DrainTimeoutRangeCheck + ConnectClient)</name>
  <read_first>
    - pkg/cli/server.go (Task 3 output)
    - pkg/cli/connect.go (clientFactory test seam — defaultClientFactory)
    - pkg/cli/run.go (run subcommand — connectClient interaction pattern)
    - pkg/cli/dev_server_test.go (existing test pattern — bytes.Buffer for stderr)
  </read_first>
  <files>pkg/cli/server_test.go</files>
  <behavior>
    - Test 1 (TestServerCmd_Flags): inspect flags via cmd.Flags().Lookup; assert all 6 flags present with correct types + defaults; assert rootdir is required.
    - Test 2 (TestServerCmd_DrainTimeoutRangeCheck): three sub-tests — 0s rejected, -5s rejected, 2h accepted (warns, then fails downstream at connectClient stub).
    - Test 3 (TestServerCmd_ConnectClient): override defaultClientFactory; sub-test for cloud variant (apiKey set), dev variant (no flags); skip selfhosted with TODO (mTLS PEM fixtures non-trivial).
  </behavior>
  <action>
    Create `pkg/cli/server_test.go` (NEW). Use `package cli` (white-box).

    Imports:
    ```go
    package cli

    import (
        "bytes"
        "errors"
        "testing"

        "github.com/spf13/cobra"
        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
        "go.temporal.io/sdk/client"

        "github.com/mikelalcon/skytime/pkg/worker"
    )
    ```

    Test 1 (TestServerCmd_Flags):
    ```go
    func TestServerCmd_Flags(t *testing.T) {
        cmd := newServerCommand(&config{})
        flagSpecs := []struct {
            name      string
            wantType  string
            wantValue string
        }{
            {"rootdir", "string", ""},
            {"task-queue", "string", "skytime"},
            {"addr", "string", ":8080"},
            {"credfile", "string", ""},
            {"drain-timeout", "duration", "30s"},
            {"json-log", "bool", "false"},
        }
        for _, spec := range flagSpecs {
            f := cmd.Flags().Lookup(spec.name)
            require.NotNil(t, f, "flag %q must be registered", spec.name)
            assert.Equal(t, spec.wantType, f.Value.Type())
            assert.Equal(t, spec.wantValue, f.DefValue)
        }
        f := cmd.Flags().Lookup("rootdir")
        require.NotNil(t, f)
        assert.NotEmpty(t, f.Annotations[cobra.BashCompOneRequiredFlag], "rootdir must be marked required")
    }
    ```

    Tests 2a, 2b, 2c (DrainTimeoutRangeCheck):
    ```go
    func TestServerCmd_DrainTimeoutRangeCheck_Zero(t *testing.T) {
        cmd := newServerCommand(&config{})
        cmd.SetArgs([]string{"--rootdir=/tmp", "--drain-timeout=0s"})
        cmd.SetErr(&bytes.Buffer{})
        err := cmd.Execute()
        require.Error(t, err)
        assert.Contains(t, err.Error(), "drain-timeout must be at least 1s")
    }

    func TestServerCmd_DrainTimeoutRangeCheck_Negative(t *testing.T) {
        cmd := newServerCommand(&config{})
        cmd.SetArgs([]string{"--rootdir=/tmp", "--drain-timeout=-5s"})
        cmd.SetErr(&bytes.Buffer{})
        err := cmd.Execute()
        require.Error(t, err)
        assert.Contains(t, err.Error(), "drain-timeout must be at least 1s")
    }

    func TestServerCmd_DrainTimeoutRangeCheck_AboveOneHour(t *testing.T) {
        prev := defaultClientFactory
        defaultClientFactory = clientFactory{
            NewCloud:      func(_ worker.CloudOptions) (client.Client, error) { return nil, errors.New("test-stub") },
            NewSelfHosted: func(_ worker.SelfHostedOptions) (client.Client, error) { return nil, errors.New("test-stub") },
            NewDev:        func(_ worker.DevClientOptions) (client.Client, error) { return nil, errors.New("test-stub") },
        }
        t.Cleanup(func() { defaultClientFactory = prev })

        cmd := newServerCommand(&config{})
        cmd.SetArgs([]string{"--rootdir=/tmp", "--drain-timeout=2h"})
        cmd.SetErr(&bytes.Buffer{})
        err := cmd.Execute()
        require.Error(t, err) // expected: connect stub error, NOT the range-check error
        assert.NotContains(t, err.Error(), "drain-timeout must be at least 1s",
            "2h must be accepted (warning only), not rejected")
    }
    ```

    Test 3 (ConnectClient):
    ```go
    func TestServerCmd_ConnectClient(t *testing.T) {
        cases := []struct {
            name     string
            cfg      *config
            wantCall string
        }{
            {"cloud", &config{apiKey: "k"}, "cloud"},
            {"dev", &config{}, "dev"},
        }
        for _, tc := range cases {
            t.Run(tc.name, func(t *testing.T) {
                var cloudCalls, selfHostedCalls, devCalls int
                prev := defaultClientFactory
                defaultClientFactory = clientFactory{
                    NewCloud:      func(_ worker.CloudOptions) (client.Client, error) { cloudCalls++; return nil, errors.New("stub") },
                    NewSelfHosted: func(_ worker.SelfHostedOptions) (client.Client, error) { selfHostedCalls++; return nil, errors.New("stub") },
                    NewDev:        func(_ worker.DevClientOptions) (client.Client, error) { devCalls++; return nil, errors.New("stub") },
                }
                t.Cleanup(func() { defaultClientFactory = prev })

                cmd := newServerCommand(tc.cfg)
                cmd.SetArgs([]string{"--rootdir=/tmp"})
                cmd.SetErr(&bytes.Buffer{})
                _ = cmd.Execute()

                switch tc.wantCall {
                case "cloud":
                    assert.Equal(t, 1, cloudCalls)
                    assert.Zero(t, selfHostedCalls)
                    assert.Zero(t, devCalls)
                case "dev":
                    assert.Zero(t, cloudCalls)
                    assert.Zero(t, selfHostedCalls)
                    assert.Equal(t, 1, devCalls)
                }
            })
        }
    }

    // selfhosted variant test deferred — requires valid mTLS PEM fixtures.
    func TestServerCmd_ConnectClient_SelfHosted(t *testing.T) {
        t.Skip("TODO: mTLS PEM fixtures non-trivial; selfhosted variant covered in pkg/cli/connect_test.go")
    }
    ```

    Verify:
    ```bash
    go test ./pkg/cli/ -run 'TestServerCmd_(Flags|DrainTimeoutRangeCheck|ConnectClient)' -count=1 -race
    go test ./pkg/cli/... -count=1 -race
    ```

    All must exit 0.
  </action>
  <acceptance_criteria>
    - `grep -nE 'func TestServerCmd_Flags' pkg/cli/server_test.go` returns exactly one match
    - `grep -nE 'func TestServerCmd_DrainTimeoutRangeCheck_Zero' pkg/cli/server_test.go` returns exactly one match
    - `grep -nE 'func TestServerCmd_DrainTimeoutRangeCheck_Negative' pkg/cli/server_test.go` returns exactly one match
    - `grep -nE 'func TestServerCmd_DrainTimeoutRangeCheck_AboveOneHour' pkg/cli/server_test.go` returns exactly one match
    - `grep -nE 'func TestServerCmd_ConnectClient' pkg/cli/server_test.go` returns exactly one match
    - `go test ./pkg/cli/ -run 'TestServerCmd_(Flags|DrainTimeoutRangeCheck|ConnectClient)' -count=1 -race` exits 0
    - `go test ./pkg/cli/... -count=1 -race` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/cli/ -run 'TestServerCmd_(Flags|DrainTimeoutRangeCheck|ConnectClient)' -count=1 -race && go test ./pkg/cli/... -count=1 -race</automated>
  </verify>
  <done>
    Flag presence + range validation + connectClient routing covered. The 2h-warn case asserts range check accepts; selfhosted variant skipped with TODO.
  </done>
</task>

<task type="auto" tdd="true">
  <id>07-05-06</id>
  <name>Task 6: Add signal-loop test stubs (DrainOnSIGTERM, DrainTimeoutExpiry, SecondSignalForceExit) — skipped with TODO pointing to Phase 7.1 worker test seam</name>
  <read_first>
    - pkg/cli/server.go (Task 3 output — testDrainHook stages)
    - pkg/cli/server_test.go (Task 5 output)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Pitfall 9 — testDrainHook recommended pattern)
    - .planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md (§ Manual-Only Verifications — manual smoke covers actual signal flow)
  </read_first>
  <files>pkg/cli/server_test.go</files>
  <action>
    Reachability blocker explanation: pkg/cli is a black-box consumer of pkg/worker. The `worker.sdkWorkerNew` seam used by Plan 04's worker_test.go to inject a fake SDK Worker is package-private. Without an exported `worker.WithSDKFactory(fn)` Option, pkg/cli tests cannot construct a Worker whose Stop() behavior is deterministic — and the signal-loop tests need exactly that.

    DECISION: ship the three test FUNCTIONS with `t.Skip("TODO(phase-7.1): ...")` so:
    1. Their NAMES match VALIDATION.md's per-task verification map (forward compatibility).
    2. The testDrainHook stage names are LOCKED in source (regression-prevented by source grep on server.go).
    3. Manual smoke (`temporal server start-dev` + real `skytime server` + Ctrl-C) covers actual behavior.
    4. Phase 7.1 (when adding worker.WithSDKFactory) drops the skips and implements the assertions.

    APPEND to `pkg/cli/server_test.go`:
    ```go
    func TestServerCmd_DrainOnSIGTERM(t *testing.T) {
        t.Skip("TODO(phase-7.1): pkg/cli tests cannot reach pkg/worker.sdkWorkerNew seam. Add exported worker.WithSDKFactory(fn) Option to enable in-process drain testing. Source-grep acceptance + testDrainHook stage names + manual smoke (VALIDATION.md § Manual-Only Verifications) cover this for Phase 7.")
    }

    func TestServerCmd_DrainTimeoutExpiry(t *testing.T) {
        t.Skip("TODO(phase-7.1): same reachability blocker as TestServerCmd_DrainOnSIGTERM — needs worker.WithSDKFactory")
    }

    func TestServerCmd_SecondSignalForceExit(t *testing.T) {
        t.Skip("TODO(phase-7.1): same reachability blocker — also needs testForceExit override harness wired with the seam")
    }
    ```

    Verify:
    ```bash
    go test ./pkg/cli/ -run 'TestServerCmd_(DrainOnSIGTERM|DrainTimeoutExpiry|SecondSignalForceExit)' -count=1 -race
    go test ./pkg/cli/... -count=1 -race
    go vet ./pkg/cli/...
    ```

    All must exit 0 (skipped tests count as pass for `go test` exit code).

    DO NOT delete the test functions — their names are the documentation pointer.
    DO NOT add worker.WithSDKFactory in this plan — that's Phase 7.1 scope.
  </action>
  <acceptance_criteria>
    - `grep -nE '^func TestServerCmd_DrainOnSIGTERM' pkg/cli/server_test.go` returns exactly one match
    - `grep -nE '^func TestServerCmd_DrainTimeoutExpiry' pkg/cli/server_test.go` returns exactly one match
    - `grep -nE '^func TestServerCmd_SecondSignalForceExit' pkg/cli/server_test.go` returns exactly one match
    - `grep -nE 't\.Skip\("TODO\(phase-7\.1\)' pkg/cli/server_test.go` returns at least 3 matches
    - `go test ./pkg/cli/ -run 'TestServerCmd_(DrainOnSIGTERM|DrainTimeoutExpiry|SecondSignalForceExit)' -count=1` exits 0 (skip = pass)
    - `go test ./pkg/cli/... -count=1 -race` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/cli/ -run 'TestServerCmd_(DrainOnSIGTERM|DrainTimeoutExpiry|SecondSignalForceExit)' -count=1 -race && go test ./pkg/cli/... -count=1 -race && [ "$(grep -cE 't\.Skip\(.TODO\(phase-7\.1\)' pkg/cli/server_test.go)" -ge 3 ]</automated>
  </verify>
  <done>
    Three skipped test stubs pin the function names matching VALIDATION.md. Source-grep + manual smoke + the locked stage names cover Phase 7's signal behavior end-to-end. Phase 7.1 owns the assertion implementation once worker.WithSDKFactory ships.
  </done>
</task>

<task type="auto" tdd="true">
  <id>07-05-07</id>
  <name>Task 7: Author banner + json-log tests (TestServerCmd_BannerSorted via NewWorkerForTest, TestServerCmd_JSONLog via stderr capture)</name>
  <read_first>
    - pkg/cli/server.go (Task 3 output — printStartupBanner)
    - pkg/cli/server_test.go (Tasks 5 + 6 output)
    - pkg/worker/worker.go (Task 2 output — NewWorkerForTest)
    - pkg/extension/testing/triggersource.go (FakeTriggerSource for fixtures)
  </read_first>
  <files>pkg/cli/server_test.go</files>
  <behavior>
    - Test 1 (TestServerCmd_BannerSorted): hand-build a Worker via NewWorkerForTest with three flows (in declaration order: zebra, alpha, middle) and two triggers (in declaration order: zebra-flow, alpha-flow) — both with the same Source.Kind. Capture slog records via slog.NewJSONHandler against a buffer (NOT setupServerLogging, which mutates global state). Call printStartupBanner directly. Parse the three JSON records. Assert flows array is ["alpha", "middle", "zebra"] sorted. Assert triggers array is sorted by (kind, flow): triggers[0].flow == "alpha", triggers[1].flow == "zebra".
    - Test 2 (TestServerCmd_JSONLog): swap os.Stderr for an os.Pipe-backed pair; call setupServerLogging(false, true); emit a record via the returned logger; close write end; read pipe; json.Unmarshal asserts msg, level, time, custom keys are present and parseable.
  </behavior>
  <action>
    APPEND to `pkg/cli/server_test.go`:

    Required additional imports (add to the test file's import block):
    ```go
    import (
        // ... existing ...
        "encoding/json"
        "io"
        "log/slog"
        "os"
        "strings"

        "go.starlark.net/syntax"

        "github.com/mikelalcon/skytime/pkg/dag"
        "github.com/mikelalcon/skytime/pkg/extension/testing" // alias as exttest below
        "github.com/mikelalcon/skytime/pkg/interpreter"
    )
    ```
    Use the import alias `exttest "github.com/mikelalcon/skytime/pkg/extension/testing"` to avoid conflict with the stdlib `testing` package.

    Test 1:
    ```go
    func TestServerCmd_BannerSorted(t *testing.T) {
        // Build registries directly via interpreter constructors; bypass parser+boot.
        flowReg := interpreter.NewRegistry()
        require.NoError(t, flowReg.Register("zebra", "h1", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "zebra"}}))
        require.NoError(t, flowReg.Register("alpha", "h2", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "alpha"}}))
        require.NoError(t, flowReg.Register("middle", "h3", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "middle"}}))
        flowReg.Freeze()

        trigReg := interpreter.NewTriggerRegistry()
        require.NoError(t, trigReg.Register("h1", &dag.Trigger{
            FlowName: "zebra",
            Source:   &exttest.FakeTriggerSource{KindName: "skytime.test.webhook", ReqFields: []string{"payload"}},
            Pos:      syntax.MakePosition(stringPtr("flows.star"), 5, 1),
        }))
        require.NoError(t, trigReg.Register("h2", &dag.Trigger{
            FlowName: "alpha",
            Source:   &exttest.FakeTriggerSource{KindName: "skytime.test.webhook", ReqFields: []string{"payload"}},
            Pos:      syntax.MakePosition(stringPtr("flows.star"), 11, 1),
        }))
        trigReg.Freeze()

        w := worker.NewWorkerForTest(flowReg, trigReg)

        // Capture banner output via JSON handler against a buffer.
        var buf bytes.Buffer
        logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

        printStartupBanner(logger, w, "/some/dir", "demo-queue", ":8080")

        lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
        require.Len(t, lines, 3, "banner emits 3 records: starting server, registered flows, registered triggers")

        var startingServer, registeredFlows, registeredTriggers map[string]any
        require.NoError(t, json.Unmarshal([]byte(lines[0]), &startingServer))
        require.NoError(t, json.Unmarshal([]byte(lines[1]), &registeredFlows))
        require.NoError(t, json.Unmarshal([]byte(lines[2]), &registeredTriggers))

        assert.Equal(t, "starting server", startingServer["msg"])
        assert.Equal(t, "/some/dir", startingServer["rootdir"])
        assert.Equal(t, "demo-queue", startingServer["task-queue"])
        assert.Equal(t, ":8080", startingServer["addr"])

        assert.Equal(t, "registered flows", registeredFlows["msg"])
        flows, _ := registeredFlows["flows"].([]any)
        assert.Equal(t, []any{"alpha", "middle", "zebra"}, flows)

        assert.Equal(t, "registered triggers", registeredTriggers["msg"])
        triggers, _ := registeredTriggers["triggers"].([]any)
        require.Len(t, triggers, 2)
        first, _ := triggers[0].(map[string]any)
        assert.Equal(t, "alpha", first["flow"])
        assert.Equal(t, "skytime.test.webhook", first["source"])
        second, _ := triggers[1].(map[string]any)
        assert.Equal(t, "zebra", second["flow"])
    }

    func stringPtr(s string) *string { return &s }
    ```

    Test 2:
    ```go
    func TestServerCmd_JSONLog(t *testing.T) {
        prevStderr := os.Stderr
        r, w, err := os.Pipe()
        require.NoError(t, err)
        os.Stderr = w
        t.Cleanup(func() { os.Stderr = prevStderr })

        prevDefault := slog.Default()
        t.Cleanup(func() { slog.SetDefault(prevDefault) })

        logger := setupServerLogging(false, true)
        require.NotNil(t, logger)
        logger.Info("test record", "key1", "value1", "count", 42)

        require.NoError(t, w.Close())
        var buf bytes.Buffer
        _, err = io.Copy(&buf, r)
        require.NoError(t, err)

        var rec map[string]any
        require.NoError(t, json.Unmarshal([]byte(strings.TrimRight(buf.String(), "\n")), &rec))
        assert.Equal(t, "test record", rec["msg"])
        assert.Equal(t, "value1", rec["key1"])
        assert.Equal(t, float64(42), rec["count"])
        assert.NotEmpty(t, rec["level"])
        assert.NotEmpty(t, rec["time"])
    }
    ```

    NOTE on syntax.MakePosition: verify the signature against `go doc go.starlark.net/syntax MakePosition`. If the function is `MakePosition(filename *string, line, col int32) Position`, the helper above is correct. If the signature differs (e.g., takes string by value, or takes int instead of int32), adjust accordingly. The test must NOT depend on the exact line/col values — only on the stable sort order they imply.

    Verify:
    ```bash
    go test ./pkg/cli/ -run 'TestServerCmd_(BannerSorted|JSONLog)' -count=1 -race
    go test ./pkg/cli/... ./pkg/worker/... -count=1 -race
    ```

    All must exit 0.
  </action>
  <acceptance_criteria>
    - `grep -nE 'func TestServerCmd_BannerSorted' pkg/cli/server_test.go` returns exactly one match
    - `grep -nE 'func TestServerCmd_JSONLog' pkg/cli/server_test.go` returns exactly one match
    - `grep -n 'NewWorkerForTest' pkg/cli/server_test.go` returns at least one match
    - `grep -n 'slog.NewJSONHandler' pkg/cli/server_test.go` returns at least one match
    - `grep -n 'setupServerLogging(false, true)' pkg/cli/server_test.go` returns at least one match
    - `grep -n 'json.Unmarshal' pkg/cli/server_test.go` returns at least 2 matches (banner test + json-log test)
    - `go test ./pkg/cli/ -run TestServerCmd_BannerSorted -count=1 -race` exits 0
    - `go test ./pkg/cli/ -run TestServerCmd_JSONLog -count=1 -race` exits 0
    - `go test ./pkg/cli/... -count=1 -race` exits 0
    - `go test ./pkg/worker/... -count=1 -race` exits 0
    - `go vet ./pkg/cli/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/cli/ -run 'TestServerCmd_(BannerSorted|JSONLog)' -count=1 -race && go test ./pkg/cli/... ./pkg/worker/... -count=1 -race && grep -q 'func TestServerCmd_BannerSorted' pkg/cli/server_test.go && grep -q 'func TestServerCmd_JSONLog' pkg/cli/server_test.go</automated>
  </verify>
  <done>
    Banner format proven sorted via direct printStartupBanner call against NewWorkerForTest fixture. JSON-log handler swap proven via os.Stderr pipe + json.Unmarshal. Together with Tasks 5+6 stubs, Plan 05 covers SERVER-01..03 with the unit-testable parts; signal-loop end-to-end deferred to Phase 7.1.
  </done>
</task>

</tasks>

<verification>
After all 7 tasks complete:

```bash
go build ./...
go vet ./...
go test ./pkg/cli/... ./pkg/worker/... ./pkg/interpreter/... -count=1 -race
```

All must exit 0.

Manual smoke test (per VALIDATION.md § Manual-Only Verifications):
```bash
# Terminal 1:
temporal server start-dev

# Terminal 2:
go run ./cmd/skytime server --rootdir=examples/http-github-webhook/ --task-queue=demo --temporal=localhost:7233
# Observe: starting server, registered flows: [a,b,c], registered triggers: [...]
# Press Ctrl-C; observe: server draining
# Wait for: drain complete; exit 0
```
</verification>

<success_criteria>
- SERVER-01 satisfied: skytime server runs as a long-lived subcommand; reuses connectClient for D4-08 routing; flag inventory matches D-07-17..D-07-20.
- SERVER-02 satisfied: SIGTERM/SIGINT initiates drain via worker.Stop with WorkerStopTimeout = --drain-timeout; --drain-timeout=0 rejected; expiry → exit 1; second-signal escalation → testForceExit(1) (default os.Exit). End-to-end signal-loop tests deferred to Phase 7.1; manual smoke covers Phase 7.
- SERVER-03 satisfied: startup banner emits sorted flows + triggers via slog; --json-log swaps to JSONHandler; default charm-log; trigger entries shaped {source, flow}.
- D-07-15..D-07-20 implemented per CONTEXT.md.
- Worker.FlowNames() + FlowRegistry.FlowNames() + NewWorkerForTest are the support accessors landed for the banner test.
- testDrainHook + testForceExit seams in place; six stage names LOCKED.
- Wave-4 unblocks Wave-5 (Plan 06 — dev-server → dev-temporal rename + firewall + grep gate).
</success_criteria>

<output>
After completion, create `.planning/phases/07-trigger-primitive-server-shell/07-05-SUMMARY.md` documenting:
- The skytime server flag inventory (rootdir, task-queue, addr, credfile, drain-timeout, json-log) with defaults and validation rules
- The connectClient reuse from pkg/cli/run.go (D4-08 routing)
- The two-signal escalation shape (signal.Notify, NOT NotifyContext) and the testDrainHook stage names — LOCKED for Phase 7.1
- The testForceExit seam for second-signal os.Exit testing
- The setupServerLogging branch (charm-log default, JSON via --json-log)
- The startup banner shape — slog records: "starting server", "registered flows", "registered triggers" with documented keys
- The Worker.FlowNames() + FlowRegistry.FlowNames() additions (sorted-slice accessors)
- The NewWorkerForTest test constructor in pkg/worker (test-only, unblocks pkg/cli black-box tests)
- The deferred Phase 7.1 follow-up: worker.WithSDKFactory(fn) Option to enable in-process drain testing; three TestServerCmd_* tests skipped with TODO pointers
- Test coverage list for SERVER-01..03 (TestServerCmd_*)
- Manual smoke test instructions for VALIDATION.md
</output>
