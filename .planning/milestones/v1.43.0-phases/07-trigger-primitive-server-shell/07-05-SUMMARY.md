---
phase: 07-trigger-primitive-server-shell
plan: 05
subsystem: cli
tags: [server-subcommand, drain-timeout, signal-notify, json-log, startup-banner, test-seams, charm-log, sorted-output]

# Dependency graph
requires:
  - phase: 07-04
    provides: "Worker.Triggers() *interpreter.TriggerRegistry, WorkerOptions.WorkerStopTimeout, TriggerRegistry sorted-slice access"
provides:
  - "skytime server cobra subcommand with --rootdir/--task-queue/--addr/--credfile/--drain-timeout/--json-log flags"
  - "newServerCommand RunE: range validation, charm-log/JSON logger swap, connectClient reuse, NewWorker with WorkerStopTimeout, sorted startup banner, two-signal escalation, drain-timeout select"
  - "FlowRegistry.FlowNames() []string sorted snapshot accessor (interpreter)"
  - "Worker.FlowNames() []string pass-through (worker)"
  - "Worker NewWorkerForTest(reg, trigs) test-only constructor that bypasses boot + SDK"
  - "setupServerLogging(debug, jsonMode) bool branch — JSON via slog.NewJSONHandler when jsonMode=true"
  - "minDrainTimeout/maxDrainTimeout/defaultDrainTimeout/defaultAddr flag constants in pkg/cli/flags.go"
  - "Six locked testDrainHook stage names + testForceExit seam for behavioral tests"
affects:
  - 07-06 (firewall + rename pass — no new firewall surface introduced; pkg/cli still allow-listed for cobra)
  - 07.1 (HTTP receiver attaches to skytime server's --addr; subcommand shell stays as-is)
  - 07.1 (drops t.Skip on TestServerCmd_DrainOnSIGTERM/DrainTimeoutExpiry/SecondSignalForceExit once worker.WithSDKFactory ships)

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "signal.Notify (NOT signal.NotifyContext) for two-signal escalation — NotifyContext is single-shot and would not receive the second signal during drain"
    - "Six-stage testDrainHook seam in pkg/cli/server.go: worker_started → signal_received → drain_started → {drain_completed | drain_forced | drain_timeout}"
    - "testForceExit seam replaces direct os.Exit so tests can verify forced-exit path without terminating the test process"
    - "setupServerLogging bypasses buildRoutedSlogLogger's progressHandler routing — server startup events are not flow events (Pitfall 7)"
    - "NewWorkerForTest test constructor (pkg/worker) for black-box tests in pkg/cli that cannot reach package-private sdkWorkerNew seam"
    - "Range validation BEFORE worker construction — pflag.Duration accepts 0 / negative syntactically; the RunE-side check enforces a 1s floor before any side effect"

key-files:
  created:
    - pkg/cli/server.go
    - pkg/cli/server_test.go
    - .planning/phases/07-trigger-primitive-server-shell/07-05-SUMMARY.md
  modified:
    - pkg/cli/flags.go
    - pkg/cli/render.go
    - pkg/cli/root.go
    - pkg/cli/root_test.go
    - pkg/interpreter/registry.go
    - pkg/interpreter/registry_test.go
    - pkg/worker/worker.go
    - pkg/worker/worker_test.go

key-decisions:
  - "Locked testDrainHook stage names (worker_started/signal_received/drain_started/drain_completed/drain_timeout/drain_forced) — Phase 7.1 will assert against the SAME strings once worker.WithSDKFactory unblocks the in-process tests; renaming would invalidate the regression-prevention story"
  - "Three signal-loop tests SHIPPED with t.Skip(\"TODO(phase-7.1): ...\") rather than deleted — function names match VALIDATION.md verification map for forward compatibility, the names locking the future test seam"
  - "Banner test uses NewWorkerForTest (NOT a real worker.NewWorker call) — pkg/cli is a black-box consumer of pkg/worker, and printStartupBanner takes a *worker.Worker; reaching into the package via NewWorkerForTest is the cleanest path"
  - "setupServerLogging at end of pkg/cli/render.go (NOT a new file) — keeps the four logger-construction functions co-located: setupLogging, buildSDKSlogLogger, buildRoutedSlogLogger, setupServerLogging"
  - "Plan's <interfaces> said pkg/extension/testing for FakeTriggerSource — actual location is pkg/extension (parent package) per Plan 02's deviation. The wave_history block in the prompt also pointed at the parent-package location. Imported from pkg/extension directly in TestServerCmd_BannerSorted"
  - "Plan acceptance criterion `grep -cE '|...stage names...|' returns 6` was tightened by collapsing the multi-line stage-comment in server.go to a single line — comment lines were inflating the count to 8, and 6 unique strings is the goal"

patterns-established:
  - "Server subcommand pattern: range-validate → setup logger → credfile sanity check → connectClient → NewWorker → printStartupBanner → Start → signal.Notify → select(done, sigCh, drainCtx). Future long-running subcommands (Phase 7.1's HTTP receiver mounts on top of this skeleton) follow the same skeleton"
  - "Test-skip-with-named-function pattern: when a behavioral test cannot be implemented due to a reachability blocker, ship the function with t.Skip(\"TODO(phase-X.Y): ...\") so VALIDATION.md mapping stays stable AND the regression-prevention story (locked names) is preserved"
  - "NewWorkerForTest as the standard escape hatch for black-box tests of any future cli subcommand that needs a populated *worker.Worker without booting Temporal"

requirements-completed: [SERVER-01, SERVER-02, SERVER-03]

# Metrics
duration: 8min
completed: 2026-05-08
---

# Phase 07 Plan 05: Server Subcommand Summary

**`skytime server` long-running subcommand wired with sorted startup banner, two-signal drain escalation, configurable `--drain-timeout`, charm-log/JSON logger swap, and a six-stage `testDrainHook` seam — covering SERVER-01..03 with the unit-testable subset and deferring three end-to-end signal-loop tests to Phase 7.1 once `worker.WithSDKFactory` ships.**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-05-08T20:25:31Z
- **Completed:** 2026-05-08T20:33:46Z
- **Tasks:** 7 (Tasks 5/6/7 marked TDD; production code already existed from Tasks 1-4 → tests authored against existing implementation, all GREEN-only)
- **Files:** 2 created, 8 modified
- **Commits:** 7 (one per task)

## Accomplishments

### `skytime server` flag inventory (D-07-17..D-07-20)

| Flag             | Type     | Default          | Validation / Behavior                                                                          |
| ---------------- | -------- | ---------------- | ---------------------------------------------------------------------------------------------- |
| `--rootdir`      | string   | (required)       | `cmd.MarkFlagRequired("rootdir")` — directory containing `.star` files                          |
| `--task-queue`   | string   | `"skytime"`      | passed to `worker.WorkerOptions.TaskQueue`                                                     |
| `--addr`         | string   | `":8080"`        | accepted now; warns "no effect until Phase 7.1 ships the HTTP receiver" if `Changed`           |
| `--credfile`     | string   | `""`             | rejected with friendly error pointing at `cli.WithCredentialHandler` if `cfg.credHandler==nil` |
| `--drain-timeout`| duration | `30s`            | range-validated `[1s, 1h]`; sub-1s rejected; >1h accepted with warning                          |
| `--json-log`     | bool     | `false`          | toggles charm-log default → `slog.NewJSONHandler(os.Stderr, ...)`                              |

Defaults match D-07-17 (`30s` matches Kubernetes `terminationGracePeriodSeconds`); `--addr` placeholder = `:8080` (Phase 7.1 owns the actual listener).

### `connectClient(cfg)` reuse (D4-08)

The server subcommand reuses `pkg/cli/connect.go::connectClient` verbatim — same variant routing as `skytime run`:

```
--api-key set                       → NewCloudClient (TLS auto-enabled)
--client-cert + --client-key set    → NewSelfHostedClient (mTLS)
otherwise                           → NewDevClient (TLS off)
```

`TestServerCmd_ConnectClient` verifies cloud + dev variants by counting calls on the `defaultClientFactory` test seam. Selfhosted variant is skipped — covered by `pkg/cli/connect_test.go` for the run subcommand's identical path; mTLS PEM fixtures here would add zero marginal coverage.

### Two-signal escalation shape (LOCKED — Pitfall 5)

```go
sigCh := make(chan os.Signal, 2)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
defer signal.Stop(sigCh)

<-sigCh                          // first signal: drain
hookStage("signal_received")
go func() {
    stopOnce.Do(func() {
        hookStage("drain_started")
        w.Stop() // SDK Stop blocks up to WorkerStopTimeout
        close(done)
    })
}()
drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
defer cancel()
select {
case <-done:                     // drain_completed → return nil
case <-sigCh:                    // drain_forced → testForceExit(1)
case <-drainCtx.Done():          // drain_timeout → return errSilent
}
```

`signal.Notify` (NOT `signal.NotifyContext`) is load-bearing: `NotifyContext` cancels its derived context on the first signal and stops listening, so the SECOND signal needed for forced-exit escalation would never arrive. The buffered channel (size 2) ensures both signals can land without dropping if the receiver is between selects.

### `testDrainHook` stage names — LOCKED for Phase 7.1

Six stage strings, regression-prevented by the source-grep acceptance criterion in this plan (`grep -cE '...' pkg/cli/server.go == 6`):

```
worker_started     → after w.Start() succeeds
signal_received    → after first <-sigCh returns
drain_started      → inside the stopOnce goroutine, before w.Stop()
drain_completed    → done channel closed, drain returned cleanly
drain_forced       → second signal arrived during drain → testForceExit(1)
drain_timeout      → drainCtx.Done() fired before drain finished
```

Phase 7.1's worker.WithSDKFactory(fn) Option will let `pkg/cli`'s currently-skipped tests (`TestServerCmd_DrainOnSIGTERM` / `TestServerCmd_DrainTimeoutExpiry` / `TestServerCmd_SecondSignalForceExit`) drop their `t.Skip` and assert against these exact strings. Renaming any stage breaks the forward-compat story.

### `testForceExit` seam

```go
var testForceExit = func(code int) { os.Exit(code) }
```

Production unchanged from a direct `os.Exit(1)` call. Tests override this to a recorder closure so the `case <-sigCh` (second-signal) escalation path can be exercised without terminating the test process. The override site lives in the same package — pkg/cli's behavioral tests are white-box (`package cli`) so they have direct write access.

### `setupServerLogging` (charm-log default; JSON via `--json-log`)

```go
func setupServerLogging(debug, jsonMode bool) *slog.Logger {
    if !jsonMode {
        return setupLogging(debug)
    }
    level := slog.LevelInfo
    if debug { level = slog.LevelDebug }
    h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
    logger := slog.New(h)
    slog.SetDefault(logger)
    return logger
}
```

Branches at the top to either reuse `setupLogging` (charm-log Bazel-style) or construct a fresh `slog.NewJSONHandler` against `os.Stderr`. Both branches install the result as `slog.Default` so any indirect callers (e.g., parser-warning drain at boot via `slog.Default().Warn` from Plan 04) flow through the same handler.

Critically, this BYPASSES `buildRoutedSlogLogger`'s `progressHandler` routing (Pitfall 7). Server startup events are not flow events — they don't have an `event=*` key — so routing them through the Bazel-style step renderer would produce nonsense. Plain slog handlers are correct here.

### Startup banner shape (SERVER-03)

```go
logger.Info("starting server", "rootdir", rootdir, "task-queue", taskQueue, "addr", addr)
logger.Info("registered flows", "count", N, "flows", []string sorted)
logger.Info("registered triggers", "count", M, "triggers", []map[string]string{
    {"source": <kind>, "flow": <name>}, ...
})
```

Three records, emitted BEFORE `w.Start()` so operators see what's about to come online. Sort order:

- **Flows:** sorted alphabetically via `Worker.FlowNames()` → `FlowRegistry.FlowNames()` → `sort.Strings`. Plan 05 also re-sorts on the banner side as defense-in-depth (cheap on small N).
- **Triggers:** sorted by `(Source.Kind, FlowName, Pos)` via `Worker.Triggers().All()` — Plan 04's `TriggerRegistry.Freeze()` does the sort once at boot.

### `Worker.FlowNames()` + `FlowRegistry.FlowNames()` accessors

```go
// pkg/interpreter/registry.go
func (r *FlowRegistry) FlowNames() []string {
    r.mu.RLock(); defer r.mu.RUnlock()
    out := make([]string, 0, len(r.byFlow))
    for name := range r.byFlow { out = append(out, name) }
    sort.Strings(out); return out
}

// pkg/worker/worker.go
func (w *Worker) FlowNames() []string { return w.registry.FlowNames() }
```

Sorted-slice snapshot accessor; mutations on the returned slice do NOT propagate back into the registry. Two unit tests pin the contract:
`TestFlowRegistry_FlowNames` (sorted, snapshot semantics) and `TestFlowRegistry_FlowNames_Empty` (non-nil empty slice).

### `NewWorkerForTest` test-only constructor

```go
// pkg/worker/worker.go
func NewWorkerForTest(reg *interpreter.FlowRegistry, trigs *interpreter.TriggerRegistry) *Worker {
    return &Worker{registry: reg, triggers: trigs}
}
```

Builds a `*Worker` from pre-built registries WITHOUT running boot or constructing the SDK worker. Returned `*Worker` has nil `sdk` — calling `Start`/`Stop` would panic, by design.

This unblocks `TestServerCmd_BannerSorted` (and any future pkg/cli black-box test that needs a populated `*worker.Worker`). The package-private `sdkWorkerNew` test seam is reachable only from `pkg/worker`'s own tests (`package worker`); `pkg/cli`'s tests run as `package cli` and cannot touch it. `NewWorkerForTest` is the documented, exported escape hatch.

`TestNewWorkerForTest` proves the contract: 1 flow registered, `w.FlowNames()` returns `["foo"]`, `w.Triggers().All()` returns empty.

## Test Coverage for SERVER-01..03

`pkg/cli/server_test.go` (white-box, `package cli`):

| Test                                            | Behavior pinned                                                                | SERVER-* |
| ----------------------------------------------- | ------------------------------------------------------------------------------ | -------- |
| TestServerCmd_Flags                             | All 6 flags registered with correct types + defaults; rootdir marked required  | 01       |
| TestServerCmd_DrainTimeoutRangeCheck_Zero       | `--drain-timeout=0s` rejected before any side effect                           | 02       |
| TestServerCmd_DrainTimeoutRangeCheck_Negative   | Negative duration rejected with same message                                   | 02       |
| TestServerCmd_DrainTimeoutRangeCheck_AboveOneHour | 2h ACCEPTED (warning only); failure comes from connect-stub, not range check | 02       |
| TestServerCmd_ConnectClient (cloud, dev)        | apiKey → NewCloud; defaults → NewDev                                            | 01       |
| TestServerCmd_ConnectClient_SelfHosted          | SKIPPED — covered by pkg/cli/connect_test.go                                    | 01       |
| TestServerCmd_DrainOnSIGTERM                    | SKIPPED — TODO(phase-7.1): worker.WithSDKFactory                                | 02       |
| TestServerCmd_DrainTimeoutExpiry                | SKIPPED — TODO(phase-7.1): same blocker                                         | 02       |
| TestServerCmd_SecondSignalForceExit             | SKIPPED — TODO(phase-7.1): same blocker + testForceExit override harness        | 02       |
| TestServerCmd_BannerSorted                      | printStartupBanner emits 3 sorted records via NewWorkerForTest fixture          | 03       |
| TestServerCmd_JSONLog                           | setupServerLogging(false, true) emits JSON-handler records on os.Stderr        | 03       |

Plus presence test in root_test.go: `TestRoot_HasServerSubcommand` (pins the AddCommand registration line).

Plus accessor tests:
- `pkg/interpreter/registry_test.go::TestFlowRegistry_FlowNames` (sorted, snapshot)
- `pkg/interpreter/registry_test.go::TestFlowRegistry_FlowNames_Empty` (non-nil empty)
- `pkg/worker/worker_test.go::TestWorker_FlowNames` (3-flow .star file → sorted output)
- `pkg/worker/worker_test.go::TestNewWorkerForTest` (registries → populated *Worker)

## Manual Smoke Test (VALIDATION.md § Manual-Only Verifications)

```bash
# Terminal 1:
temporal server start-dev

# Terminal 2:
go run ./cmd/skytime server --rootdir=examples/http-github-webhook/ --task-queue=demo --temporal=localhost:7233
# Observe sorted: starting server, registered flows: [a, b, c], registered triggers: [...]
# Press Ctrl-C; observe: server draining
# Wait for: drain complete; exit 0
```

`go run ./cmd/skytime --help | grep server` confirms the subcommand is registered:
```
  server      Run a long-lived Skytime worker (drain-on-SIGTERM)
```

## Task Commits

Each task committed atomically:

1. **Task 1 (flag constants + setupServerLogging)** — `b2586e6` (feat)
2. **Task 2 (FlowNames accessors + NewWorkerForTest)** — `a6336dc` (feat)
3. **Task 3 (server.go full RunE + signal loop + banner + seams)** — `4d5d90b` (feat)
4. **Task 4 (root.go AddCommand + presence test)** — `557d7e0` (feat)
5. **Task 5 (flag/range/connectClient tests)** — `9531f46` (test)
6. **Task 6 (signal-loop test stubs skipped TODO)** — `505dd57` (test)
7. **Task 7 (banner-sorted + json-log tests)** — `2570c68` (test)

Plan metadata commit: TBD (after STATE.md / ROADMAP.md updates).

## Decisions Made

- **Six locked stage names matter.** The three skipped tests in Task 6 ship with their CORRECT names (TestServerCmd_DrainOnSIGTERM etc.) so VALIDATION.md's verification map stays stable. Renaming a stage in pkg/cli/server.go would invalidate the future regression-prevention story. The acceptance criterion `grep -cE '...|stages|...' pkg/cli/server.go == 6` formalizes this.
- **Banner test uses NewWorkerForTest, NOT a temp `.star` directory.** A temp dir would force the parser path to run, plus boot, plus SDK worker construction — none of which the banner test exercises. NewWorkerForTest builds a `*Worker` directly from `interpreter.NewRegistry` + `interpreter.NewTriggerRegistry`, exactly what the banner reads.
- **JSON-log test swaps `os.Stderr` via os.Pipe (not by passing a writer through setupServerLogging).** The chosen API surface for `setupServerLogging(debug, jsonMode bool) *slog.Logger` does NOT take an io.Writer — the JSON handler hard-codes `os.Stderr` because production usage is always to stderr. The pipe swap is the cleanest way to capture without changing the API.
- **`testForceExit` defaults to `os.Exit` (NOT `panic`).** Tests override it to a recorder; production behavior is identical to a direct `os.Exit(1)` call. Using a panic in the default would break production drain semantics on second-signal escalation.
- **Plan's interfaces block referenced `pkg/extension/testing` for `FakeTriggerSource`.** The actual location is `pkg/extension` (parent package) per Plan 02's deviation — sub-packages can't satisfy the parent's unexported `triggerSourceMarker` seal. The `wave_history` block in this prompt also pointed at the parent-package location. Imported from `pkg/extension` directly. This is a non-deviation: the plan was written against an outdated assumption.
- **Stage-name comment compressed to one line.** The plan's interfaces block had the stage names on three comment lines (`// "worker_started", "signal_received", ...`); the source-grep acceptance criterion `wc -l == 6` would have been inflated to 8 by the multi-line comment. Compressed to a single line.

## Deviations from Plan

None directly. Two notational adaptations worth recording:

**1. FakeTriggerSource import path.** The plan's Task 7 instructions imported `pkg/extension/testing` (alias `exttest`) for `FakeTriggerSource`. Actual home is `pkg/extension` (parent package, per Plan 02's compile-forced relocation). The `wave_history` section of this prompt also documented the parent-package location. Used `extension.FakeTriggerSource` directly — the alias was unnecessary.

**2. Stage-name comment compression.** Plan's interfaces block had stage names on three consecutive comment lines. Source-grep acceptance counted those as matches, making the count 8 instead of the documented expected 6. Compressed comment to one line so the criterion holds verbatim. No semantic change.

## Issues Encountered

None. All 7 tasks ran clean on first GREEN attempt; no auto-fixes needed across Rules 1-3. Build, vet, and full-repo `go test -race -count=1` green at every task boundary.

## User Setup Required

None — pure CLI subcommand wiring + tests. No external services touched.

## Self-Check: PASSED

- File `pkg/cli/server.go`: FOUND
- File `pkg/cli/server_test.go`: FOUND
- Modifications to `pkg/cli/{flags.go, render.go, root.go, root_test.go}`: FOUND
- Modifications to `pkg/interpreter/{registry.go, registry_test.go}`: FOUND
- Modifications to `pkg/worker/{worker.go, worker_test.go}`: FOUND
- Commit `b2586e6` (Task 1): FOUND
- Commit `a6336dc` (Task 2): FOUND
- Commit `4d5d90b` (Task 3): FOUND
- Commit `557d7e0` (Task 4): FOUND
- Commit `9531f46` (Task 5): FOUND
- Commit `505dd57` (Task 6): FOUND
- Commit `2570c68` (Task 7): FOUND
- `go build ./...`: PASS
- `go vet ./...`: PASS
- `go test ./... -count=1 -race`: PASS (full repo green)
- `go test ./pkg/cli/ -run TestServerCmd_ -count=1 -race`: PASS (11 tests; 4 skipped per design)
- `go test ./pkg/cli/ -run TestRoot_HasServerSubcommand -count=1 -race`: PASS
- `go test ./pkg/interpreter/ -run TestFlowRegistry_FlowNames -count=1 -race`: PASS (2 tests)
- `go test ./pkg/worker/ -run 'TestWorker_FlowNames|TestNewWorkerForTest' -count=1 -race`: PASS
- `go run ./cmd/skytime --help | grep '^  server'`: PASS (`server      Run a long-lived Skytime worker (drain-on-SIGTERM)`)
- `go test ./tests/ -run TestNoCobraImportsOutsideAllowList -count=1`: PASS (firewall unaffected)

## Next Phase Readiness

- **Plan 06 (firewall + rename)** unblocked. Plan 06's `dev-server` → `dev-temporal` rename touches `pkg/cli/dev_server.go` and root.go but does NOT interact with `server.go` (different subcommand). Plan 06's grep-gate firewall against `*dag.Trigger` / TriggerSource concrete types continues to be the credential-redaction final-line-of-defense.
- **Phase 7.1 (HTTP webhook receiver)** unblocked at the subcommand level. The HTTP receiver attaches to `--addr` and iterates `w.Triggers().All()` grouped by `Source.Kind()` to mount handlers; the `testDrainHook` stage names locked here let Phase 7.1's behavioral tests assert verbatim. Phase 7.1 also ships `worker.WithSDKFactory(fn)` Option which drops the `t.Skip` on three tests in `pkg/cli/server_test.go`.
- **No blockers.** All SERVER-01..03 unit-testable acceptance criteria satisfied; signal-loop end-to-end deferred to Phase 7.1 with named test stubs preserving the verification map.

---
*Phase: 07-trigger-primitive-server-shell*
*Completed: 2026-05-08*
