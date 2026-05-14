# Phase 7: Trigger primitive + server shell — Research

**Researched:** 2026-05-08
**Domain:** Starlark parser primitive + DAG node + extension SDK type + long-running Temporal worker subcommand
**Confidence:** HIGH (SDK semantics + codebase shape verified end-to-end)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
Authoritative source: `.planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md`. Selected items reproduced here for downstream agents:

- **D-07-01 lambda env:** new `triggerTimeGlobals` = `lambdaTimeGlobals` (locked 20-key set) + `json.*` + `time.now()`. Defined in `pkg/bridge`.
- **D-07-02 payload injection:** lambdas receive a single `req` parameter modeled as `*starlarkstruct.Struct`. HTTP-shaped sources expose `req.payload` (parsed body) and `req.headers`; cron (later) exposes `req.scheduled_time` etc.
- **D-07-03 lambda signature:** both `map(req)` and `idempotency_key(req)` are single-positional. ROADMAP success-criterion-1's `lambda payload, headers` example is illustrative only — actual locked signature is `lambda req:`. REQUIREMENTS.md TRIG-01 wording will need updating during planning.
- **D-07-04 no determinism requirement:** trigger lambdas run at HTTP ingress before `client.ExecuteWorkflow`, so non-deterministic globals (`time.now()`) are observably safe. Document in `pkg/parser/doc.go`.
- **D-07-05 parse-time validation:** three-layer check — free-var lint (Phase 1), arity (exactly 1 positional), `req.<field>` attribute walk via the D4-02 `ctx_walk.go` pattern generalized.
- **D-07-06 sealed marker:** `type TriggerSource interface { triggerSourceMarker() }` in `pkg/extension/trigger.go`.
- **D-07-07 pkg layout:** `pkg/extension/trigger.go` — same package as `Extension` and `OperationSpec`.
- **D-07-08 namespace ownership:** sources live under their owning extension (`github.webhook(...)`), NOT under a separate `triggers.*` extension. **DEVIATES from REQUIREMENTS.md TRIG-07/08 wording.** REQUIREMENTS update is a planning task.
- **D-07-09 JSON shape:** `dag.Trigger.Source` JSON = `{ "kind": "github.webhook", "config": { ... } }`. Each concrete `TriggerSource` implements `MarshalJSON`. Round-trip via kind-keyed unmarshal registry. **CRITICAL: `config` carries credential ID string only — never resolved Secret values.**
- **D-07-10 redaction firewall:** AST-walking test rejects `%+v` / `%#v` against `*dag.Trigger` or `TriggerSource` types.
- **D-07-11 sibling registry:** new `interpreter.NewTriggerRegistry()` parallel to FlowRegistry; same content-hash discipline; same frozen-after-boot. `bootRegistry` returns `(*FlowRegistry, *TriggerRegistry, error)`. `NewWorker` accepts both.
- **D-07-12 cross-file FlowName resolve:** at parse-finalize, every `trigger.FlowName` MUST resolve to a known flow. Position-aware "trigger references unknown flow X; known flows: [...]" error.
- **D-07-13 duplicate (flow, source-kind) pairs allowed.** Literal byte-identical configs warn-only, never reject.
- **D-07-14 test stub:** parser tests use a `fakeTriggerSource` defined in `_test.go`; Phase 7 ships NO production trigger source factories. (First real one: `github.WebhookSource` in 7.1.)
- **D-07-15 signal handling:** SIGINT and SIGTERM identical → drain. Second signal forces immediate `worker.Stop()` + `os.Exit(1)`.
- **D-07-16 drain timeout expiry:** call `sdkworker.Worker.Stop()`, log `[skytime] drain-timeout exceeded; N workflows forced; restart resumes from event history`, exit 1. Temporal preserves workflow state on the server; durability is intact.
- **D-07-17 --drain-timeout flag:** default 30s, type `time.Duration` via `pflag.Duration`. Range-check at flag-parse: minimum 1s, maximum 1h (warn-but-accept above 1h).
- **D-07-18 --addr flag:** accepted in Phase 7 but UNUSED. At startup log: `[skytime] note: --addr=X has no effect until Phase 7.1 ships the HTTP receiver`. Default `:8080`.
- **D-07-19 connection flags:** `--task-queue`, `--temporal`, `--credfile` reuse `connectClient(cfg)` from `run.go`. `--credfile` is no-op-when-no-handler — Phase 7.4 owns the `cli.WithCredfile` Option lift.
- **D-07-20 startup log format:** charm-log Bazel-style by default; new `--json-log` boolean flips slog handler to JSON via `slog.NewJSONHandler`. Trigger lines use `source-kind → flow-name` arrow form. Names sorted lexicographically.
- **D-07-21 hard rename:** `dev-server` → `dev-temporal` across code + docs + CI smoke. Pre-1.0, no deprecation alias.
- **D-07-22 rename verification:** CI check / grep test fails if any tracked file (excluding `.planning/`, `CHANGELOG.md`) contains the literal `dev-server` after the rename.

### Claude's Discretion
- Internal naming (`newDevTemporalCommand`, `bootRegistryWithTriggers`, `validateTriggerLambda`) — pick consistent with precedent.
- Connection-flag retry behavior on initial Temporal connect — recommend YES (bounded exponential backoff) but defer to 7.1 if scope pressure.
- Test layout for `skytime server` — extend `dev_server_test.go` test-seam pattern (override hooks, `atomic.Pointer`).
- Boot ordering on extension `Initialize` failure preserved; warning on empty `ReqSchema()`.
- Firewall test mechanism for credential redaction (AST-walker shape).

### Deferred Ideas (OUT OF SCOPE)
- `triggers.github_webhook` namespace under separate `triggers` extension (REJECTED per D-07-08).
- All concrete source factories (`github.webhook`, `webhook.post`, `cron`) — Phase 7.1+.
- HTTP listener + signature validation — Phase 7.1.
- Idempotency mapping at ingress — Phase 7.1.
- `cli.WithCredfile` / `cli.WithBuildID` — Phase 7.4.
- Dashboard, manual trigger UI — Phase 7.3.
- Auth integration docs — Phase 7.5.
- Lifting `--json-log` to root command — v1.44+.
- Long-connection retry on `connectClient` for `skytime server` — Claude's discretion, recommended YES.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| TRIG-01 | Top-level `trigger(flow=, source=, map=, idempotency_key=, credential=)` builtin captures references with no I/O | `pkg/parser/builtins.go::builtinFlow` factory pattern (see § Codebase Integration Map § Parser); `pkg/parser/lambda_capture.go::captureLambda` for the lambdas; `pkg/parser/globals.go::newParseTimeGlobals` registration site |
| TRIG-02 | Extensions return new `TriggerSource` value type (parallel to `ActionRef`) | `pkg/extension/extension.go` SDK contract; `pkg/dag/action.go::ActionRef` precedent for opaque-payload-on-DAG-node; D-07-06 sealed-marker pattern |
| TRIG-03 | New `dag.Trigger` node with stable JSON marshaling (Kind, FlowName, Source, MapLambda, IdempotencyLambda, CredentialID); round-trips | `pkg/dag/node.go` sealed Node interface; `pkg/dag/marshal.go` (Pos-stripping precedent — extends to Secret-stripping per D-07-09); `pkg/dag/action.go::Freeze` recursion model |
| TRIG-04 | Position-aware errors at parse time: unknown flow, missing required source kwarg, source not a TriggerSource, malformed lambda free vars | `pkg/parser/builtins.go::wrapBuiltinError`; D4-02 `ctx_walk.go` AST visitor for `req.<field>` walk; finalize-pass conventions in `pkg/parser/finalize.go` |
| TRIG-05 | Boot registry walks `--rootdir` for `*.star`, skipping `*_test.star`; registers flows AND triggers from same files | `pkg/worker/boot.go::bootRegistry` — extend signature; `pkg/parser/parser.go::Flows() / Lambdas()` accessors as precedent for new `Triggers()` accessor |
| SERVER-01 | `skytime server --rootdir=... --task-queue=... --temporal=... --addr=... [--credfile=...]` long-running process | `pkg/cli/run.go::connectClient` reuse; `pkg/cli/dev_server.go::newDevServerCommand` signal-handling precedent; cobra subcommand pattern |
| SERVER-02 | SIGTERM drains workflows up to `--drain-timeout` (default 30s); forces shutdown after timeout | Temporal SDK `WorkerStopTimeout` field on `worker.Options` blocks Stop() up to the timeout (verified in SDK source); `signal.NotifyContext` + escalation pattern (signal.Notify channel for second-signal counting) |
| SERVER-03 | Startup logs `registered flows: [...]` and `registered triggers: [...]` in deterministic order | `pkg/cli/render.go::buildRoutedSlogLoggerWithHandle` for charm-log routing; `slog.NewJSONHandler` for `--json-log`; sort.Strings for determinism |
| CLI-13 | `skytime dev-server` renamed to `skytime dev-temporal` across code + docs + CI smoke | `git grep -F "dev-server"` — full rename surface mapped (see § Pitfall 8) |
</phase_requirements>

## Executive Summary

Phase 7 is a **foundation phase**: it lands three interlocking surfaces (parser primitive + DAG node + extension type, sibling registry + boot extension, server subcommand shell) without any HTTP routing, source factories, or runtime ingress logic. The downstream phases (7.1+) consume these contracts but don't change them — getting the contracts right is the entire value of Phase 7.

**What's clear (HIGH confidence):**
- The Starlark primitive shape mirrors `builtinFlow`/`builtinStep` factory pattern verbatim. Every parse-time concern (lambda capture, free-var lint, position-aware errors, finalize cross-references) has a precedent in Phase 1+4 code. The only genuine new work is the **`req.<field>` attribute walker**, which generalizes the existing D4-02 `ctx_walk.go` from a hardcoded "ctx" to a parameterized (free-var-name, valid-field-set) visitor. Two callers afterwards: ctx-walker (existing) and req-walker (new).
- **Temporal SDK Worker.Stop semantics are well-defined** (verified in SDK v1.42.0 source `internal_worker_base.go:699`): `Stop()` is blocking; it closes a stop channel and waits up to `WorkerStopTimeout` for in-flight tasks to complete, then cancels activity contexts and exits. Uncompleted workflow tasks aren't ACK'd back to the server, so Temporal's WorkflowTaskTimeout machinery redispatches them to another worker — this IS the durability story D-07-16 claims. Wire `WorkerStopTimeout = --drain-timeout` and the implementation is one field. **Caveat:** Go SDK shutdown completes even if individual activities are still running (they continue to consume slots until their goroutines return) — local activities and well-behaved activities that honor `ctx.Done()` exit cleanly; long-blocking activities can block past the deadline.
- The **registered-flows-and-triggers boot** extension is mechanical: `bootRegistry` already iterates flows from `p.Flows()`; add a parallel `p.Triggers()` accessor, sort by deterministic key, register each. The signature change `(*FlowRegistry, error)` → `(*FlowRegistry, *TriggerRegistry, error)` has exactly **one internal caller** (`pkg/worker/worker.go::NewWorker`); no external callers.
- The **`dev-server` → `dev-temporal` rename surface** is fully mapped by `git grep -F dev-server`: 11 production-code touchpoints (CLI files + 1 test fixture + extbin top comment) + ~10 doc files + 1 CI smoke script. None of the changes are technically risky; the only real concern is missing one (mitigated by D-07-22's CI grep gate).

**What's risky (MEDIUM-HIGH attention needed):**
- **Two-signal escalation pattern.** `signal.NotifyContext` only delivers the first signal then resets — it cannot count signals. The correct shape is direct `signal.Notify(ch, …)` plus a goroutine that switches on first vs second receive. The dev_server.go test seam pattern (`testRunningCmd atomic.Pointer`) doesn't transfer cleanly because `skytime server` is in-process (not subprocess). New test seam needed for behavioral coverage — recommend a `testDrainHook func(...)` package-private variable plus a goroutine + channel + 1s deadline harness.
- **TriggerRegistry shape — not a simple FlowRegistry mirror.** FlowRegistry keys by `(flow_name, content_hash)` because the workflow execution path needs to resolve "load this exact flow version." Triggers are different: the future HTTP router (7.1) iterates ALL triggers at boot to mount HTTP handlers; lookup-by-flow-name happens once at registration time, not per-request. Recommended shape: `byFile map[content_hash][]*dag.Trigger` plus a flat sorted slice for iteration. (See § Pitfall 1 for full discussion.)
- **Generalizing ctx_walk.go.** The current visitor's body is roughly 80 lines but tightly coupled: it hardcodes `state.has(acc.AttrName)` against a parser-internal `stateSchema`. Generalizing to a (validAttrs []string, kwargName string) signature requires (a) extracting `findCtxAccesses` to be parameterized by free-var name (currently uses first param), and (b) splitting the validation step from the walk step so a different validator can plug in. Minimum-invasive refactor: rename `findCtxAccesses` to `findFreeVarAccesses(src, filename, lambdaPos, freeVarName)` taking the param name as input; keep the existing `validateLambdaCtxAccesses` calling it with `"ctx"`; add `validateTriggerReqAccesses` calling it with `"req"`. (See § Pitfall 3.)
- **JSON marshaling for `dag.Trigger.Source`.** The two-field discriminated form (`{kind, config}`) requires a registry of unmarshal funcs (one per source kind). Phase 7 ships zero source kinds, so the registry is empty. The test stub `fakeTriggerSource` registers itself in test setup. Phase 7.1's first real source (`github.WebhookSource`) registers in its `init()`. **Risk:** init-order across packages can be subtle — recommend explicit registration in extension `Initialize()` rather than `init()`.

**Primary recommendation:** Decompose Phase 7 into 5 waves (see § Recommended Plan Decomposition). Wave 1 lands the pure-data DAG/extension types (no parser changes); Wave 2 wires the parser builtin + lambda env + req-walker; Wave 3 lands the sibling TriggerRegistry + boot extension; Wave 4 lands `skytime server` with drain semantics; Wave 5 is the rename + firewall + docs. Waves 1-2 are testable purely at parse-time; Wave 3 touches multiple integration points but is mechanical; Wave 4 is the only non-mechanical piece and benefits from concentrated focus.

## Standard Stack

The library and tooling are pinned by the project's existing `go.mod` (Go 1.25.8, Temporal SDK v1.42.0, starlark.net 2026-03-26 pseudo-version, cobra v1.10.2, pflag v1.0.9). Phase 7 introduces NO new direct dependencies — the standard library + existing transitive deps cover everything.

### Core (already in go.mod, used)
| Library | Version | Purpose | Why used here |
|---------|---------|---------|---------------|
| `go.starlark.net/starlark` | `v0.0.0-20260326113308-fadfc96def35` | Parser primitives | `*starlark.Function`, `*starlark.Dict`, `starlark.UnpackArgs` |
| `go.starlark.net/starlarkstruct` | (same module) | `*starlarkstruct.Struct` for `req` injection | Already used by `pkg/bridge/struct.go` for `ctx` |
| `go.starlark.net/syntax` | (same module) | AST walker for req-attribute validation | Already used by `pkg/parser/ctx_walk.go` |
| `go.starlark.net/lib/json` | (same module) | `json.encode` / `json.decode` for trigger lambdas | First-party; locked into `triggerTimeGlobals` per D-07-01. `var Module = ...` exposed as `*starlarkstruct.Module` |
| `go.starlark.net/lib/time` | (same module) | `time.now()` for trigger lambdas | First-party; used for D-07-01 `time.now`. Module exposes `now`, `from_timestamp`, `parse_duration`, `parse_time`, `time`, plus duration constants |
| `go.temporal.io/sdk/worker` | `v1.42.0` | `WorkerStopTimeout`-driven graceful drain | The exact field that powers D-07-16. **No new SDK dependency** — already used in `pkg/worker/worker.go` |
| `github.com/spf13/cobra` | `v1.10.2` | New `server` subcommand | Same pattern as existing `run` / `dev-server` / `validate` |
| `github.com/spf13/pflag` | `v1.0.9` | `pflag.Duration` for `--drain-timeout` | Verified: accepts `time.ParseDuration` syntax (`30s`, `1h`, `0`, `-5s`); validates format; doesn't validate range — Skytime adds its own range check per D-07-17 |
| `charm.land/log/v2` | `v2.0.0` | Charm-log handler for default startup banner | Already used; `pkg/cli/render.go::setupLogging` is the precedent |
| `log/slog` | stdlib (Go 1.25) | `slog.NewJSONHandler` for `--json-log` | First-party; routes to the same writers, just swap handler when `--json-log` is set |
| `os/signal` | stdlib | `signal.Notify` for two-signal escalation | `signal.NotifyContext` is single-shot — must use `signal.Notify` directly to count signals |

### Verification
All package versions verified against the project's existing `go.sum`. No new external dependencies are introduced by Phase 7.

```bash
# Already in go.mod:
go.temporal.io/sdk v1.42.0
github.com/spf13/cobra v1.10.2
github.com/spf13/pflag v1.0.9
charm.land/log/v2 v2.0.0
go.starlark.net v0.0.0-20260326113308-fadfc96def35
```

`go.starlark.net/lib/json` and `go.starlark.net/lib/time` are sub-packages of the existing `go.starlark.net` module — adding imports does not require a new module entry. Verified by inspecting `~/go/pkg/mod/go.starlark.net@v0.0.0-20260326113308-fadfc96def35/lib/`.

## Library/SDK Knowledge

### Temporal Go SDK v1.42.0 — Worker.Stop semantics

Verified in `~/go/pkg/mod/go.temporal.io/sdk@v1.42.0/internal/internal_worker_base.go`:

```go
// Stop is a blocking call and cleans up all the resources associated with worker.
func (bw *baseWorker) Stop() {
    if !bw.isWorkerStarted {
        return
    }
    close(bw.stopCh)
    bw.limiterContextCancel()

    if success := awaitWaitGroup(&bw.stopWG, bw.options.stopTimeout); !success {
        traceLog(func() {
            bw.logger.Info("Worker graceful stop timed out.", "Stop timeout", bw.options.stopTimeout)
        })
    }

    if bw.options.backgroundContextCancel != nil {
        bw.options.backgroundContextCancel(ErrWorkerShutdown)
    }
    bw.isWorkerStarted = false
}
```

And the option that drives the timeout (`internal_worker.go:180-181`):

```go
// WorkerStopTimeout is the time delay before hard terminate worker
WorkerStopTimeout time.Duration
```

**Concrete semantics:**
1. `Stop()` closes the worker's internal `stopCh` (poll loops exit; new tasks are not picked up).
2. `Stop()` BLOCKS in `awaitWaitGroup(&stopWG, stopTimeout)` waiting for in-flight tasks to drain.
3. If `stopTimeout` elapses, the wait returns false; SDK logs a graceful-stop-timed-out message; activity contexts are cancelled; `Stop()` returns.
4. **Workflow tasks that didn't ACK the server stay un-ACK'd.** Temporal server's WorkflowTaskTimeout (default 10s, configurable per-task-queue) elapses; server redispatches the task to another worker via the standard recovery path. **This is durability without per-Skytime invention.**
5. **Activity behavior is asymmetric.** From the SDK doc on `GetWorkerStopChannel`: *"After the timeout hits, the worker will cancel the activity context and then exit."* Activities that honor `ctx.Done()` exit cleanly; long-blocking activities can be force-terminated. **For Phase 7 this matters because:** Skytime's only activity is `ExecuteBatch` (Phase 2), which loops over actions and calls each operation's `OperationFunc(ctx, …)` with a deadline-bounded context (D2-15). Operations that respect ctx exit cleanly.

**Wiring for Phase 7:**
```go
// In pkg/cli/server.go (or pkg/cli/server/cmd.go):
worker.NewWorker(c, worker.WorkerOptions{
    RootDir: rootdir,
    TaskQueue: taskQueue,
    // ... other options ...
    // Plus the new field on WorkerOptions:
    WorkerStopTimeout: drainTimeout,
})
```

This requires adding a `WorkerStopTimeout` field to `pkg/worker/options.go::WorkerOptions` and threading it into `sdkworker.Options` inside `NewWorker`. **One-line SDK option threading + one-field WorkerOptions extension.**

**Two-stop-call pitfall:** SDK docs warn `Stop()` may panic if called a second time. Skytime's `Worker.Stop()` is already `sync.Once`-wrapped (`pkg/worker/worker.go:104`) — the second-signal force-stop path doesn't need to call SDK Stop directly; it should call `Worker.Stop()` (which is the wrapped sync.Once-safe version) plus `os.Exit(1)`.

### Two-signal escalation pattern (Go stdlib idiom)

`signal.NotifyContext` is convenient but **only fires once** — when the parent context is done, the registered signal handler resets. For Phase 7's "first signal = drain, second signal = force exit" requirement, the correct pattern is:

```go
// In pkg/cli/server.go RunE:
sigCh := make(chan os.Signal, 2) // buffer 2 so we don't lose the second signal
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
defer signal.Stop(sigCh)

// First signal: drain
<-sigCh
slog.Default().Info("server draining; second SIGINT/SIGTERM forces immediate exit")
drainCtx, drainCancel := context.WithTimeout(context.Background(), drainTimeout)
defer drainCancel()

// Goroutine watches for second signal during drain
forceExitCh := make(chan struct{})
go func() {
    select {
    case <-sigCh:
        close(forceExitCh)
    case <-drainCtx.Done():
        // drain finished naturally or timed out; nothing to do
    }
}()

// Drain by calling Stop() (blocks up to WorkerStopTimeout)
done := make(chan struct{})
go func() {
    w.Stop() // sync.Once-wrapped; idempotent
    close(done)
}()

select {
case <-done:
    return nil // clean drain
case <-forceExitCh:
    fmt.Fprintln(os.Stderr, "[skytime] drain interrupted; forcing exit")
    os.Exit(1)
case <-drainCtx.Done():
    fmt.Fprintln(os.Stderr, "[skytime] drain-timeout exceeded; restart resumes from event history")
    return errSilent // exit code 1 via cobra
}
```

**Test seam recommendation:** Expose a package-private `testDrainHook func(stage string)` callable at "drain-start", "drain-complete", "drain-forced", "drain-timeout" — tests inject hooks to assert ordering without dispatching real OS signals.

### `pflag.Duration` semantics

Verified via web fetch:
- Accepts any string parseable by `time.ParseDuration`: `"30s"`, `"1h"`, `"500ms"`, `"-5s"`, `"0"`.
- **Built-in validation:** invalid format (e.g., `"thirty seconds"`) returns an error from `pflag.Set`.
- **No range validation:** `--drain-timeout=0` and `--drain-timeout=-1h` parse successfully. Skytime must add range checks per D-07-17 (1s ≤ value ≤ 1h, warn-but-accept above 1h).

Pattern:
```go
var drainTimeout time.Duration
cmd.Flags().DurationVar(&drainTimeout, "drain-timeout", 30*time.Second,
    "max time to wait for in-flight workflows to complete on SIGTERM/SIGINT (default 30s)")
// In RunE, after flag parse:
if drainTimeout < time.Second {
    return fmt.Errorf("--drain-timeout must be ≥ 1s; got %s", drainTimeout)
}
if drainTimeout > time.Hour {
    slog.Default().Warn("--drain-timeout exceeds 1h; this may indicate misuse", "value", drainTimeout)
}
```

### Charm-log JSON handler swap for `--json-log`

Existing pattern in `pkg/cli/render.go::setupLogging` constructs a `charmlog.Handler`. For `--json-log`, swap to `slog.NewJSONHandler(os.Stderr, opts)` instead — the same writer, different handler. Wiring point: extend `setupLogging` to take a `jsonMode bool` parameter (or add `setupServerLogging` that branches on the flag).

```go
func setupServerLogging(debug, jsonMode bool) *slog.Logger {
    var handler slog.Handler
    if jsonMode {
        handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
            Level: levelFor(debug),
            // No ReplaceAttr customization — let slog defaults handle key naming
        })
    } else {
        // Existing charm-log path
        handler = charmlog.NewWithOptions(os.Stderr, charmlogOpts(debug))
    }
    logger := slog.New(handler)
    slog.SetDefault(logger)
    return logger
}
```

**Note:** the existing routedSlogLogger pattern (`buildRoutedSlogLoggerWithHandle`) wraps the slog handler with a `*progressHandler` that intercepts `event=*` records for Bazel-style rendering of `flow_complete` etc. For `skytime server`, the startup banner is structured slog records (`event=server_starting`, `event=registered_flows`) — these can route through the same progressHandler if it has a code path for `server` events, OR they can bypass the progress renderer and route directly to the slog handler. **Recommended:** Phase 7's startup events bypass the progress-handler routing (they're not flow events); they emit through `slog.Default().Info(...)` and the Default has the right handler installed.

### `*starlarkstruct.Struct` for `req` injection

The existing `pkg/bridge/struct.go::ToStarlarkStruct(map[string]any) (*starlarkstruct.Struct, error)` recursively converts Go maps to attribute-bearing structs with deterministic key order. This is exactly what's needed to inject `req.payload`, `req.headers`, etc. **Reuse verbatim** — no new conversion code required. Phase 7.1's HTTP receiver builds a `map[string]any{"payload": parsedBody, "headers": ...}` and calls `bridge.ToStarlarkStruct(...)` — same as `ctx` injection at workflow time.

### `go.starlark.net/lib/json` shape

`Module` is exposed as `*starlarkstruct.Module` with members `encode`, `decode`, `indent`. Wiring as `triggerTimeGlobals["json"] = json.Module` makes `req.payload`-style values JSON-serializable from inside lambdas. Identical pattern to the `tester` injection in `pkg/parser/globals.go::newParseTimeGlobals`.

```go
// In pkg/bridge/lambda_globals.go (extend with a new function):
import (
    starlarkjson "go.starlark.net/lib/json"
    starlarktime "go.starlark.net/lib/time"
)

var triggerTimeGlobals = func() starlark.StringDict {
    sd := make(starlark.StringDict, len(lambdaTimeGlobals)+2)
    for k, v := range lambdaTimeGlobals {
        sd[k] = v
    }
    sd["json"] = starlarkjson.Module
    sd["time"] = starlarktime.Module
    sd.Freeze()
    return sd
}()

// TriggerTimeGlobals returns a copy of the locked trigger-time globals.
func TriggerTimeGlobals() starlark.StringDict { /* same shape as LambdaTimeGlobals */ }
```

**D-07-04 rationale:** trigger lambdas run ONCE at HTTP ingress before `client.ExecuteWorkflow`; the result is the workflow input and is frozen at that point. Non-determinism (`time.now()`) is observably safe for trigger lambdas — they're not workflow code, they're ingress-time mappers. This MUST be documented in `pkg/bridge/doc.go` (and probably `pkg/parser/doc.go`) so future readers don't conflate trigger lambdas with workflow lambdas.

## Codebase Integration Map

### Parser surface

| Touchpoint | What changes | Depends on | Depended on by |
|------------|--------------|------------|----------------|
| `pkg/parser/builtins.go` | New `builtinTrigger` function (~80 LOC) following `builtinFlow` factory pattern: kwarg unpack via `starlark.UnpackArgs`, `wrapBuiltinError` for position-aware errors, Trigger node construction | `dag.Trigger`, `extension.TriggerSource`, `lambda_capture.go` | `globals.go::newParseTimeGlobals` registers it |
| `pkg/parser/globals.go` | One-line addition: `g["trigger"] = starlark.NewBuiltin("trigger", p.builtinTrigger)` | `builtinTrigger` exists | nothing — globals are the surface |
| `pkg/parser/lambda_capture.go` | Extend `captureLambda` (or add `captureLambdaWithArity`) to enforce arity-1 for trigger lambdas; existing free-var lint already runs | nothing | `builtinTrigger` |
| `pkg/parser/ctx_walk.go` | Generalize `findCtxAccesses` to `findFreeVarAccesses(src, filename, lambdaPos, freeVarName)` — change one parameter; the body already uses `firstParamName` so the rename is mostly cosmetic. Add a NEW caller `validateTriggerReqAccesses` in a new file (`pkg/parser/req_walk.go`?) that iterates triggers and dispatches | `findCtxAccesses` exists | `validateLambdaCtxAccesses` (existing); `validateTriggerReqAccesses` (new) |
| `pkg/parser/finalize.go` | Add cross-file trigger.FlowName resolution pass + req-walker pass to the existing `finalize()` chain. New entries between `resolveCallFlows` and `lintMixedIdempotency` (FlowName check) and after `validateLambdaCtxAccesses` (req-walker) | `p.flows`, new `p.triggers` | `parser.parse()` calls `finalize()` once |
| `pkg/parser/parser.go` | New fields: `triggers map[posKey]*dag.Trigger` (or similar shape — see § Pitfall 1); new accessors `Triggers() / TriggersInOrder()`. NewParser initializes empty map | nothing structural | `pkg/worker/boot.go` consumes via `Triggers()` |

### DAG surface

| Touchpoint | What changes | Why |
|------------|--------------|-----|
| `pkg/dag/trigger.go` (NEW, ~80 LOC) | `type Trigger struct { Pos, FlowName, Source TriggerSource, MapLambda *CapturedLambda, IdempotencyLambda *CapturedLambda, CredentialID string }` plus `Kind() string`, `Position()`, `nodeMarker()`, `Freeze()` — recursive into Source, MapLambda, IdempotencyLambda. **NOT** a Node satisfier in the strict workflow-body sense (Triggers don't appear inside flow.Body) but implements the same interface idiom for symmetry. **Open Q for planner:** does Trigger need `nodeMarker()` to be in the Node interface, or should it be a parallel sealed interface? Recommend separate; see § Pitfall 11. |
| `pkg/dag/marshal.go` | New `triggerJSON` shape: `{kind, FlowName, Source, MapLambdaID, IdempotencyLambdaID, CredentialID}`. The `Source` field uses Source.MarshalJSON (delegates to concrete TriggerSource's marshaler producing `{kind, config}`). **Pos excluded** by existing precedent (cross-machine stability). **Source.config NEVER carries Secret** — only credential ID. | Mirrors `actionRefJSON` shape. UnmarshalJSON handles both: kind discriminator → Source unmarshal-registry lookup |

### Extension surface

| Touchpoint | What changes | Why |
|------------|--------------|-----|
| `pkg/extension/trigger.go` (NEW, ~30 LOC) | `type TriggerSource interface { triggerSourceMarker(); ReqSchema() []string; MarshalJSON() ([]byte, error) }`. Sealed via `triggerSourceMarker()` unexported method. `ReqSchema()` returns the valid `req.<field>` names for parser validation. **Phase 7 ships zero concrete types** — first one in 7.1. | Parallel to `OperationSpec` for ops (D-07-07) |

### Registry + Worker surface

| Touchpoint | What changes | Why |
|------------|--------------|-----|
| `pkg/interpreter/registry.go` | New `TriggerRegistry` struct adjacent to `FlowRegistry`. Methods: `Register(t *dag.Trigger)`, `All() []*dag.Trigger` (sorted), `Freeze()`. Same `mu sync.RWMutex` + `frozen` discipline. **See § Pitfall 1 for shape decisions.** | Mirrors FlowRegistry concurrency model |
| `pkg/worker/boot.go::bootRegistry` | Signature change: `func bootRegistry(rootDir string, exts []extension.Extension) (*interpreter.FlowRegistry, *interpreter.TriggerRegistry, error)`. Inside the existing parse loop, after parsing each file, also call `p.Triggers()` and register each into the trigger registry with the file's content_hash. | One internal caller (`NewWorker`); easy refactor |
| `pkg/worker/worker.go::NewWorker` | Receives both registries from `bootRegistry`; stores both on `Worker`; new `Triggers() *interpreter.TriggerRegistry` accessor | Used by future HTTP router (7.1); Phase 7 only uses for startup banner |
| `pkg/worker/options.go::WorkerOptions` | New field `WorkerStopTimeout time.Duration`. Threaded into `sdkworker.Options.WorkerStopTimeout` inside NewWorker | Powers D-07-16 graceful drain |

### CLI surface

| Touchpoint | What changes | Why |
|------------|--------------|-----|
| `pkg/cli/server.go` (NEW, ~250 LOC) | `newServerCommand(cfg *config) *cobra.Command`. RunE: parse flags, range-check drain-timeout, connect Temporal, build worker (with WorkerStopTimeout), worker.Start, print banner, signal-loop with two-signal escalation, drain on signal. | New SERVER-01..03 surface. Single file; pkg/cli/server/ subdirectory NOT needed yet (see § Pitfall 10) |
| `pkg/cli/server_test.go` (NEW) | Behavioral tests using `testDrainHook` seam; banner format tests via captured slog buffer; flag validation tests | Phase 7 test layout |
| `pkg/cli/dev_server.go` → `pkg/cli/dev_temporal.go` | RENAME file. Inside, rename `newDevServerCommand` → `newDevTemporalCommand`. Cobra `Use: "dev-server"` → `Use: "dev-temporal"`. Long-text update. testRunningCmd seam variable name unchanged (purely internal). | D-07-21 hard rename |
| `pkg/cli/dev_server_test.go` → `pkg/cli/dev_temporal_test.go` | RENAME file. Update test names: `TestDevServerCmd_*` → `TestDevTemporalCmd_*`. Update internal references to `newDevServerCommand` → `newDevTemporalCommand`. **Critical:** the `dev_server.go` filename literal in `TestDevServerCmd_SignalForwardSourceSmoke` (currently `os.ReadFile("dev_server.go")`) MUST update to `dev_temporal.go` | D-07-21 |
| `pkg/cli/root.go` | One-line edit: `root.AddCommand(newDevServerCommand(cfg))` → `root.AddCommand(newDevTemporalCommand(cfg))`. Plus add: `root.AddCommand(newServerCommand(cfg))` | Subcommand registration |
| `pkg/cli/flags.go` | NO CHANGES for Phase 7 (server's flags are local to its cobra command, not persistent). Persistent flags (`--address` etc.) already work with the connection-flag reuse from D-07-19. | Localized server flags |
| `pkg/cli/connect.go` | NO CHANGES — `connectClient(cfg)` is reused verbatim per D-07-19 | — |
| `pkg/cli/render.go` | Possibly extend `setupLogging` to support `--json-log` mode (or add a parallel `setupServerLogging`). Recommend the parallel function so the existing routes for `validate`/`run` are untouched. | `--json-log` flag wiring |
| `pkg/cli/root_test.go` | Update tests asserting `dev-server` subcommand presence to check for `dev-temporal`. Add `server` subcommand presence test | D-07-21 + new subcommand |

### Bridge surface

| Touchpoint | What changes | Why |
|------------|--------------|-----|
| `pkg/bridge/lambda_globals.go` | New `triggerTimeGlobals` constant (lambdaTimeGlobals + json.Module + time.Module). New `TriggerTimeGlobals()` accessor mirroring `LambdaTimeGlobals()`. **Imports** add `go.starlark.net/lib/json` and `go.starlark.net/lib/time`. | D-07-01 lambda env |
| `pkg/bridge/doc.go` | New documentation block explaining D-07-04: trigger lambdas run at HTTP ingress (not workflow time), so non-determinism is safe; contrast with workflow-time lambdas | Documents the contract |

### Binary entrypoints

| Touchpoint | What changes | Why |
|------------|--------------|-----|
| `cmd/skytime/main.go` | One change: top doc-comment line `// skytime is the Skytime CLI binary: validate, run, and dev-server` → `validate, run, dev-temporal, and server`. Compile-time references to `dev-server` text in error messages (search for "validate, run, dev-server"). | D-07-21 doc + new server inheritance |
| `examples/http-github-webhook/cmd/extbin/main.go` | One change: top doc-comment subcommand list `extbin dev-server` → `extbin dev-temporal`. NO Go code changes (`server` subcommand inherited from `pkg/cli` automatically). Phase 7.4 owns the `lazyCredfileHandler` lift; unchanged here. | D-07-21 |

### Tests + CI

| Touchpoint | What changes | Why |
|------------|--------------|-----|
| `tests/firewall_cli_test.go` | NO CHANGES — pkg/cli is already on the cobra/charm-log allow-list, server.go inherits | — |
| `tests/firewall_credential_redaction_test.go` (NEW, ~80 LOC) | AST-walk `pkg/dag/trigger.go` and any source-extension package; reject `%+v` / `%#v` against `*dag.Trigger` or any `TriggerSource` concrete type. Pattern from `firewall_cli_test.go`. | D-07-10 |
| `tests/dev_server_grep_test.go` (NEW, ~20 LOC) | Walk all tracked files (excluding `.planning/`, `CHANGELOG.md`); grep for literal `dev-server`; fail if found. CI gate. | D-07-22 |
| `tests/docgen_drift_test.go` | Will FAIL after the rename until `go generate ./pkg/parser/` regenerates `docs/reference/builtins.md`. Run go generate; commit; test passes. | D-07-21 |
| `.github/workflows/scripts/walkthrough_smoke.sh` | Replace literal `temporal dev-server` text in cleanup logs (purely cosmetic) and any `extbin dev-server` references — but the smoke script currently shells `temporal server start-dev` directly, NOT through `extbin dev-server`. Verify the script's commands once and update only the affected lines. | D-07-21 |
| `examples/http-github-webhook/cmd/extbin/main_test.go` | One change: subcommand-list test `[]string{"validate", "run", "dev-server", "test"}` → `[]string{"validate", "run", "dev-temporal", "test", "server"}` | D-07-21 + new server |

### Documentation

| File | Change | Source line count |
|------|--------|-------------------|
| `README.md` | Replace `dev-server` → `dev-temporal` (~5 occurrences) | ~5 lines |
| `docs/getting-started.md` | Replace `dev-server` → `dev-temporal` (~5 occurrences) | ~5 lines |
| `docs/reference/cli.md` | Major: rename section `## skytime dev-server` → `## skytime dev-temporal`; rename `pkg/cli/dev_server.go` references → `pkg/cli/dev_temporal.go`; ADD a new `## skytime server` section. | ~80 lines added + ~10 edited |
| `docs/architecture.md` | One occurrence | 1 line |
| `docs/cli-binary.md` | One occurrence (`./my-skytime dev-server`) | 1 line |
| `docs/for-flow-authors/README.md` | Multiple occurrences | ~3 lines |
| `examples/README.md` | Multiple occurrences | ~5 lines |
| `examples/http-github-webhook/README.md` | Few occurrences | ~3 lines |
| `CLAUDE.md` (project-root) | Two occurrences | 2 lines |
| `docs/for-extension-developers/README.md` | Possibly 0 — verify with grep | TBD |

## Runtime State Inventory

The `dev-server` → `dev-temporal` rename is a string-rename refactor; runtime-state inventory applies.

| Category | Items Found | Action Required |
|----------|-------------|-----------------|
| Stored data | None — Skytime has no databases or persisted state. The `temporal server start-dev` subprocess uses an in-memory backend by default; no persistence in this scope. | None |
| Live service config | None — no n8n, no Datadog tags, no Tailscale. The Temporal server itself runs as a subprocess started by the user (or by `walkthrough_smoke.sh`); the rename does NOT touch any live service registration. | None |
| OS-registered state | None — no Task Scheduler / launchd / systemd entries. CI runs the binary directly. | None |
| Secrets/env vars | `SKYTIME_CREDFILE_PATH` (extbin) — name unchanged. `SKYTIME_TEMPORAL_*` (cli/flags.go) — names unchanged. **NO env vars reference `dev-server`** (verified by grep). | None |
| Build artifacts | `/tmp/extbin` (CI-built binary). Will rebuild from source on next CI run; the rename of `pkg/cli/dev_server.go` → `pkg/cli/dev_temporal.go` triggers a fresh compile. **No stale artifacts persist.** Local developers' `./skytime` and `./extbin` binaries become outdated until rebuilt — this is acceptable (no migration alias per D-07-21). | None — natural rebuild on next `go build` |

**The canonical question:** *After every file in the repo is updated, what runtime systems still have the old `dev-server` string cached, stored, or registered?* → **Nothing.** The rename is purely textual. The only persisted state would be cached `go build` output, which will be invalidated by source changes.

## Environment Availability

Phase 7 introduces NO new external dependencies for execution. The existing dependencies (Go 1.25+, optional `temporal` CLI for dev-temporal subcommand, network access) are unchanged.

| Dependency | Required By | Available | Version | Fallback |
|------------|-------------|-----------|---------|----------|
| Go toolchain (1.25+) | Build | ✓ | 1.25.8 (in go.mod) | — |
| `go.temporal.io/sdk` v1.42.0 | Worker drain via WorkerStopTimeout | ✓ | v1.42.0 | — |
| `temporal` CLI | `skytime dev-temporal` subcommand (renamed) | depends on user | — | Existing missing-binary install hint already present |
| Local Temporal server (for `skytime server` integration smoke) | Wave 4 integration tests | depends on user | — | Skip integration smoke if `temporal` CLI absent (existing pattern in `dev_server_test.go`) |

**Missing dependencies with no fallback:** None.

**Missing dependencies with fallback:** Only the `temporal` CLI; Phase 4's existing skip-when-absent pattern carries over to Phase 7's integration smokes.

## Common Pitfalls

### Pitfall 1: TriggerRegistry shape — not a simple FlowRegistry mirror

**What goes wrong:** A naive mirror that keys triggers by `(flow_name, content_hash)` (matching FlowRegistry) becomes awkward at the consumer side. The future HTTP router (Phase 7.1) iterates ALL triggers at server boot to mount HTTP handlers — there's no per-request lookup-by-flow-name. Cron (Phase 7.2) does the same. Keying by flow_name forces awkward iteration patterns and doesn't capture the "many triggers per flow, possibly per file version" reality.

**Why it happens:** FlowRegistry's keying is workflow-execution-driven (workflow start needs to load the exact flow version pinned to a workflow's content_hash). Triggers have a different lifecycle: registered once at boot, iterated once at HTTP-router-mount time, never looked up per-request.

**How to avoid:** Recommend two access patterns on TriggerRegistry:

```go
type TriggerRegistry struct {
    mu      sync.RWMutex
    frozen  bool
    // sorted slice for deterministic iteration. Sort key: (Source.Kind, FlowName, Pos)
    triggers []*dag.Trigger
    // contentHashByFile groups triggers by their owning file's content_hash, for
    // future hot-reload (post-v1) or per-file diagnostics. Phase 7 sets but doesn't
    // read this index; future phases consume it.
    byContentHash map[string][]*dag.Trigger
}

func (r *TriggerRegistry) All() []*dag.Trigger { ... } // returns sorted slice
func (r *TriggerRegistry) ByContentHash(hash string) []*dag.Trigger { ... }
func (r *TriggerRegistry) Register(contentHash string, t *dag.Trigger) error { ... }
func (r *TriggerRegistry) Freeze() { ... }
```

The `All()` accessor is what Phase 7's startup banner uses. Phase 7.1's HTTP router also calls `All()` and groups by Source kind for handler mounting. Phase 7.2's cron schedule reconciliation calls `All()` and filters by Source type-switch.

**Warning signs:** Tests requiring multi-key lookup are a smell — re-evaluate the shape if you find yourself wanting `Get(flowName, hash, kind, ...)`.

### Pitfall 2: Cross-file trigger.FlowName resolution at parse-finalize

**What goes wrong:** `bootRegistry` parses files in sorted order; flow_names are accumulated as files load. If `trigger(flow="check_user")` is parsed before `flow(name="check_user")`, the trigger captured the name but can't yet validate it.

**Why it happens:** The parser session's `flows` map is populated incrementally during the load chain. By the time `finalize()` runs (after all files are parsed), `p.flows` is complete.

**How to avoid:** Add a new finalize-pass `validateTriggerFlowNames` to `pkg/parser/finalize.go` AFTER `resolveCallFlows` (since both look at `p.flows`):

```go
func (p *Parser) finalize() error {
    if err := p.resolveCallFlows(); err != nil { return err }
    if err := p.validateTriggerFlowNames(); err != nil { return err }  // NEW
    if err := p.lintMixedIdempotency(); err != nil { return err }
    // ... existing chain ...
    if err := p.validateLambdaCtxAccesses(); err != nil { return err }
    if err := p.validateTriggerReqAccesses(); err != nil { return err } // NEW
    // ...
}

func (p *Parser) validateTriggerFlowNames() error {
    for _, trig := range p.triggers {
        if _, ok := p.flows[trig.FlowName]; !ok {
            knownFlows := sortedKeys(p.flows)
            return &dag.ParseError{
                Pos: trig.Pos,
                Msg: fmt.Sprintf("trigger references unknown flow %q; known flows: %v",
                    trig.FlowName, knownFlows),
            }
        }
    }
    return nil
}
```

`load()` order doesn't matter — finalize runs after all loads complete.

**Warning signs:** Test that defines `trigger(flow="X")` in one file and `flow(name="X")` in a second file (loaded via `load()`) and verifies the trigger validates. Without finalize-time check, this test fails.

### Pitfall 3: Generalizing pkg/parser/ctx_walk.go for req.<field>

**What goes wrong:** The current `findCtxAccesses` reads `firstParamName` from the AST and uses it as the free-var name for the visitor. Trigger lambdas use `req` as the conventional first param — but the validation layer needs to know the **valid attribute set per-source-kind**, which is dynamic (each TriggerSource declares its own `ReqSchema()`).

**Why it happens:** The state-schema check in `validateLambdaCtxAccesses` calls `state.has(acc.AttrName)` where `state` is built from `flow.Inputs` plus accumulated `OutputAlias` values — entirely flow-static. Trigger lambdas have a different schema source: `Trigger.Source.ReqSchema()`.

**How to avoid:** Minimum-invasive refactor — keep `findCtxAccesses` as-is (it's already parameterized by passing the lambda's first-param name). Split the validation into per-context callers:

```go
// In ctx_walk.go (no signature change — already takes lambdaPos):
func findFreeVarAccesses(src []byte, filename string, lambdaPos syntax.Position) ([]ctxAccess, error)
// (rename ctxAccess → freeVarAccess if you want; non-required)

// In state_schema.go (existing): unchanged
func (p *Parser) validateLambdaCtxAccesses() error { ... }

// In trigger_validate.go (NEW):
func (p *Parser) validateTriggerReqAccesses() error {
    for _, trig := range p.triggers {
        // For each source, get the valid req fields:
        validFields := trig.Source.ReqSchema()
        validSet := setFromSlice(validFields)

        // Validate map lambda:
        if err := p.checkLambdaReqAccesses(trig, trig.MapLambda, validSet, "map"); err != nil {
            return err
        }
        // Validate idempotency_key lambda:
        if err := p.checkLambdaReqAccesses(trig, trig.IdempotencyLambda, validSet, "idempotency_key"); err != nil {
            return err
        }
    }
    return nil
}

func (p *Parser) checkLambdaReqAccesses(trig *dag.Trigger, captured *dag.CapturedLambda,
    validFields map[string]struct{}, kwargName string) error {
    if captured == nil { return nil }
    src := p.fileBytes[captured.Pos.Filename()]
    accesses, err := findFreeVarAccesses(src, captured.Pos.Filename(), captured.Pos)
    if err != nil { return /* wrapped */ }
    for _, acc := range accesses {
        if _, ok := validFields[acc.AttrName]; !ok {
            return &dag.ValidationError{
                Pos: acc.Pos,
                Msg: fmt.Sprintf("trigger %s lambda: req.%s is not declared by source %s; valid fields: %v",
                    kwargName, acc.AttrName, trig.Source.Kind(), sortedKeys(validFields)),
            }
        }
    }
    return nil
}
```

**Verification:** Trace a concrete example through:
- `.star`: `trigger(flow="x", source=fakeWebhook(), map=lambda req: {"foo": req.payload.user_id})`
- After parse, `trig.MapLambda.Pos` points at the `lambda req:` token in the user file.
- `findFreeVarAccesses(src, filename, MapLambda.Pos)` re-parses the user file, finds the LambdaExpr at that position, walks its body for `*syntax.DotExpr` whose `X` is the first param (`req`). Returns `[{Pos: req.payload.NamePos, AttrName: "payload"}]`.
- `validateTriggerReqAccesses` checks `payload ∈ validFields` (FakeWebhook declares `["payload", "headers"]` so yes — pass).

The cached-file-bytes re-parse approach scales fine to two callers; performance is O(file_bytes) per finalize call, already paid by the existing ctx-walker. No new caching needed.

**Warning signs:** A test that writes `trigger(map=lambda req: {"x": req.payloud})` (typo) MUST surface an error pointing at `req.payloud` with `"valid fields: [headers, payload]"`. If the error fires at a different position OR mentions `req` as undefined (vs `req.payloud`), the visitor isn't matching positions correctly.

### Pitfall 4: Worker.Stop semantics in Temporal SDK v1.42.0

**What goes wrong:** Phase 7 might invent a custom drain loop ("poll workflow.ListWorkflow until empty") that's both unnecessary and wrong. The SDK already does this.

**Why it happens:** Documentation around `Stop()` is scattered; the timeout's role isn't obvious until you read `internal_worker_base.go::Stop()`.

**How to avoid:** Use `WorkerStopTimeout` directly. Threading through:

```go
// pkg/worker/options.go: NEW field
type WorkerOptions struct {
    // ... existing fields ...

    // WorkerStopTimeout is the SDK's graceful-stop duration. Worker.Stop()
    // closes the poll channel and blocks up to this duration waiting for
    // in-flight tasks to complete; uncompleted workflow tasks are NOT ACK'd
    // back to the server, so Temporal redispatches them on the next worker
    // start (this IS the durability story). Default 0 = SDK default behavior.
    WorkerStopTimeout time.Duration
}

// pkg/worker/worker.go: thread into sdkworker.Options
sdkOpts := sdkworker.Options{
    // ... existing fields ...
    WorkerStopTimeout: opts.WorkerStopTimeout,
}
```

Then in `pkg/cli/server.go`'s RunE, wire `WorkerStopTimeout: drainTimeout`. After SIGTERM, call `worker.Stop()` (which is sync.Once-wrapped already) — it blocks up to drainTimeout, then returns. Exit code from there.

**Warning signs:** A test that submits a slow workflow, sends SIGTERM, and verifies the server exits within drain-timeout +/- 1s. If the server hangs longer than drain-timeout + 1s, `WorkerStopTimeout` isn't threaded through correctly.

### Pitfall 5: Signal handling under load — `signal.NotifyContext` is single-shot

**What goes wrong:** The natural reach for "graceful drain on SIGTERM" is `signal.NotifyContext`, which delivers the first signal and resets. For Phase 7's two-signal escalation (D-07-15), this is wrong — the second signal would propagate to the default OS handler (immediate termination via SIGTERM, not via Skytime's controlled os.Exit(1)).

**Why it happens:** `signal.NotifyContext`'s docs say *"...you should still call stop() after ctx.Done() to allow a second Ctrl+C to forcefully terminate"* — meaning a second signal kills the process default-style, NOT through Skytime's drain-interrupted message path.

**How to avoid:** Use `signal.Notify` directly (NOT `signal.NotifyContext`) with a buffered channel size 2; spawn a goroutine that handles first vs second receive. See § Library/SDK Knowledge § Two-signal escalation pattern for the canonical shape. Test seam recommendation: `testDrainHook func(stage string)` package-private, set during behavioral tests.

**Warning signs:** Test that sends two SIGTERMs in succession (with a small delay) and verifies (a) first triggers drain message, (b) second triggers force-exit message + non-zero exit. If the second signal kills the process before Skytime prints the force-exit message, the wiring is wrong.

### Pitfall 6: --drain-timeout edge cases

**What goes wrong:** `pflag.Duration` accepts `--drain-timeout=0` and `--drain-timeout=-5s` as valid Go duration values. With `WorkerStopTimeout: 0`, the SDK uses its zero-default behavior (which means immediate termination of in-flight tasks — bad). With negative durations, behavior is undefined.

**Why it happens:** pflag delegates to `time.ParseDuration` which has no range opinion; SDK treats `WorkerStopTimeout: 0` as "no graceful timeout" (because zero is the field's zero value).

**How to avoid:** Add range validation in `RunE` BEFORE constructing the worker. Per D-07-17:

```go
const (
    minDrainTimeout = 1 * time.Second
    maxDrainTimeout = 1 * time.Hour
)

if drainTimeout < minDrainTimeout {
    return fmt.Errorf("--drain-timeout must be at least 1s; got %s (use 30s default if unsure)", drainTimeout)
}
if drainTimeout > maxDrainTimeout {
    slog.Default().Warn("--drain-timeout exceeds 1h; this likely indicates misconfiguration",
        "value", drainTimeout)
    // accept but warn
}
```

**Warning signs:** Test that `--drain-timeout=0s` produces a clean error message; `--drain-timeout=-5s` produces a clean error message; `--drain-timeout=2h` succeeds with a warning log (not error).

### Pitfall 7: --json-log flag scope and routing

**What goes wrong:** Naively swapping the slog handler for JSON might also intercept the `progressHandler` routing path used by `skytime run` for Bazel-style flow-progress rendering. If `skytime server` shares the wiring, JSON output gets garbled.

**Why it happens:** `pkg/cli/render.go::buildRoutedSlogLoggerWithHandle` wraps cfg.sdkLogger.Handler with a *progressHandler that dispatches `event=*` records. If `skytime server` reuses this wrapping, server startup events route through the progress renderer (which is designed for flow events).

**How to avoid:** `skytime server` should NOT use the routed logger — it doesn't run flows. Use plain `slog.Default()` (set up by `setupLogging`) directly. Add a `--json-log` branch in `setupLogging`:

```go
func setupServerLogging(debug, jsonMode bool) *slog.Logger {
    var h slog.Handler
    if jsonMode {
        h = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: levelFor(debug)})
    } else {
        h = charmlog.NewWithOptions(os.Stderr, charmlogOpts(debug))
    }
    logger := slog.New(h)
    slog.SetDefault(logger)
    return logger
}
```

Then in `newServerCommand`'s RunE: `logger := setupServerLogging(cfg.debug, jsonLog)` — overrides the persistent setupLogging that ran in PreRunE. Bypasses the progress-handler routing entirely.

**Note:** the SDK client's logger (`cfg.sdkLogger`) is independent — `--verbose` controls SDK chatter visibility regardless of `--json-log`. Server emits its own structured events; SDK noise routes through cfg.sdkLogger as before.

**Warning signs:** Test `--json-log` output is parseable as JSON line-by-line (each line `json.Unmarshal`s without error). Test `--json-log=false` (default) output contains charm-log-style `[skytime]` banner.

### Pitfall 8: dev-server → dev-temporal — exhaustive grep already done

**What goes wrong:** Missing one occurrence leaves a stale `dev-server` reference; CI grep gate (D-07-22) catches it but only in a CI failure cycle.

**Why it happens:** Renames are mechanical but human attention is fallible — easy to overlook a comment, doc, or test fixture.

**How to avoid:** Use the canonical grep result (verified at research time):

```bash
git grep -F "dev-server" -- ':!.planning/' ':!CHANGELOG.md'
git grep -i "dev_server" -- ':!.planning/'
```

Categorize touchpoints (already mapped in § Codebase Integration Map):
- **Code (rename):** `pkg/cli/dev_server.go` (file rename), `pkg/cli/dev_server_test.go` (file rename), `pkg/cli/root.go` (subcommand registration line), `pkg/cli/root_test.go` (subcommand presence test), `cmd/skytime/main.go` (top doc comment), `examples/http-github-webhook/cmd/extbin/main.go` (top doc comment), `examples/http-github-webhook/cmd/extbin/main_test.go` (subcommand list test)
- **Docs (text update):** `README.md`, `docs/getting-started.md`, `docs/reference/cli.md` (largest update — entire `## skytime dev-server` section), `docs/architecture.md`, `docs/cli-binary.md`, `docs/for-flow-authors/README.md`, `examples/README.md`, `examples/http-github-webhook/README.md`, `CLAUDE.md` (project-root)
- **CI (text update):** `.github/workflows/scripts/walkthrough_smoke.sh` (cleanup-message strings only — script doesn't actually invoke `dev-server` subcommand; uses `temporal server start-dev` directly)
- **Auto-regenerated:** `docs/reference/builtins.md` via `go generate ./pkg/parser/` after the `// skytime:doc` markers update (not in scope for Phase 7's rename — `dev-server` isn't a Starlark builtin)

**The CI grep gate (D-07-22) is the final safety net** — runs after the rename PR merges and fails CI if any tracked file (excluding `.planning/`, `CHANGELOG.md`) contains `dev-server`. New test file: `tests/dev_server_grep_test.go`.

**Warning signs:** docgen drift test (`tests/docgen_drift_test.go`) failing surprisingly after the rename. Cause: probably the `cli.md` rename leaked into `builtins.md` regeneration (it shouldn't, since builtins.md only covers Starlark builtins, not CLI subcommands — but verify).

### Pitfall 9: Test infrastructure for `skytime server` — in-process not subprocess

**What goes wrong:** Copying `dev_server_test.go`'s `testRunningCmd atomic.Pointer[exec.Cmd]` pattern to `skytime server` won't work — `skytime server` runs the worker in-process, not as a subprocess. There's no `exec.Cmd` to point at.

**Why it happens:** `dev-server`/`dev-temporal` IS a subprocess wrapper (it shells `temporal server start-dev`). `skytime server` is the long-running process itself.

**How to avoid:** Use a different test seam for behavioral coverage:

**Option A (recommended): testDrainHook func**
```go
// In pkg/cli/server.go (production path):
var testDrainHook func(stage string) // package-private; nil in production

func newServerCommand(cfg *config) *cobra.Command {
    return &cobra.Command{
        // ...
        RunE: func(cmd *cobra.Command, args []string) error {
            // ...
            if testDrainHook != nil { testDrainHook("worker-started") }
            // wait for signal
            <-sigCh
            if testDrainHook != nil { testDrainHook("drain-start") }
            // drain
            // ...
        },
    }
}

// In server_test.go:
func TestServerCmd_DrainOnSignal(t *testing.T) {
    var stages []string
    var stagesMu sync.Mutex
    testDrainHook = func(s string) { stagesMu.Lock(); stages = append(stages, s); stagesMu.Unlock() }
    defer func() { testDrainHook = nil }()

    // Build command with mocked client (testClientFactory captures choice).
    // Run RunE in a goroutine; wait for "worker-started" stage; dispatch signal
    // to current process; assert drain-start stage observed; assert RunE returns.
}
```

**Option B (subprocess test): exec the binary**
For end-to-end CI smoke (slower, less granular). Only use for one or two integration smokes, not for unit-level coverage.

**Recommendation:** Option A for unit tests; one Option B integration smoke that spawns `go run ./cmd/skytime server --rootdir=...` with a tmpdir of one `.star` file, sends SIGTERM, asserts process exits within drain-timeout + 1s.

**Warning signs:** Test that asserts the drain-completes-within-timeout property. If it relies on `testRunningCmd atomic.Pointer` pattern (subprocess seam), it's wrong — `skytime server` has no subprocess.

### Pitfall 10: Pkg layout — single file vs subpackage

**What goes wrong:** Anticipating Phase 7.1's HTTP routing + Phase 7.3's dashboard templates by creating `pkg/cli/server/` subdirectory now adds ceremony without payoff. Or, deferring the subpackage and finding Phase 7.1 needs to refactor anyway.

**Why it happens:** The temptation to "design ahead" for known future expansion vs the pull toward minimal Phase 7 scope.

**How to avoid:** **Single file `pkg/cli/server.go` for Phase 7.** Justification:
- Phase 7's content is ~250 LOC: command builder + flag wiring + signal-loop + drain logic + startup banner. All cohesive.
- Phase 7.1 adds HTTP routing — that's where the subpackage refactor naturally lands. The 7.1 plan WILL touch this file regardless; a subpackage refactor in 7.1 is one task, not a phase-spanning concern.
- Phase 7.3's dashboard templates need `*.html` resources via `embed.FS` — `pkg/cli/server/web/` is the natural home, but only after 7.1 has the HTTP scaffolding.
- Premature subpackage creation in Phase 7 leaves an empty/near-empty package that adds confusion ("what's in pkg/cli/server vs pkg/cli?").

**Warning signs:** Plan for Phase 7 proposing `pkg/cli/server/cmd.go` + `pkg/cli/server/router.go` (empty placeholder) + `pkg/cli/server/dashboard.go` (empty placeholder) — that's anticipating instead of doing.

### Pitfall 11: Trigger as DAG node — sealed seal vs separate type

**What goes wrong:** Adding `Trigger` to the `dag.Node` interface (with `nodeMarker()`) implies Triggers can appear inside flow.Body — they can't. They're top-level declarations registered separately.

**Why it happens:** Convenience desire to reuse the existing Node infrastructure (Position, Kind, Freeze).

**How to avoid:** Make Trigger satisfy a similar shape but NOT the Node interface:

```go
// In pkg/dag/trigger.go:
type Trigger struct {
    Pos               syntax.Position
    FlowName          string
    Source            TriggerSource // sealed via extension.TriggerSource
    MapLambda         *CapturedLambda
    IdempotencyLambda *CapturedLambda
    CredentialID      string
    frozen            bool
}

func (t *Trigger) Position() syntax.Position { return t.Pos }
func (t *Trigger) Kind() string              { return "Trigger" }
func (t *Trigger) Freeze() {
    if t.frozen { return }
    t.frozen = true
    // Source has its own Freeze() (TriggerSource interface includes Freeze):
    if t.Source != nil { /* freeze if source supports it */ }
    // Lambdas freeze via captured.Fn.Freeze()
}

// NOT Node — no nodeMarker(). Trigger is a top-level declaration, not a body node.
```

**Justification:** flow.Body walkers (interpreter, validators, finalize passes) iterate `[]Node`. If Trigger satisfies Node, every walker has to add a defensive `case *dag.Trigger:` that errors ("trigger should never appear here"). Cleaner: Trigger is a separate type, only ever referenced from `parser.Triggers()` and `interpreter.TriggerRegistry`.

**Warning signs:** A test that writes `flow(steps=[trigger(...)])` and expects a clean parse error. Without separation, the error is lost in walker noise; with separation, `trigger(...)` returns `*dag.Trigger` and `convertNodeList` rejects it because it's not a `*nodeValue`.

### Pitfall 12: Backward compat — bootRegistry signature change scope

**What goes wrong:** Underestimating the rename impact, or breaking external callers.

**Why it happens:** "(*FlowRegistry, error) → (*FlowRegistry, *TriggerRegistry, error)" sounds like it could break consumers.

**How to avoid:** Verified — `git grep -F "bootRegistry"` returns ONE production caller: `pkg/worker/worker.go::NewWorker` (internal). All other matches are in `.planning/`'s historic plan files. **Zero external impact.**

`NewWorker` change is also small: `Worker` gains a new field `triggers *interpreter.TriggerRegistry` and an accessor `Triggers()`. WorkerOptions doesn't need a new field (TriggerRegistry comes from boot internally).

```go
// pkg/worker/worker.go (after change):
type Worker struct {
    sdk      sdkworker.Worker
    registry *interpreter.FlowRegistry
    triggers *interpreter.TriggerRegistry  // NEW
    opts     WorkerOptions
    stopOnce sync.Once
}

func (w *Worker) Triggers() *interpreter.TriggerRegistry { return w.triggers }  // NEW

// Inside NewWorker:
flowReg, trigReg, err := bootRegistry(opts.RootDir, opts.Extensions)
// ...
return &Worker{sdk: sdkW, registry: flowReg, triggers: trigReg, opts: opts}, nil
```

**Warning signs:** None expected; the refactor is local to pkg/worker.

### Pitfall 13: Validation strategy — Tier 1 dominates Phase 7

**What goes wrong:** Investing in Tier 3 (E2E .star tests) for Phase 7 is premature — there are no real source factories yet. A `_test.star` referencing `fakeTriggerSource` requires bridge plumbing through `parser.WithTestMode()` which isn't built for this purpose.

**Why it happens:** Phase 5's Tier-3 harness (`tester.{workflow, mock_action, run}`) sets a high bar; the temptation is to mirror it for triggers.

**How to avoid:** Phase 7's primary tier is **Tier 1 (parse-time)** — every trigger primitive's behavior surfaces at parse-time. Tier 2 (Go unit) covers TriggerRegistry, JSON round-trip, signal-handling test seams. Tier 3 not needed until Phase 7.1 ships `github.WebhookSource` (a real source whose ingress can be exercised end-to-end). CLI-level tests cover `skytime server` lifecycle (Pitfall 9 Option A).

See § Validation Architecture for the concrete test plan.

## Code Examples

### Example 1: builtinTrigger structure (Pattern based on builtinFlow)

```go
// In pkg/parser/builtins.go (or a new pkg/parser/trigger.go for organization).
//
// Reference: pkg/parser/builtins.go::builtinFlow lines 119-193.

func (p *Parser) builtinTrigger(thread *starlark.Thread, fn *starlark.Builtin,
    args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
    var (
        flowName       string
        sourceVal      starlark.Value
        mapVal         starlark.Value
        idempotencyVal starlark.Value
        credentialID   string
    )
    if err := starlark.UnpackArgs("trigger", args, kwargs,
        "flow", &flowName,
        "source", &sourceVal,
        "map", &mapVal,
        "idempotency_key", &idempotencyVal,
        "credential?", &credentialID,
    ); err != nil {
        return nil, p.wrapBuiltinError("trigger", thread, err)
    }
    pos := callerPosition(thread)

    // Type-check Source
    src, ok := sourceVal.(extension.TriggerSource)
    if !ok {
        return nil, &dag.ParseError{
            Pos: pos,
            Msg: fmt.Sprintf("trigger.source: expected TriggerSource, got %s", sourceVal.Type()),
        }
    }

    // Capture lambdas with arity-1 enforcement (D-07-05 layer 2)
    mapLambda, err := p.captureLambdaWithArity(thread, "map", mapVal, 1)
    if err != nil {
        return nil, err
    }
    idempLambda, err := p.captureLambdaWithArity(thread, "idempotency_key", idempotencyVal, 1)
    if err != nil {
        return nil, err
    }

    trig := &dag.Trigger{
        Pos:               pos,
        FlowName:          flowName,
        Source:            src,
        MapLambda:         mapLambda,
        IdempotencyLambda: idempLambda,
        CredentialID:      credentialID,
    }

    // Register in parser session — keyed by Pos (unique per call site)
    p.triggers[posKey(pos)] = trig

    // No return value — trigger() is a top-level statement, like flow().
    return starlark.None, nil
}
```

### Example 2: TriggerSource sealed interface

```go
// pkg/extension/trigger.go (NEW)

// TriggerSource is a sealed marker interface — only concrete types in
// extension packages can implement it. Phase 7 ships zero concrete types;
// the first one (`github.WebhookSource`) lands in Phase 7.1.
//
// Sealed via the unexported triggerSourceMarker() method, mirroring
// dag.Node's seal pattern (pkg/dag/node.go).
type TriggerSource interface {
    // Kind returns the discriminator for JSON marshaling and for the
    // startup banner ("github.webhook" → "X" for "github.webhook → X").
    Kind() string

    // ReqSchema returns the valid req.<field> attribute names available
    // to the trigger's map and idempotency_key lambdas. Used by the
    // parser-time req-walker (pkg/parser/req_walk.go) to surface
    // "req.payloud" → "did you mean: req.payload?" errors.
    ReqSchema() []string

    // MarshalJSON produces the {kind, config} envelope. config MUST
    // contain credential ID strings only — never resolved Secret values
    // (D-07-09). Implementations are responsible for stripping any
    // Secret-typed fields from the marshaled config.
    MarshalJSON() ([]byte, error)

    // triggerSourceMarker is the seal — unexported, callable only from
    // pkg/extension or sub-packages.
    triggerSourceMarker()
}
```

### Example 3: TriggerRegistry shape

```go
// pkg/interpreter/registry.go (extend with TriggerRegistry)

type TriggerRegistry struct {
    mu       sync.RWMutex
    frozen   bool
    triggers []*dag.Trigger // sorted slice for deterministic iteration
    byHash   map[string][]*dag.Trigger // groups by owning file's content_hash
}

func NewTriggerRegistry() *TriggerRegistry {
    return &TriggerRegistry{byHash: map[string][]*dag.Trigger{}}
}

func (r *TriggerRegistry) Register(contentHash string, t *dag.Trigger) error {
    if t == nil { return errors.New("Register: trigger required") }
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.frozen { return ErrRegistryFrozen }
    r.triggers = append(r.triggers, t)
    r.byHash[contentHash] = append(r.byHash[contentHash], t)
    return nil
}

func (r *TriggerRegistry) Freeze() {
    r.mu.Lock()
    defer r.mu.Unlock()
    // Sort triggers by (Source.Kind(), FlowName, Pos) for deterministic order.
    sort.SliceStable(r.triggers, func(i, j int) bool {
        a, b := r.triggers[i], r.triggers[j]
        if a.Source.Kind() != b.Source.Kind() {
            return a.Source.Kind() < b.Source.Kind()
        }
        if a.FlowName != b.FlowName {
            return a.FlowName < b.FlowName
        }
        // tiebreaker: position
        return a.Pos.String() < b.Pos.String()
    })
    r.frozen = true
}

func (r *TriggerRegistry) All() []*dag.Trigger {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make([]*dag.Trigger, len(r.triggers))
    copy(out, r.triggers)
    return out
}
```

### Example 4: skytime server signal loop

```go
// pkg/cli/server.go (skeleton)

var testDrainHook func(stage string) // package-private test seam

func newServerCommand(cfg *config) *cobra.Command {
    var (
        rootdir       string
        taskQueue     string
        addr          string
        credfilePath  string
        drainTimeout  time.Duration
        jsonLog       bool
    )

    cmd := &cobra.Command{
        Use:   "server",
        Short: "Run a long-lived Skytime worker (drain-on-SIGTERM)",
        Long:  "Boots a Temporal worker against the configured task queue, registers flows + triggers from --rootdir, and stays up until SIGTERM/SIGINT.",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Validate flag ranges
            if drainTimeout < time.Second {
                return fmt.Errorf("--drain-timeout must be ≥ 1s; got %s", drainTimeout)
            }
            if drainTimeout > time.Hour {
                slog.Default().Warn("--drain-timeout exceeds 1h", "value", drainTimeout)
            }

            // Set up logging (charm-log default; JSON if --json-log)
            logger := setupServerLogging(cfg.debug, jsonLog)

            // Connect Temporal
            c, err := connectClient(cfg)
            if err != nil { return /* ... */ }
            defer c.Close()

            // Build worker with WorkerStopTimeout = drainTimeout
            w, err := worker.NewWorker(c, worker.WorkerOptions{
                RootDir:           rootdir,
                TaskQueue:         taskQueue,
                Extensions:        cfg.exts,
                CredentialHandler: cfg.credHandler,
                Logger:            cfg.sdkLogger,
                WorkerStopTimeout: drainTimeout,
            })
            if err != nil { return /* ... */ }

            // Print startup banner (sorted flows + triggers)
            printStartupBanner(logger, w, rootdir, taskQueue, addr)
            if addr != ":8080" || hasAddrFlag(cmd) {
                logger.Info("note: --addr has no effect until Phase 7.1 ships the HTTP receiver", "addr", addr)
            }

            if err := w.Start(); err != nil { return /* ... */ }
            if testDrainHook != nil { testDrainHook("worker-started") }
            logger.Info("worker started; SIGTERM/SIGINT to drain", "drain-timeout", drainTimeout)

            // Two-signal escalation
            sigCh := make(chan os.Signal, 2)
            signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
            defer signal.Stop(sigCh)

            <-sigCh
            if testDrainHook != nil { testDrainHook("drain-start") }
            logger.Info("draining (second signal forces immediate exit)")

            done := make(chan struct{})
            go func() {
                w.Stop() // sync.Once-wrapped; blocks up to WorkerStopTimeout
                close(done)
            }()

            drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
            defer cancel()

            select {
            case <-done:
                if testDrainHook != nil { testDrainHook("drain-complete") }
                logger.Info("drain complete")
                return nil
            case <-sigCh:
                if testDrainHook != nil { testDrainHook("drain-forced") }
                logger.Error("drain interrupted by second signal; forcing exit")
                os.Exit(1)
                return nil // unreachable
            case <-drainCtx.Done():
                if testDrainHook != nil { testDrainHook("drain-timeout") }
                logger.Error("drain-timeout exceeded; restart resumes from event history")
                return errSilent // exit 1 via cobra
            }
        },
    }

    cmd.Flags().StringVar(&rootdir, "rootdir", "", "directory containing .star files (required)")
    cmd.Flags().StringVar(&taskQueue, "task-queue", "skytime", "Temporal task queue")
    cmd.Flags().StringVar(&addr, "addr", ":8080", "HTTP listener address (Phase 7.1+; ignored in Phase 7)")
    cmd.Flags().StringVar(&credfilePath, "credfile", "", "credential file path (Phase 7.4+; no-op when no handler wired)")
    cmd.Flags().DurationVar(&drainTimeout, "drain-timeout", 30*time.Second,
        "max time to wait for in-flight workflows to complete on SIGTERM/SIGINT (1s..1h)")
    cmd.Flags().BoolVar(&jsonLog, "json-log", false, "emit logs as JSON instead of charm-log Bazel-style")
    _ = cmd.MarkFlagRequired("rootdir")
    return cmd
}
```

## State of the Art

| Old approach | Current approach | When changed | Impact |
|--------------|------------------|--------------|--------|
| Custom drain loops polling `client.ListWorkflow` until empty | `WorkerStopTimeout` field on `worker.Options` (SDK does the wait internally) | SDK has had this field for years (verified in v1.42.0 source); Skytime hasn't used it because v1.42.0 only ran one-shot `skytime run` workers | Phase 7 wires it directly; no custom drain code needed |
| `signal.NotifyContext` for "graceful drain" | `signal.NotifyContext` works for single-signal cases; multi-signal escalation needs `signal.Notify` directly | Go 1.16+ stable | D-07-15 two-signal requirement → must use `signal.Notify` directly |
| Default `pflag.Duration` accepts any duration | Range validation in RunE | Always (pflag never grew range support) | Skytime adds 1s..1h check per D-07-17 |
| Pre-1.0 `dev-server` subcommand naming | `dev-temporal` (clearer: it's Temporal's dev-server, not Skytime's) | This phase | Hard rename, no alias (CLI-13) |

**Deprecated/outdated knowledge:**
- The earlier draft plan and CONTEXT.md drafts mentioned creating `triggers.github_webhook` under a separate `triggers` extension package. **REJECTED** by D-07-08 — sources live under their owning extension's namespace (`github.webhook(...)`).
- `signal.NotifyContext` as the canonical signal pattern — works for many cases but not for two-signal escalation; documented in § Pitfall 5.

## Open Questions

### 1. Should `dag.Trigger` satisfy `dag.Node`?

**What we know:** Trigger has Position(), Kind(), needs Freeze(). Node interface requires `nodeMarker()`.

**What's unclear:** Pure design choice. If Trigger satisfies Node, downstream walkers must defensively reject `case *dag.Trigger` everywhere (since Triggers shouldn't appear in flow.Body). If Trigger is a separate type, walkers don't need defensive cases.

**Recommendation:** **Separate type** (NOT a Node satisfier). Triggers are top-level declarations registered in TriggerRegistry, never inside flow.Body. See § Pitfall 11 for full reasoning. Resolved: Pitfall 11.

### 2. TriggerRegistry concurrent registration during boot — same RWMutex pattern as FlowRegistry?

**What we know:** FlowRegistry uses `sync.RWMutex` + `frozen` boolean; bootRegistry serializes registration in a single goroutine.

**What's unclear:** Triggers might be registered out-of-order (parser session order vs filesystem walk order), which could matter for sort stability.

**Recommendation:** Same pattern — `sync.RWMutex` + `frozen`, sorted on Freeze() rather than per-insertion. Sort key `(Source.Kind, FlowName, Pos)`. Phase 7 boot is single-goroutine; the lock is for boot-time correctness only. Resolved: Example 3.

### 3. Where does TriggerSource unmarshal-registry live?

**What we know:** Each concrete TriggerSource implements MarshalJSON producing `{kind, config}`. Round-trip needs a kind-keyed map of unmarshal funcs.

**What's unclear:** Phase 7 ships zero concrete sources, so the registry is empty. Phase 7.1's first source registers itself. Where's the registry?

**Recommendation:** `pkg/dag/trigger_unmarshal.go` (or `pkg/extension/trigger_unmarshal.go`) with:
```go
var unmarshalers = map[string]func([]byte) (TriggerSource, error){}
func RegisterTriggerSourceUnmarshaler(kind string, fn func([]byte) (TriggerSource, error)) {
    unmarshalers[kind] = fn
}
```
Sources register in their package's `init()` OR explicitly during extension `Initialize()` (latter preferred for explicit lifecycle). Phase 7's test stub `fakeTriggerSource` registers in test setup. Phase 7.1's `github.WebhookSource` registers in its package's init or extension Initialize.

This is OPEN for the planner to decide based on how sources are organized in Phase 7.1; Phase 7's surface is the registry function + the test stub.

### 4. Should `--credfile` flag emit a clear error when no handler is wired?

**What we know:** D-07-19 says `--credfile` is "no-op when no handler wired". Phase 7.4 owns the lift.

**What's unclear:** Should the no-op silently succeed, or warn, or hard-error?

**Recommendation:** **Hard error** with a clear message: `"--credfile is set but this binary has no credential handler wired; rebuild with cli.WithCredentialHandler() or omit --credfile (see docs/cli-binary.md)"`. Silent no-op risks the user thinking credentials are loading when they aren't. Resolved-as-recommendation; planner may revise.

## Validation Architecture

> Phase config has `nyquist_validation: true` (default; not explicitly disabled).

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `github.com/stretchr/testify` (per CLAUDE.md) |
| Config file | none — `go test` defaults |
| Quick run command | `go test ./pkg/parser/... ./pkg/dag/... ./pkg/extension/... -count=1 -race` |
| Full suite command | `go test ./... -count=1 -race` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| TRIG-01 | `trigger(...)` parses without I/O | Tier 1 unit | `go test ./pkg/parser/ -run TestBuiltinTrigger -count=1` | ❌ Wave 0 — `pkg/parser/trigger_test.go` |
| TRIG-01 | Captured Trigger has correct fields | Tier 2 unit | `go test ./pkg/parser/ -run TestBuiltinTrigger_Fields` | ❌ Wave 0 |
| TRIG-02 | Extensions return TriggerSource opaquely | Tier 2 unit | `go test ./pkg/extension/ -run TestTriggerSource_Sealed` | ❌ Wave 0 — `pkg/extension/trigger_test.go` |
| TRIG-03 | dag.Trigger JSON round-trip | Tier 2 unit | `go test ./pkg/dag/ -run TestTrigger_MarshalRoundTrip` | ❌ Wave 0 — `pkg/dag/trigger_test.go` |
| TRIG-03 | Trigger.Freeze recursion | Tier 2 unit | `go test ./pkg/dag/ -run TestTrigger_Freeze` | ❌ Wave 0 |
| TRIG-04 | Unknown flow → position-aware error | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_UnknownFlow` | ❌ Wave 0 |
| TRIG-04 | Source not a TriggerSource → error | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_BadSource` | ❌ Wave 0 |
| TRIG-04 | req.<field> typo → error with valid-list | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_ReqAttrTypo` | ❌ Wave 0 |
| TRIG-04 | Lambda arity wrong → error | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_BadArity` | ❌ Wave 0 |
| TRIG-04 | Mutable closure → existing free-var lint | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_MutableClosure` | ❌ Wave 0 |
| TRIG-05 | bootRegistry registers flows + triggers | Tier 2 integration | `go test ./pkg/worker/ -run TestBootRegistry_RegistersTriggers` | ❌ Wave 0 |
| TRIG-05 | Mixed valid + test files: only non-test files registered | Tier 2 integration | `go test ./pkg/worker/ -run TestBootRegistry_SkipsTestFiles` | ✅ existing skip-`*_test.star` test extends |
| SERVER-01 | server subcommand registers flags | Tier 2 unit | `go test ./pkg/cli/ -run TestServerCmd_Flags` | ❌ Wave 0 — `pkg/cli/server_test.go` |
| SERVER-01 | server uses connectClient routing | Tier 2 unit | `go test ./pkg/cli/ -run TestServerCmd_ConnectClient` | ❌ Wave 0 |
| SERVER-02 | Drain on SIGTERM completes within timeout | Tier 2 behavioral via testDrainHook | `go test ./pkg/cli/ -run TestServerCmd_DrainOnSIGTERM` | ❌ Wave 0 |
| SERVER-02 | Drain timeout expiry → exit 1 + log | Tier 2 behavioral via testDrainHook | `go test ./pkg/cli/ -run TestServerCmd_DrainTimeoutExpiry` | ❌ Wave 0 |
| SERVER-02 | Second signal forces immediate exit | Tier 2 behavioral via testDrainHook | `go test ./pkg/cli/ -run TestServerCmd_SecondSignalForceExit` | ❌ Wave 0 |
| SERVER-02 | --drain-timeout=0 → flag error | Tier 2 unit | `go test ./pkg/cli/ -run TestServerCmd_DrainTimeoutRangeCheck` | ❌ Wave 0 |
| SERVER-03 | Startup banner sorted lexicographically | Tier 2 unit (slog buffer capture) | `go test ./pkg/cli/ -run TestServerCmd_BannerSorted` | ❌ Wave 0 |
| SERVER-03 | --json-log emits JSON | Tier 2 unit | `go test ./pkg/cli/ -run TestServerCmd_JSONLog` | ❌ Wave 0 |
| CLI-13 | dev-temporal subcommand exists | Tier 2 unit | `go test ./pkg/cli/ -run TestRoot_HasDevTemporalSubcommand` | ✅ existing root_test.go updates |
| CLI-13 | No file contains `dev-server` literal | Tier 2 firewall | `go test ./tests/ -run TestNoDevServerLiteralRemains` | ❌ Wave 0 — D-07-22 grep gate |
| CLI-13 | docgen drift test passes after rename | Tier 2 firewall | `go test ./tests/ -run TestDocgenDrift` | ✅ existing — runs go generate then compares |
| D-07-10 | Credential redaction firewall (AST-walking %+v / %#v) | Tier 2 firewall | `go test ./tests/ -run TestCredentialRedactionFirewall` | ❌ Wave 0 — `tests/firewall_credential_redaction_test.go` |

### Sampling Rate
- **Per task commit:** `go test ./pkg/parser/... ./pkg/dag/... ./pkg/extension/... -count=1 -race` (~5s)
- **Per wave merge:** `go test ./... -count=1 -race` (~30s)
- **Phase gate:** Full suite green + manual smoke `go run ./cmd/skytime server --rootdir=examples/http-github-webhook/ --task-queue=demo` (requires running Temporal dev server) before `/gsd:verify-work`

### Test Corpus / Fixtures Needed (Wave 0)

`pkg/parser/testdata/triggers/`:
- `valid.star` — `trigger(flow="check", source=fakeWebhook(), map=lambda req: {"x": req.payload.foo}, idempotency_key=lambda req: req.headers.delivery_id)` against a stub source declaring `req.payload`, `req.headers`. Parses clean.
- `typo.star` — same as valid but `req.payloud.foo`. Errors with valid-field list.
- `bad_arity.star` — `lambda req, headers: ...` (two positionals). Errors at parse time.
- `unknown_flow.star` — `trigger(flow="missing", ...)` with no matching flow. Errors at parse-finalize.
- `mutable_closure.star` — closes over a mutable list (existing free-var lint catches; reused fixture pattern from Phase 1).
- `not_a_source.star` — `trigger(source="just a string", ...)`. Errors with "expected TriggerSource".
- `cross_file_flow.star` + `cross_file_trigger.star` (uses `load()`) — flow declared in one, trigger in another. Validates cross-file resolution (D-07-12).
- `duplicate_warn.star` — two byte-identical triggers. Parses clean but emits boot-time warning (D-07-13).

### Wave 0 Gaps
- [ ] `pkg/parser/trigger_test.go` — covers TRIG-01, TRIG-04
- [ ] `pkg/parser/req_walk.go` + `pkg/parser/req_walk_test.go` — covers TRIG-04 req-attribute walk
- [ ] `pkg/parser/testdata/triggers/*.star` — corpus fixtures
- [ ] `pkg/dag/trigger.go` + `pkg/dag/trigger_test.go` — covers TRIG-03
- [ ] `pkg/extension/trigger.go` + `pkg/extension/trigger_test.go` — covers TRIG-02
- [ ] `pkg/extension/trigger_test.go::fakeTriggerSource` — test stub used across packages (export from extension test_helpers or duplicate per package)
- [ ] `pkg/interpreter/registry_test.go` extensions — TriggerRegistry tests
- [ ] `pkg/worker/boot_test.go` extensions — covers TRIG-05 trigger registration during boot
- [ ] `pkg/cli/server.go` + `pkg/cli/server_test.go` — covers SERVER-01..03
- [ ] `tests/firewall_credential_redaction_test.go` — covers D-07-10
- [ ] `tests/dev_server_grep_test.go` — covers D-07-22
- [ ] Integration smoke (Wave 4): `go run ./cmd/skytime server` against `temporal server start-dev`, send SIGTERM, assert exit code + log output (skip when `temporal` CLI absent)

## Recommended Plan Decomposition

Five waves, each producing testable artifacts. Parallelism is possible within waves but waves run sequentially.

### Wave 1: DAG node + Extension TriggerSource (pure data types)

**Scope:**
- `pkg/extension/trigger.go` — `TriggerSource` sealed interface + `triggerSourceMarker()` + `ReqSchema()` + JSON contract
- `pkg/dag/trigger.go` — `dag.Trigger` struct + Kind() + Position() + Freeze() (NOT a Node satisfier; see Pitfall 11)
- `pkg/dag/marshal.go` extension — `triggerJSON` shape + MarshalJSON + UnmarshalJSON with kind-keyed unmarshal registry
- Test stub: `fakeTriggerSource` (in `pkg/extension/trigger_test.go`)

**Tests:**
- `pkg/extension/trigger_test.go` — sealed interface tests (compile-time + runtime)
- `pkg/dag/trigger_test.go` — JSON round-trip; Freeze recursion; Pos exclusion

**Why first:** Pure data types with zero parser/worker dependencies. Wave 2 builds on these. Independent tests.

**Risk:** Low — mechanical type definitions.

### Wave 2: Parser builtin + lambda env + req-walker

**Scope:**
- `pkg/bridge/lambda_globals.go` extension — `triggerTimeGlobals` constant + `TriggerTimeGlobals()` accessor (lambdaTimeGlobals + json.Module + time.Module)
- `pkg/bridge/doc.go` extension — D-07-04 documentation block
- `pkg/parser/builtins.go` (or new `pkg/parser/trigger.go`) — `builtinTrigger` factory function
- `pkg/parser/lambda_capture.go` — extend `captureLambda` with arity enforcement (or add `captureLambdaWithArity`)
- `pkg/parser/ctx_walk.go` — minor refactor: rename `findCtxAccesses` → `findFreeVarAccesses` (signature unchanged; renaming for clarity)
- `pkg/parser/req_walk.go` (NEW) — `validateTriggerReqAccesses` + `checkLambdaReqAccesses`
- `pkg/parser/finalize.go` — add `validateTriggerFlowNames` and `validateTriggerReqAccesses` to the chain
- `pkg/parser/globals.go` — register `"trigger"` builtin
- `pkg/parser/parser.go` — new `triggers` field + `Triggers() / TriggersInOrder()` accessors
- `pkg/parser/testdata/triggers/*.star` — full Tier-1 fixture corpus (8 files)

**Tests:**
- `pkg/parser/trigger_test.go` — Wave 2's Tier-1 surface (TRIG-01, TRIG-04)
- `pkg/parser/req_walk_test.go` — req-walker isolated tests
- `pkg/bridge/lambda_globals_test.go` extension — TriggerTimeGlobals locked-set test

**Why second:** All parser primitives in place; downstream consumers (Wave 3+) can use Triggers from a parsed file.

**Risk:** Medium — generalizing ctx_walk.go is the only non-mechanical change; req-attribute walker needs careful position handling. Pitfall 3 covers.

### Wave 3: TriggerRegistry + boot integration

**Scope:**
- `pkg/interpreter/registry.go` — `TriggerRegistry` struct + Register + Freeze + All + ByContentHash (mirror FlowRegistry concurrency model)
- `pkg/worker/boot.go::bootRegistry` — extend signature + extend parse loop to register triggers
- `pkg/worker/worker.go::NewWorker` — receive both registries; new `Triggers()` accessor
- `pkg/worker/options.go::WorkerOptions` — new `WorkerStopTimeout` field threaded into sdkworker.Options
- `pkg/cli/run.go` — NO change for Phase 7 (run uses only FlowRegistry); verify compile

**Tests:**
- `pkg/interpreter/registry_test.go` extensions — TriggerRegistry concurrency + sort + frozen tests
- `pkg/worker/boot_test.go` extensions — TRIG-05 boot integration test (multi-file dir with mixed flows + triggers)
- `pkg/worker/options_test.go` — WorkerStopTimeout default + threading tests

**Why third:** Wave 2 produces `Triggers()` from a parsed session; Wave 3 plumbs them into the worker bootstrap. Clean dependency.

**Risk:** Low — mechanical mirror of existing FlowRegistry pattern.

### Wave 4: skytime server subcommand

**Scope:**
- `pkg/cli/server.go` — `newServerCommand` with full RunE: connect, build worker, banner, signal-loop with two-signal escalation
- `pkg/cli/render.go` — `setupServerLogging` function (charm-log default; JSON via --json-log)
- `pkg/cli/root.go` — `root.AddCommand(newServerCommand(cfg))`
- `pkg/cli/server_test.go` — behavioral tests via `testDrainHook` seam

**Tests:**
- SERVER-01..03 covered (see § Validation Architecture)
- Manual integration smoke: spawn `go run ./cmd/skytime server` against `temporal server start-dev`; send SIGTERM; assert clean exit

**Why fourth:** Worker + registries + parser primitives all in place. Server is a consumer.

**Risk:** Medium-High — only non-mechanical wave. Two-signal escalation is the trickiest part. Pitfall 5, 9 cover.

### Wave 5: dev-server → dev-temporal rename + firewalls + docs

**Scope:**
- `pkg/cli/dev_server.go` → `pkg/cli/dev_temporal.go` (file + symbol rename)
- `pkg/cli/dev_server_test.go` → `pkg/cli/dev_temporal_test.go` (file + test name rename)
- `pkg/cli/root.go` — line edit
- `cmd/skytime/main.go` — top doc-comment edit
- `examples/http-github-webhook/cmd/extbin/main.go` — top doc-comment edit
- `examples/http-github-webhook/cmd/extbin/main_test.go` — subcommand-list edit
- `pkg/cli/root_test.go` — subcommand presence test edit
- All doc files (~10 .md files) — text replace
- `.github/workflows/scripts/walkthrough_smoke.sh` — text replace
- `CLAUDE.md` (project root) — text replace
- `tests/firewall_credential_redaction_test.go` (NEW) — D-07-10 AST walker
- `tests/dev_server_grep_test.go` (NEW) — D-07-22 CI grep gate
- `go generate ./pkg/parser/` — regenerate `docs/reference/builtins.md` (verify no drift)

**Tests:**
- `tests/firewall_credential_redaction_test.go` — D-07-10
- `tests/dev_server_grep_test.go` — D-07-22
- `tests/docgen_drift_test.go` (existing) — must pass

**Why last:** Renames touch many files; doing this last avoids merge conflicts during waves 1-4. The firewall tests need waves 1-4 already in place to walk Trigger AST.

**Risk:** Low — mechanical text replacement; CI grep gate is the safety net.

### Wave dependency graph

```
Wave 1 (dag.Trigger + extension.TriggerSource)
    ↓
Wave 2 (parser builtin + req-walker)
    ↓
Wave 3 (TriggerRegistry + boot)
    ↓
Wave 4 (skytime server subcommand)
    ↓
Wave 5 (rename + firewalls + docs)
```

Strictly sequential. Within each wave, tasks may run in parallel (e.g., Wave 1's `pkg/dag/trigger.go` and `pkg/extension/trigger.go` are independent files). Wave 5 can begin partial work (firewall test scaffolding) once Waves 1-2 complete; the rename pieces wait for Waves 1-4 to avoid merge churn.

## Open Questions for Planner

1. **Pkg layout for `pkg/parser/req_walk.go`:** Recommended NEW file separate from existing `state_schema.go` (which owns `validateLambdaCtxAccesses`). Keeps trigger-specific validation cohesive. The planner may merge into one file if the LOC is small enough — but recommend separation for clarity.

2. **TriggerSource unmarshal-registry location:** `pkg/dag/trigger_unmarshal.go` vs `pkg/extension/trigger_unmarshal.go`. Both work. Recommend `pkg/extension/` since concrete TriggerSources live in extensions. Open Q 3 covers; planner finalizes.

3. **Test stub `fakeTriggerSource` reuse:** Phase 7 tests need it in `pkg/parser/`, `pkg/dag/`, `pkg/extension/`, `pkg/worker/`, `pkg/interpreter/`. Options: (a) export from `pkg/extension/testing` package, (b) duplicate per test package, (c) use `pkg/extension/internal/testfixtures`. Recommend (a) — minimal reach, single source of truth. Existing precedent: `pkg/testing` package for Phase 5's harness.

4. **--credfile no-op semantics:** Hard error vs silent no-op vs warn-only. Recommended hard error (Open Q 4 above). Planner may revisit based on user-friendliness goals.

5. **`testDrainHook` formal contract:** Stage names — recommend `"worker-started"`, `"drain-start"`, `"drain-complete"`, `"drain-forced"`, `"drain-timeout"`. Document in `pkg/cli/server.go` doc comment. Tests assert ordering via captured stage slice. Planner may extend if more granularity needed.

## Sources

### Primary (HIGH confidence)
- Project codebase (verified file-by-file): `pkg/dag/`, `pkg/parser/`, `pkg/extension/`, `pkg/bridge/`, `pkg/worker/`, `pkg/interpreter/`, `pkg/cli/`, `cmd/skytime/`, `examples/http-github-webhook/`, `tests/`, `.github/workflows/scripts/walkthrough_smoke.sh`, `go.mod`, `.planning/PROJECT.md`, `.planning/STATE.md`, `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md`, `.planning/v1.43-DRAFT-PLAN.md`, `.planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md`
- Temporal SDK v1.42.0 source (local module cache `~/go/pkg/mod/go.temporal.io/sdk@v1.42.0/internal/`):
  - `internal_worker.go:180-181` — `WorkerStopTimeout` field declaration
  - `internal_worker_base.go:699-718` — `Stop()` implementation (blocking + timeout-based wait)
  - `activity.go:255-265` — `GetWorkerStopChannel` doc explaining timeout semantics
- go.starlark.net v0.0.0-20260326113308-fadfc96def35 source (local module cache):
  - `lib/json/json.go` — JSON module shape and `Module` constant
  - `lib/time/time.go` — Time module shape, `now()` function, `Module` constant
  - `starlarkjson/json.go` — backwards-compatible alias

### Secondary (MEDIUM confidence — verified via web search + cross-references)
- [pkg.go.dev: go.temporal.io/sdk/worker](https://pkg.go.dev/go.temporal.io/sdk/worker) — Worker interface (Start/Run/Stop/InterruptCh)
- [Temporal Worker Shutdown Behavior](https://docs.temporal.io/encyclopedia/workers/worker-shutdown) — graceful vs non-graceful shutdown; activity context cancellation; Go SDK behavior asymmetry (activities continue using slots after shutdown if they don't honor ctx)
- [pkg.go.dev: github.com/spf13/pflag](https://pkg.go.dev/github.com/spf13/pflag) — `pflag.Duration` accepts `time.ParseDuration` syntax; no built-in range validation
- [signal.NotifyContext usage](https://henvic.dev/posts/signal-notify-context/) — single-shot; second signal restores OS default
- [pkg.go.dev: os/signal](https://pkg.go.dev/os/signal) — `signal.Notify` for buffered channel + counting

### Tertiary (LOW confidence — referenced for context only)
- [Temporal Graceful Worker Shutdown (Java SDK)](https://keithtenzer.com/temporal/Temporal-Graceful-Worker-Shutdown/) — Java-only article; Go semantics inferred from SDK source instead

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all packages already in go.mod; versions verified against go.sum; no new dependencies
- Architecture: HIGH — codebase patterns (builtin factory, sealed Node, FlowRegistry concurrency, signal handling) are well-established and documented in existing PROJECT.md / phase summaries
- SDK semantics (Worker.Stop / WorkerStopTimeout): HIGH — verified against SDK v1.42.0 source code locally; behavior matches `WorkerStopTimeout` documentation
- Pitfalls: HIGH for items 1-9 (codebase-grounded); MEDIUM for items 10-13 (design recommendations vs hard rules)
- Validation architecture: HIGH — all test patterns extend existing infrastructure (testify + go test + AST-walker firewalls + slog buffer capture)

**Research date:** 2026-05-08
**Valid until:** 2026-06-07 (30 days; SDK and starlark.net change rarely; cobra/pflag stable; Skytime codebase under active dev so re-verify before any Phase 7 plan-task issue >2 weeks after research)
