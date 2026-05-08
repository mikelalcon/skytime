# Phase 3: Lambda-Serialization Decision + Interpreter + Worker — Research

**Researched:** 2026-04-30
**Domain:** Temporal Go SDK v1.42.0 — generic interpreter workflow + worker bootstrap, with `*starlark.Function` lambdas surviving the serialization boundary via re-parse-on-start (Option B, locked per D3-01)
**Confidence:** HIGH on all SDK API shapes (verified against vendored v1.42.0 source via `go doc`); HIGH on Build ID currency (cross-checked against the v1.42.0 GoDoc and SDK changelog); MEDIUM on the cancellation-watchdog implementation (the only place where Skytime is doing something the SDK was not explicitly designed for — rationale below).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Lambda Serialization Mechanism (HEADLINE)

- **D3-01: Strategy is "re-parse on workflow start" (Option B)** — NOT custom DataConverter, NOT inline source embedding. Workflow's first event is a `workflow.SideEffect` that records the `content_hash` of the `.star` file the workflow was started against. On every subsequent workflow tick (including replays), the worker uses `(flow_name, content_hash)` to look up the parsed flow + lambda map from its in-memory registry.
- **D3-02: Workers DO NOT serialize `*starlark.Function` to history.** Function values stay in worker-local memory. Only `LambdaID` strings cross the workflow's boundary. The bridge's `CallLambda` receives the `*starlark.Function` from the worker's flow registry, looked up by `LambdaID`.
- **D3-03: Versioning relies on Temporal Build IDs (NOT Skytime DSL primitives).**
  - Each worker tags itself with a `BuildID` at startup.
  - Temporal routes workflow tasks to workers with compatible Build IDs (workflows pin to the Build ID under which they started).
  - When a `.star` file is edited, customers build a new worker binary, tag with a new Build ID, deploy alongside; old workflows drain on old workers, new workflows run on new workers.
  - Skytime exposes Build ID via `worker.Options.BuildID` (D3-15). No `version()` builtin in Starlark, no `workflow.Patch`-style DSL surface.
  - `workflow.GetVersion`/`Patch` are NOT exposed to `.star` authors. Versioning is operational, not authorial.

#### WorkflowInput & Wire Format

- **D3-04: WorkflowInput shape is `{flow_name string, content_hash string, init_state map[string]any}`.**
  - `flow_name` — looked up in the worker's flow registry.
  - `content_hash` — sha256 of the `.star` file bytes that defined `flow_name`. Computed on the worker at startup; recorded on workflow start via `workflow.SideEffect` so it persists across replays.
  - `init_state` — pure data from the workflow trigger (HTTP request, signal, scheduler).
- **D3-05: The full `dag.Flow` is NOT embedded in WorkflowInput.** It's looked up from the registry. This is the smallest payload and matches the Build-ID + filesystem-snapshot deployment model. Phase 1's `WorkflowInput.Flow *dag.Flow` field gets updated to `FlowName string` + `ContentHash string` (backward-incompatible wire-format change; no consumers yet).
- **D3-06: Worker rejects mismatched replays cleanly.** If a worker tries to handle a workflow whose recorded `content_hash` isn't in its registry, fail fast with a clear error (`"workflow expects flow X@<hash>; this worker has flow X@<other_hash>; use Build IDs to drain old workflows"`).

#### Source Delivery & Worker Boot

- **D3-07: `.star` source files reach workers via filesystem path (NOT `go:embed`).** Worker takes a `--rootdir` flag (or `SKYTIME_ROOT` env var) at startup. At boot, walks the directory, parses every `.star` file, computes `content_hash` for each, builds the registry. Registry is **frozen after boot** — no hot reload during the worker's lifetime.

#### Backend Abstraction

- **D3-08: NO `wf` interface for backend pluggability.** The interpreter directly imports `go.temporal.io/sdk/workflow`.

#### `call_flow` Semantics

- **D3-09: `call_flow(name=...)` ALWAYS invokes a Temporal child workflow.** No inline macro-expansion. Each call_flow becomes `workflow.ExecuteChildWorkflow`.
- **D3-10: `call_flow` retry policy is INHERITED from the parent flow's options by default.** Override via `call_flow(name=..., retry_policy=..., timeout=...)` kwargs.
- **D3-11: Search attributes / memos PROPAGATE from parent to child by default.** `call_flow` accepts override kwargs to clear or replace them.
- **D3-12: Cross-flow lambda IDs DO NOT need disambiguation by flow name.** Within a single `.star` file, line+col is unique per lambda regardless of which flow contains it.

#### `for_each_parallel` Concurrency Model

- **D3-13: Default fan-out cap = 10.** Configurable per call via `for_each_parallel(items=..., max_concurrency=N, ...)` kwarg.
- **D3-14: On non-retryable error in any branch, CANCEL siblings and bubble up the error.** Use `workflow.NewSelector` with a per-branch cancel context derived from the workflow context; first non-retryable failure cancels the parent context, all in-flight branches see `ctx.Err() == context.Canceled`. The for_each_parallel returns the original error.
- **D3-15: Item access in lambdas is via `ctx.<item_name>`.** The bridge injects the item under `ctx` using the `item` kwarg name.
- **D3-16: Iteration contract is "stable index order; results in input order".** Branches spawned in input order with index `0..N-1`. Results collected in same order regardless of completion timing.

#### Worker Bootstrap & Client Factory

- **D3-17: Three named constructors for Temporal clients.**
  - `skytime.NewCloudClient(opts CloudOptions) (*Client, error)`
  - `skytime.NewSelfHostedClient(opts SelfHostedOptions) (*Client, error)`
  - `skytime.NewDevClient(opts DevClientOptions) (*Client, error)`
- **D3-18: Worker entry point is `worker.Start` non-blocking + `worker.Stop`.**
- **D3-19: Default task queue is `"skytime"`, with per-flow and per-step overrides.** Hierarchy: step's `task_queue` > flow's `task_queue` > worker default. Override at parser level via new DSL kwargs `flow(name=..., task_queue=...)` and `step(action=..., task_queue=...)`. **DSL retrofit required** — Phase 1's builtins do NOT currently accept `task_queue`; Phase 3 backports them and adds `dag.Flow.TaskQueue` + `dag.Step.TaskQueue` fields.
- **D3-20: BuildID is `worker.Options.BuildID` with a sensible default.** Default = a build-time-injected variable (`var defaultBuildID = "dev"` overridable via `-ldflags "-X github.com/mikelalcon/skytime.defaultBuildID=$(git rev-parse HEAD)"`).

#### Cancellation Watchdog

- **D3-21: `workflow.Context.Done()` propagates to `thread.Cancel` for in-flight lambdas.** When the workflow context is cancelled, the interpreter wakes up any active `bridge.CallLambda` invocation by triggering the thread's cancel hook.

#### Print Hook Wiring

- **D3-22: `print()` inside lambdas routes to `workflow.GetLogger(ctx).Info("[skytime/print] " + msg, "lambda_id", id)`.**

#### Determinism Requirements

- **D3-23: Map iteration in the interpreter sorts keys before reading.**
- **D3-24: Interpreter passes `workflowcheck` analysis with no findings.**

### Claude's Discretion

- Exact name of the watchdog goroutine helper.
- Internal layout of the worker registry (`map[string]map[string]*ParsedFlow` vs flat `map[FlowVersionKey]*ParsedFlow`).
- Specific concurrency primitive for `for_each_parallel` semaphore (buffered channel vs `golang.org/x/sync/semaphore`).
- Default RetryPolicy when nothing is set (none; Temporal's default is fine).
- Whether `dev-server` helper actually spawns `temporal server start-dev` in-process or just connects to localhost (probably the latter for v1).
- Ordering of `pkg/interpreter` files (one big `workflow.go` vs split by node type).

### Deferred Ideas (OUT OF SCOPE)

- **Hot-reload of `.star` files** — registry is frozen at boot.
- **`workflow.Patch` / `version()` DSL primitives** — versioning is operational, not authorial.
- **In-process `temporal server start-dev` spawning** — `NewDevClient` connects to an externally-running dev server.
- **Per-flow / per-step Build IDs** — Build ID is worker-level only in v1.
- **Multi-version worker registry without Build IDs** — explicitly NOT in v1.
- **Custom DataConverter** — explicitly NOT in v1.
- **`signal_workflow` / `wait_for_signal` primitives** — v1.x.
- **`on_error` / `on_failure` per-step or per-flow hooks** — v1.x.
- **Default Query handler** — Phase 7.
- **Continue-As-New strategy** — not in v1.
- **Backend abstraction (`wf` interface)** — explicitly dropped.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INTRP-01 | Single generic `SkytimeWorkflow(ctx, WorkflowInput)` walks any parsed `dag.Flow` and produces final state | §"SkytimeWorkflow Skeleton" + §"Walker by Node Type" |
| INTRP-02 | Documented decision (DataConverter vs. re-parse-on-start with `LambdaID` keys) governs lambda serialization; mechanism passes a replay-twice equality test | Locked per D3-01..03 (Option B). §"Lambda-Serialization Mechanism (Option B Implementation)" |
| INTRP-03 | `if_cond` and `script` evaluate lambdas inline producing zero Temporal history events | §"Inline Lambda Evaluation (zero history events)" — pure `bridge.CallLambda` calls; no `workflow.*` API used during eval |
| INTRP-04 | `for_each_parallel` spawns concurrent branches via `workflow.Go` + `workflow.Selector` with configurable bounded fan-out (documented default), uses `workflow.Await` to barrier | §"for_each_parallel Concurrency Model" |
| INTRP-05 | `call_flow` invokes the same generic workflow as `workflow.ExecuteChildWorkflow`, isolating sub-flow history | §"call_flow as Child Workflow" |
| INTRP-06 | Map iteration sorts keys before reading; verified by replay-twice CI test | §"Determinism Discipline" + §"Replay-Twice Test" |
| INTRP-07 | Interpreter passes `workflowcheck` analysis (no native `go`, no time/random calls, no map iteration without sort) | §"workflowcheck CI Integration" |
| WORK-01 | Worker bootstrap registers `SkytimeWorkflow` and `ExecuteBatch` with one Temporal worker | §"Worker Bootstrap" |
| WORK-02 | One client factory handles three Temporal connection variants — Cloud (API key + TLS), self-hosted mTLS, local dev-server (TLS off) — surfacing the v1.39 TLS-with-API-key default change in exactly one place | §"Three Named Client Constructors" |
| WORK-03 | A consumer Go service can embed Skytime as a library: `import` packages, register extensions, call `worker.Run(client, flowDir)` — no service binary required | §"Consumer main.go Pattern" |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

The repo's `CLAUDE.md` adopts the project text from PROJECT.md and the Phase-1 stack/research summaries. The actionable directives Phase 3 must honor:

- **No string compilation** — only native Starlark lambdas; never re-introduce CEL/expression-language surface for cancellation, versioning, or anywhere else.
- **No dynamic activities** — extensions never register their own Temporal activities; the interpreter's only `workflow.ExecuteActivity` call is to the single `"ExecuteBatch"` registered in Phase 2.
- **No context bleed** — `workflow.Context` MUST NOT be reachable from any `*starlark.Thread`; `*starlark.Thread` MUST NOT appear in any payload type. The cancellation watchdog (§"Cancellation Watchdog") is the one carefully-bounded interaction and must respect this firewall.
- **Determinism non-negotiable** — replay must produce identical command sequences; map iteration sorts keys; no `time.Now`, no `rand.*`, no native `go`; only `workflow.Go`. `workflowcheck` must be CI-clean.
- **Architectural firewall** — only `pkg/activity` (Phase 2) and `pkg/interpreter` + `pkg/worker` (this phase) may import `go.temporal.io/sdk/...`. The Phase-2 firewall test must be updated to permit the two new packages, and assert no other package imports the SDK.
- **Co-located tests** — `*_test.go` lives next to the code it tests; integration tests using `testsuite.TestWorkflowEnvironment` live alongside the interpreter package.
- **Atomic per-task commits** — one logical change per commit, matching the convention established in Phases 1–2.

## Summary

Phase 3 is the execute side of Skytime. The headline (D3-01) is locked: lambdas survive Temporal's serialization boundary via **re-parse-on-start (Option B)** — the `*starlark.Function` never crosses the wire; only the workflow input `{FlowName, ContentHash, InitState}` does. At workflow start, `workflow.SideEffect` records the worker-supplied `ContentHash` so it survives replay; the interpreter then looks up the parsed flow + lambda map from a worker-local registry that was frozen at worker boot.

The two non-trivial things to build, in order of risk:

1. **The cancellation watchdog (D3-21)** — the only legal interaction between `workflow.Context` and a `*starlark.Thread`, and the place where Skytime is doing something the SDK was not explicitly designed for. The right pattern: use `workflow.Go(ctx, ...)` to read `ctx.Done()` (which is `workflow.Channel`, not `<-chan struct{}`) and call `thread.Cancel` from there — but lambda eval itself runs on the workflow's main goroutine. Solution: bridge the workflow channel to a native Go channel via a small helper (a `workflow.Go` reader that closes a native channel), then keep the Phase-1 watchdog goroutine pattern unchanged. **This is the trickiest seam in the phase**, called out in §"Cancellation Watchdog" with the full rationale.

2. **`for_each_parallel` cancel-on-error semantics (D3-14, D3-16)** — `workflow.Go` for branches, `workflow.WithCancel(parent)` to derive a cancellable child context, `workflow.NewSelector` to wait on per-branch completion futures, results collected in input order via a pre-sized `[]ActionResult` slice. On the first non-retryable error, call the cancel function and bubble the original error after the in-flight `workflow.Await` returns.

Everything else is straightforward Temporal-SDK plumbing: `workflow.SideEffect` for the content-hash recording (the workflow trigger computes the hash off-Temporal so the workflow body never has to), `workflow.ExecuteActivity(ctx, "ExecuteBatch", batch)` for Step dispatch with `WithTaskQueue`/`WithRetryPolicy`/`WithStartToCloseTimeout` overrides, `workflow.ExecuteChildWorkflow(ctx, "SkytimeWorkflow", subInput)` for `call_flow` with retry policy + search-attribute inheritance copied from `workflow.GetInfo(ctx)`. Worker bootstrap is `worker.New(client, taskQueue, opts)` with `opts.BuildID` (still the supported field in v1.42, per `go doc`) and the `UseBuildIDForVersioning` flag for the assignment-rules path.

The DSL retrofit (D3-19) is a Wave 0 prerequisite: add `task_queue` kwarg to `flow()` and `step()` builtins; add `TaskQueue string` field to `dag.Flow` and `dag.Step`; update fixtures.

**Primary recommendation:** Wave the work as (0) DSL retrofit + Phase-2 firewall update + new `WorkflowInput` shape, (1) `pkg/interpreter` skeleton + `pkg/worker` registry + flow-name → content-hash → ParsedFlow lookup, (2) walker per node type (Step → Activity, Script/IfCond → bridge.CallLambda inline, Cancellation watchdog), (3) for_each_parallel + call_flow, (4) three named client constructors + worker bootstrap, (5) replay-twice integration test + workflowcheck CI hook.

## Standard Stack

### Core (already pinned, no new deps)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go.temporal.io/sdk` | `v1.42.0` (2026-04-08, pinned in `go.mod`) | Workflow + activity + client + worker APIs | Only official Go SDK. Verified: `worker.Options.BuildID` field present (deprecated label is for `DeploymentOptions.Version`, but BuildID is still fully supported), `workflow.SideEffect` API matches D3-01 plan, `client.Options.Credentials` + `client.NewAPIKeyStaticCredentials` is the v1.39+ TLS-with-API-key path. |
| `go.temporal.io/sdk/workflow` | (sub-package) | `workflow.Go`, `workflow.NewSelector`, `workflow.Await`, `workflow.SideEffect`, `workflow.ExecuteActivity`, `workflow.ExecuteChildWorkflow`, `workflow.WithCancel`, `workflow.WithTaskQueue`, `workflow.WithRetryPolicy`, `workflow.WithChildOptions`, `workflow.GetInfo`, `workflow.GetLogger` | Single source of all coroutine + activity scheduling primitives. |
| `go.temporal.io/sdk/worker` | (sub-package) | `worker.New`, `Worker.RegisterWorkflow{WithOptions}`, `Worker.RegisterActivity{WithOptions}`, `Worker.Start`, `Worker.Stop`, `worker.Options{BuildID, UseBuildIDForVersioning, MaxConcurrentWorkflowTaskExecutionSize, ...}`, `worker.NewWorkflowReplayer` | Worker lifecycle + the `WorkflowReplayer` API for the replay-twice test. |
| `go.temporal.io/sdk/client` | (sub-package) | `client.Dial`, `client.Options{HostPort, Namespace, Credentials, ConnectionOptions{TLS, TLSDisabled}, Identity}`, `client.NewAPIKeyStaticCredentials`, `client.NewMTLSCredentials` | Three connection variants. v1.39: API key implies TLS unless `TLSDisabled` set. |
| `go.temporal.io/sdk/temporal` | (sub-package) | `temporal.RetryPolicy`, `temporal.NewApplicationError`, `temporal.NewNonRetryableApplicationError`, `temporal.ApplicationError` | Retry policy used by `WithRetryPolicy`; non-retryable error construction for hash-mismatch and lambda-failure cases. |
| `go.temporal.io/sdk/testsuite` | (sub-package) | `testsuite.WorkflowTestSuite`, `testsuite.TestWorkflowEnvironment` | All integration tests for the interpreter live here — `OnActivity` to mock `ExecuteBatch`, `RegisterWorkflow(SkytimeWorkflow)`, `ExecuteWorkflow(SkytimeWorkflow, input)`. |
| `go.starlark.net/starlark` | `v0.0.0-20260326113308-fadfc96def35` | Lambda evaluation (via `pkg/bridge`) — already in use | Phase 3 calls `bridge.CallLambda` only; no new direct Starlark imports inside `pkg/interpreter`. |

### Supporting (no new direct deps; everything is `go.temporal.io/sdk` sub-packages)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `go.temporal.io/sdk/contrib/tools/workflowcheck` | `v0.4.0` (per pkg.go.dev) | Static-analysis vet tool over `pkg/interpreter` | CI-only; install via `go install go.temporal.io/sdk/contrib/tools/workflowcheck@latest` and run `workflowcheck ./pkg/interpreter/...`. Lives in a separate command module — NOT vendored into the main SDK module under `go.mod`. |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Re-parse-on-start (Option B, locked) | Custom `DataConverter` (Option A) | Locked to B per D3-01. Option A would store source location + closure env in history; rejected for v1 because it requires the `*starlark.Program` to be reconstructible on every replayer (more moving parts; no upside for the Build-ID + filesystem-snapshot deployment model). |
| `worker.Options.BuildID + UseBuildIDForVersioning` | `worker.Options.DeploymentOptions{Version, UseVersioning}` | The BuildID-based fields are marked "Deprecated: Use [WorkerDeploymentOptions.Version]" in v1.42 GoDoc. **However**: (1) the deprecation note says "deprecated soon", not "removed", (2) the assignment-rules approach (`UpdateWorkerVersioningRules`) is also deprecated in favor of Worker Deployment Versioning. For a single-worker-version-at-a-time deployment (Skytime's v1 model: build new binary, deploy alongside, drain old), `BuildID + UseBuildIDForVersioning = true` is the simpler, well-supported path and matches what D3-15/D3-20 describe. Recommend BuildID for v1; revisit Deployment Versioning if a customer needs co-versioning. |
| `workflow.SideEffect` for hash recording | Pass `ContentHash` as part of `WorkflowInput` (computed by trigger) | **Recommendation: trigger computes the hash and passes it in WorkflowInput; the workflow `assert`s it matches the worker's registry but does NOT recompute it.** SideEffect's callback runs in workflow context — it CAN call non-deterministic functions (that's the point), but it's the wrong primitive here: the hash is not non-deterministic, it's a stable property of the source file the trigger picked. Including it in WorkflowInput records it in history naturally (Temporal records the input), survives replay automatically, and avoids spending a SideEffect event. See §"Lambda-Serialization Mechanism" for the full rationale. |
| Buffered `workflow.Channel` for fan-out | Native `chan struct{}` semaphore (forbidden) | Native channels in workflow code → non-deterministic; `workflowcheck` flags. Solution: pre-sized `[]ActionResult` + per-branch `workflow.Go` + a `workflow.NewBufferedChannel(ctx, max_concurrency)` semaphore. |
| `golang.org/x/sync/semaphore` | Buffered `workflow.Channel` | `x/sync/semaphore` is fine in extensions/activities; it is **not** workflow-safe. Use `workflow.NewBufferedChannel`. |

**Installation:** No new dependencies. `workflowcheck` is a CI tool installed via `go install`, not declared in `go.mod`.

**Version verification:** `go.temporal.io/sdk@v1.42.0` is already pinned in `go.mod` (verified — see line 9 of `go.mod`). v1.42.0 was released 2026-04-08 (verified via release-notes fetch above). The `BuildID` field exists in `worker.Options` in this version (verified — see `go doc go.temporal.io/sdk/internal.WorkerOptions` output, lines for `BuildID` and `UseBuildIDForVersioning`).

## Architecture Patterns

### Recommended Project Structure

```
pkg/
├── dag/                            # existing
│   ├── input.go                    # MODIFIED: WorkflowInput.{FlowName, ContentHash, InitState}
│   ├── flow.go                     # MODIFIED: + TaskQueue string field (D3-19)
│   ├── step.go                     # MODIFIED: + TaskQueue string field (D3-19)
│   └── ...                         # rest unchanged
│
├── parser/                         # existing
│   └── builtins.go                 # MODIFIED: flow() and step() accept task_queue= kwarg
│
├── activity/                       # Phase 2 — UNCHANGED. Phase 3 only schedules ExecuteBatch.
│
├── bridge/                         # Phase 1 — UNCHANGED. Phase 3 supplies workflow-side wiring.
│
├── interpreter/                    # NEW — Phase 3 owns this package
│   ├── doc.go                      # package docs + firewall comment
│   ├── workflow.go                 # SkytimeWorkflow entry point + main walk loop
│   ├── walk_step.go                # Step → workflow.ExecuteActivity("ExecuteBatch", ...)
│   ├── walk_script.go              # Script → bridge.CallLambda inline (zero history events)
│   ├── walk_ifcond.go              # IfCond → bridge.CallLambda inline; branch on bool
│   ├── walk_foreach.go             # ForEachParallel → workflow.Go + Selector + cancel
│   ├── walk_callflow.go            # CallFlow → workflow.ExecuteChildWorkflow
│   ├── state.go                    # in-workflow state map + lambda eval helper
│   ├── cancel_watchdog.go          # workflow.Channel → native chan bridge for thread.Cancel
│   ├── registry.go                 # FlowRegistry: map[flow_name]map[content_hash]*ParsedFlow
│   ├── options.go                  # ActivityOptions/ChildWorkflowOptions builders
│   └── *_test.go                   # co-located unit tests + testsuite integration tests
│
└── worker/                         # NEW — Phase 3 owns this package
    ├── doc.go
    ├── client.go                   # NewCloudClient / NewSelfHostedClient / NewDevClient
    ├── worker.go                   # NewWorker + Start/Stop wiring
    ├── options.go                  # WorkerOptions (RootDir, BuildID, TaskQueue, ...)
    ├── boot.go                     # Walks RootDir, parses every .star, builds FlowRegistry
    ├── build_id.go                 # `var defaultBuildID = "dev"` (overridable via -ldflags)
    └── *_test.go
```

### Pattern 1: Lambda-Serialization Mechanism (Option B Implementation)

**What:** No `*starlark.Function` ever crosses the wire. Workflow input is just `{FlowName, ContentHash, InitState}`. The worker's flow registry is the in-memory hop.

**Concrete flow:**
1. **Worker boot** — `worker/boot.go` walks `--rootdir`, parses every `.star` file via `pkg/parser`, computes `sha256(fileBytes)` for each, stuffs every result into:
   ```go
   type FlowRegistry struct {
       byFlow map[string]map[string]*ParsedFlow // flow_name → content_hash → flow+lambdas
   }
   type ParsedFlow struct {
       Flow    *dag.Flow
       Lambdas map[string]*dag.CapturedLambda
   }
   ```
   Frozen after boot — no mutation API.
2. **Trigger / external code** — caller computes `sha256(fileBytes)` of the `.star` they're targeting and constructs `dag.WorkflowInput{FlowName, ContentHash, InitState}`. Calls `client.ExecuteWorkflow(SkytimeWorkflow, input)`. Temporal records the input in history.
3. **`SkytimeWorkflow` first event** — read `ContentHash` from the input, look up `registry.byFlow[input.FlowName][input.ContentHash]`. If missing, return `temporal.NewNonRetryableApplicationError("flow X@<hash> not found in worker registry; use Build IDs to drain old workflows", "FlowNotInRegistry", nil)` — this drops the workflow into a terminal-failed state that's clearly attributable.
4. **Walk the DAG** — every lambda eval looks up `lambdas[node.LambdaID]` in the same `ParsedFlow` and passes the `*starlark.Function` to `bridge.CallLambda`.

**Why not `workflow.SideEffect`:** `SideEffect` is for capturing values that depend on host state at workflow start (random number, current time, UUID). The content hash is **not** non-deterministic — it's a property of the trigger's source file, computed once before workflow start. Putting it in `WorkflowInput` is simpler (no extra event in history), preserves it across replay automatically (Temporal records inputs), and matches D3-04's locked shape. **Recommend the `WorkflowInput`-carries-hash pattern over `SideEffect`-records-hash.**

**Replay safety:** On replay, `WorkflowInput` is decoded from the history's `WorkflowExecutionStartedEventAttributes.input`. Same `FlowName`, same `ContentHash`. Same registry lookup → same `ParsedFlow`. Same lambdas. Same evaluations → byte-equal commands. Provided the worker has the matching content hash (Build IDs enforce this operationally), replay is automatic.

**Source:** `go doc go.temporal.io/sdk/workflow.SideEffect` (read above). The "non-deterministic value capture" framing is explicit in the doc.

### Pattern 2: Inline Lambda Evaluation (zero history events) — INTRP-03

**What:** `if_cond` and `script` evaluate their lambdas synchronously on the workflow goroutine via `bridge.CallLambda`. No `workflow.*` API is called during the eval itself, so zero events land in history.

**Concrete signature:**
```go
// pkg/interpreter/state.go
func (i *interpreter) evalLambda(ctx workflow.Context, lambdaID string) (starlark.Value, error) {
    captured, ok := i.parsed.Lambdas[lambdaID]
    if !ok {
        return nil, temporal.NewNonRetryableApplicationError(
            fmt.Sprintf("lambda %s not found in flow %s@%s", lambdaID, i.flowName, i.contentHash),
            "LambdaNotFound", nil,
        )
    }
    cancelChan := i.makeCancelChannel(ctx) // see Cancellation Watchdog
    val, err := bridge.CallLambda(
        contextFromWorkflowCtx(ctx), // a stdlib context.Context that carries no workflow.Context
        captured,
        i.state.snapshot(),
        bridge.CallOptions{
            Logger:   i.logger,
            PrintSink: func(_ context.Context, msg string) {
                workflow.GetLogger(ctx).Info("[skytime/print] "+msg, "lambda_id", captured.ID)
            },
            Cancel: cancelChan,
        },
    )
    if err != nil {
        // Wrap with Starlark backtrace — let interpreter caller decide retry
        return nil, fmt.Errorf("lambda %s @ %s: %w", lambdaID, captured.Pos, err)
    }
    return val, nil
}
```

`contextFromWorkflowCtx(ctx)` returns `context.Background()` (or a derived context that uses `i.logger`) — it MUST NOT embed the workflow.Context, per the no-context-bleed invariant. Cancellation propagates through the `Cancel` channel, not through the Go context.

**When to use:** for `IfCond.LambdaID`, `Script.LambdaID`, and `ForEachParallel.ItemsLambdaID` (when set). All three are pure "evaluate this lambda on current state, get a value back."

**Workflow-history footprint:** zero events. The lambda eval is just CPU on the workflow goroutine. The replay-twice test asserts this.

### Pattern 3: Step → ExecuteBatch Activity Dispatch

**What:** Each `dag.Step` becomes one `workflow.ExecuteActivity(ctx, "ExecuteBatch", step.Actions)` call. Result is `[]dag.ActionResult` (Phase 2 wire format).

**Concrete:**
```go
// pkg/interpreter/walk_step.go
func (i *interpreter) walkStep(ctx workflow.Context, step *dag.Step) error {
    actx := workflow.WithActivityOptions(ctx, i.activityOptionsForStep(step))
    var results []dag.ActionResult
    if err := workflow.ExecuteActivity(actx, "ExecuteBatch", step.Actions).Get(ctx, &results); err != nil {
        return err // ApplicationError; interpreter caller decides
    }
    return i.recordStepResults(step, results)
}

func (i *interpreter) activityOptionsForStep(step *dag.Step) workflow.ActivityOptions {
    opts := workflow.ActivityOptions{
        // D2-15: sum-of-per-action-timeouts + 30s headroom — interpreter computes
        StartToCloseTimeout: i.computeBatchTimeout(step),
        HeartbeatTimeout:    60 * time.Second, // generous; activity heartbeats per-action (D2-16)
    }
    // D3-19 task-queue hierarchy: step > flow > worker default
    if step.TaskQueue != "" {
        opts.TaskQueue = step.TaskQueue
    } else if i.flow.TaskQueue != "" {
        opts.TaskQueue = i.flow.TaskQueue
    }
    if step.Retry != nil {
        opts.RetryPolicy = toTemporalRetryPolicy(step.Retry)
    }
    return opts
}
```

**Activity name:** `"ExecuteBatch"` — **literal string**, NOT a function reference. The Phase-2 activity is registered with `RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})`. The interpreter calls by name to avoid pulling `pkg/activity` into the workflow package's import graph (the firewall test should still allow it via the worker bootstrap, but using a string is the loosest coupling).

### Pattern 4: for_each_parallel Concurrency Model — INTRP-04

**What:** Spawn N branches via `workflow.Go`, each runs the body for one item. Use `workflow.NewBufferedChannel(ctx, max_concurrency)` as a semaphore. Results collected in input order via a pre-sized `[]any` slice. On non-retryable error in any branch, call `cancel()` and bubble the error.

**Concrete pattern:**
```go
// pkg/interpreter/walk_foreach.go
func (i *interpreter) walkForEach(ctx workflow.Context, fe *dag.ForEachParallel) error {
    items, err := i.resolveItems(ctx, fe) // ItemsLiteral or ItemsLambdaID-lambda eval
    if err != nil {
        return err
    }
    n := len(items)
    if n == 0 {
        return nil
    }
    maxConc := fe.MaxConcurrency // populated by D3-13; default 10 if zero
    if maxConc <= 0 {
        maxConc = 10
    }
    if maxConc > n {
        maxConc = n
    }

    // Cancellable child context — D3-14
    childCtx, cancel := workflow.WithCancel(ctx)
    defer cancel()

    // Semaphore via buffered workflow.Channel (deterministic; native chan forbidden)
    sem := workflow.NewBufferedChannel(childCtx, maxConc)

    // Per-branch completion channel (one sender per branch)
    done := workflow.NewBufferedChannel(childCtx, n)

    // Result slot per index (D3-16: input-order regardless of completion order)
    branchErrs := make([]error, n)

    for idx := 0; idx < n; idx++ {
        idx := idx
        item := items[idx]
        // Acquire semaphore (deterministic blocking)
        sem.Send(childCtx, struct{}{})

        workflow.Go(childCtx, func(branchCtx workflow.Context) {
            defer sem.Receive(branchCtx, nil) // release
            defer done.Send(branchCtx, idx)

            // Scoped state: parent state + ctx.<item_var>=item
            scopedState := i.state.scoped(fe.ItemVar, item)
            err := i.walkBranch(branchCtx, fe.Steps, scopedState)
            if err != nil {
                branchErrs[idx] = err
                if isNonRetryable(err) { // D3-14
                    cancel()
                }
            }
        })
    }

    // Barrier on all branches
    for completed := 0; completed < n; completed++ {
        var idx int
        done.Receive(ctx, &idx)
    }

    // Aggregate: first non-retryable wins; otherwise first non-nil retryable
    return aggregateBranchErrors(branchErrs)
}
```

**Why `workflow.Channel` and not selectors-over-futures:** Each `workflow.Go` branch is fire-and-forget; we need per-branch *completion notification* with index, which is naturally a channel send. Using a Selector with one Future per branch is also legal but adds noise (we'd be calling `selector.AddFuture(...)` N times then `Select(ctx)` N times). The buffered-channel pattern is what `workflow.Await` would expand into and is what the SDK's own samples use for "wait for N coroutines."

**Cancellation propagation:** `workflow.WithCancel(ctx)` — once `cancel()` fires, the child context's `Done()` channel closes. Any in-flight `workflow.ExecuteActivity` in a branch sees `CanceledError` from its `Future.Get`. Any in-flight `bridge.CallLambda` sees its cancel channel close (via the watchdog) and the Starlark thread cancels.

**Source:** `go doc go.temporal.io/sdk/workflow.WithCancel` (verified above); `go doc go.temporal.io/sdk/workflow.NewBufferedChannel` (verified above).

### Pattern 5: call_flow as Child Workflow — INTRP-05

**What:** Each `dag.CallFlow` becomes one `workflow.ExecuteChildWorkflow(ctx, "SkytimeWorkflow", subInput)`. Retry policy and search attributes inherited from parent (D3-10, D3-11).

**Concrete:**
```go
// pkg/interpreter/walk_callflow.go
func (i *interpreter) walkCallFlow(ctx workflow.Context, cf *dag.CallFlow) error {
    parentInfo := workflow.GetInfo(ctx)
    cwo := workflow.ChildWorkflowOptions{
        WorkflowID:        cf.workflowIDOrAuto(), // optional override; default auto
        TaskQueue:         resolveChildTaskQueue(i.flow, cf), // step > flow > inherit
        // D3-10: inherit retry policy unless overridden
        RetryPolicy:       parentInfo.RetryPolicy, // *internal.RetryPolicy from GetInfo
        // D3-11: propagate search attrs / memos by default
        TypedSearchAttributes: workflow.GetTypedSearchAttributes(ctx),
        Memo:                  copyMemoMap(parentInfo.Memo),
    }
    if override, ok := cf.ChildOptions["retry_policy"]; ok {
        cwo.RetryPolicy = toTemporalRetryPolicy(override.(*dag.RetryPolicy))
    }
    childCtx := workflow.WithChildOptions(ctx, cwo)

    // Resolve the called flow's content hash from the worker's registry —
    // call_flow within the same .star directory uses the same worker.
    childContentHash := i.registry.contentHashFor(cf.Name)
    if childContentHash == "" {
        return temporal.NewNonRetryableApplicationError(
            fmt.Sprintf("call_flow %q: child flow not found in worker registry", cf.Name),
            "ChildFlowNotInRegistry", nil,
        )
    }

    subInput := dag.WorkflowInput{
        FlowName:    cf.Name,
        ContentHash: childContentHash,
        InitState:   coerceCallFlowInputs(cf.Inputs, i.state),
    }
    var result map[string]any
    return workflow.ExecuteChildWorkflow(childCtx, "SkytimeWorkflow", subInput).Get(ctx, &result)
}
```

**Retry-policy inheritance details:** `workflow.GetInfo(ctx).RetryPolicy` returns `*internal.RetryPolicy` (per the verified GoDoc). It's the workflow's run-level retry policy if one was set at start time. Default: nil (Temporal's default = no retry for child workflows). D3-10's "inherit by default" means: if `parentInfo.RetryPolicy != nil`, copy it; if nil, leave it nil (which is Temporal's no-retry default — explicitly fine per "Claude's Discretion: Default RetryPolicy when nothing is set (none; Temporal's default is fine)").

**Search-attribute inheritance:** `workflow.GetTypedSearchAttributes(ctx)` returns `temporal.SearchAttributes`. Pass it directly via `cwo.TypedSearchAttributes`. The deprecated `SearchAttributes map[string]interface{}` field exists too but the typed one is preferred in v1.42.

**Cross-flow lambda IDs (D3-12):** Each child's `ParsedFlow.Lambdas` is a separate map indexed by D-18 IDs. Across files, sha-content-prefix differs, so no collision. Within a single file, line+col is unique. The child workflow does its own `Lambdas[id]` lookup against ITS `ParsedFlow` — no cross-contamination.

**Child workflow doesn't get a fresh registry:** The same worker process that started the parent runs the child (same task queue + Build ID assignment). The child's `SkytimeWorkflow` instance pulls from the same `FlowRegistry` instance.

**Source:** `go doc go.temporal.io/sdk/workflow.ExecuteChildWorkflow`, `go doc go.temporal.io/sdk/internal.ChildWorkflowOptions`, `go doc go.temporal.io/sdk/workflow.GetInfo` — all verified above.

### Pattern 6: Cancellation Watchdog — D3-21 (THE TRICKY PART)

**Problem statement:** `bridge.CallLambda` (Phase 1) supports a `Cancel <-chan struct{}` option. Phase 1 wired this with a native goroutine because in Phase 1 `CallLambda` was called from activities (where native goroutines are fine). In Phase 3, lambdas run **inside the workflow goroutine**, where:

- Native `go` keyword is **forbidden** by `workflowcheck` and the determinism contract.
- `workflow.Context.Done()` returns `workflow.Channel`, **not** `<-chan struct{}`.
- A `workflow.Channel`'s `Receive(ctx, ...)` requires `workflow.Context` and is itself a coroutine-yielding call.

**Three options considered:**

| Option | Description | Verdict |
|--------|-------------|---------|
| (a) Pre-eval check only | Before each lambda call, `if ctx.Err() != nil { return ErrCancelled }`. No watchdog. | **Rejected**: D-22's MaxExecutionSteps default is 10M, lambda eval can take seconds; cancellation that arrives mid-eval would be ignored. Fails the "cancel within bounded time" success criterion. |
| (b) `workflow.Go` watchdog reading `ctx.Done()` | Spawn a workflow coroutine that does `ctx.Done().Receive(ctx, nil)`, then calls `thread.Cancel`. | **Conceptually right but mechanically broken**: lambda eval runs **synchronously** on the workflow's main goroutine. When the main goroutine is in `starlark.Call`, the SDK's coroutine scheduler doesn't get to run the watchdog coroutine — they're cooperative, and `starlark.Call` doesn't yield. The watchdog would only run after the lambda finished, at which point thread.Cancel is too late. |
| (c) Native goroutine watchdog reading a bridged channel | Use `workflow.Go` to bridge `workflow.Context.Done()` → a native `chan struct{}`, run lambda eval in a fresh native goroutine, race against the bridged channel. | **The right answer.** Adopted; details below. |

**Wait — option (c) uses native goroutines inside a workflow.** That's the forbidden thing.

**Reconciliation — and this is the subtle part:** `bridge.CallLambda` itself is **not** workflow code from `workflowcheck`'s perspective — it's plain Go that takes a `context.Context` and returns a `(starlark.Value, error)`. The interpreter only calls it. The interpreter's *call site* is workflow code; the *body of CallLambda* is regular code that happens to be invoked synchronously.

But from a determinism perspective: `starlark.Call` is deterministic given the same inputs (D-19/D-20 enforce this), and we're not introducing any non-determinism by running it on a native goroutine *if and only if* the result is consumed deterministically. The recipe:

1. **From the workflow goroutine**, derive the cancel channel deterministically:
   - Spawn `workflow.Go(ctx, func(branchCtx) { branchCtx.Done().Receive(branchCtx, nil); /* close native chan */ })` — this coroutine runs deterministically, always observes the same cancel signal at the same logical time on replay.
   - The "close native chan" step uses `sync.Once` + a native `close(c)` call — closing a channel is deterministic.
2. **From the workflow goroutine**, call `bridge.CallLambda(stdlibCtx, captured, state, opts)` — synchronously. Pass `opts.Cancel = the_native_chan`. Inside `CallLambda`, the existing Phase 1 native-goroutine watchdog reads the native chan and calls `thread.Cancel`.
3. **`bridge.CallLambda` returns** with either the value or a "cancelled by caller" error. The workflow goroutine resumes.

**Why this passes determinism:** From the workflow scheduler's perspective, the `bridge.CallLambda` call is one atomic unit of Go execution — same inputs → same output (or same cancellation timing relative to scheduler events). The native goroutine inside `CallLambda` is a private implementation detail that doesn't escape; nothing in workflow state observes its internal scheduling. This is identical in spirit to how `workflow.SideEffect`'s callback runs as plain Go (it can call `time.Now()`!) — the contract is "the boundary is deterministic," not "no native code anywhere."

**Why this passes `workflowcheck`:** `workflowcheck` analyzes the *interpreter package*, not the bridge. The interpreter's only direct contact with concurrency primitives is `workflow.Go`, `workflow.NewSelector`, and `workflow.NewBufferedChannel` — all sanctioned. The bridge's internal native goroutine is invisible to the analyzer (it's a function call from the analyzer's POV).

**Concrete code sketch:**
```go
// pkg/interpreter/cancel_watchdog.go
//
// makeCancelChannel returns a native <-chan struct{} that closes when ctx is
// cancelled. The bridging coroutine runs deterministically under workflow.Go,
// so the close happens at the same logical workflow time on replay. Closing
// a Go channel is a constant-time deterministic operation; reads from the
// returned channel inside bridge.CallLambda's native watchdog are safe
// because that watchdog is not workflow code.
func (i *interpreter) makeCancelChannel(ctx workflow.Context) <-chan struct{} {
    ch := make(chan struct{})
    workflow.Go(ctx, func(bctx workflow.Context) {
        bctx.Done().Receive(bctx, nil) // blocks until cancelled
        close(ch)                       // deterministic
    })
    return ch
}
```

The interpreter then calls:
```go
cancelChan := i.makeCancelChannel(ctx)
val, err := bridge.CallLambda(plainCtx, captured, state, bridge.CallOptions{
    Cancel:    cancelChan,
    PrintSink: routePrintToWorkflowLogger(ctx, captured.ID), // D3-22
    Logger:    i.logger,
})
```

**Important corollary:** `makeCancelChannel` must be called **per `bridge.CallLambda` invocation** — each lambda call gets its own native channel + own bridging coroutine. Otherwise on the first cancellation all subsequent calls would see a pre-closed channel. Cleanup: when `CallLambda` returns, the bridging coroutine is still alive (waiting on `Done()`); it's harmless because it'll exit when the workflow finishes. If we want tighter cleanup we can wrap with `workflow.WithCancel(ctx)` and cancel after `CallLambda` returns, but for v1 it's noise.

**Risk acknowledgment:** This is the one place in Phase 3 where Skytime is doing something the SDK was not explicitly designed for. The fallback if integration tests show issues is option (a) — pre-eval cancellation check only — accepting that mid-eval cancellation might wait up to MaxExecutionSteps. Document this in the planner: if (c) shows flakiness in CI replay tests, fall back to (a) and adjust the success criterion.

### Pattern 7: Three Named Client Constructors — WORK-02

**What:** Three constructors, one per connection variant. Each takes a typed `XxxOptions` struct that exposes only the fields that apply.

**Concrete:**
```go
// pkg/worker/client.go

// CloudOptions configures a Temporal Cloud connection. v1.39 default: providing
// APIKey auto-enables TLS (verified against sdk-go release notes for v1.39).
// Don't expose TLSDisabled here — Cloud always uses TLS.
type CloudOptions struct {
    HostPort  string // e.g., "us-west-2.aws.api.temporal.io:7233"
    Namespace string
    APIKey    string // required
    Identity  string // optional; defaults to "skytime/<build_id>" if empty
}

func NewCloudClient(opts CloudOptions) (client.Client, error) {
    if opts.HostPort == "" || opts.Namespace == "" || opts.APIKey == "" {
        return nil, errors.New("CloudOptions: HostPort, Namespace, APIKey all required")
    }
    return client.Dial(client.Options{
        HostPort:    opts.HostPort,
        Namespace:   opts.Namespace,
        Credentials: client.NewAPIKeyStaticCredentials(opts.APIKey), // v1.39 auto-TLS
        Identity:    coalesce(opts.Identity, "skytime/"+defaultBuildID),
        // ConnectionOptions left zero — TLS auto-enabled via Credentials per v1.39
    })
}

// SelfHostedOptions configures a self-hosted Temporal cluster with mTLS.
type SelfHostedOptions struct {
    HostPort        string
    Namespace       string
    ClientCert      tls.Certificate // mTLS client identity
    RootCAs         *x509.CertPool  // server cert verification; nil = system pool
    ServerName      string          // SNI / verify name; defaults to hostname from HostPort
    Identity        string
}

func NewSelfHostedClient(opts SelfHostedOptions) (client.Client, error) {
    if opts.HostPort == "" || opts.Namespace == "" {
        return nil, errors.New("SelfHostedOptions: HostPort, Namespace required")
    }
    tlsCfg := &tls.Config{
        Certificates: []tls.Certificate{opts.ClientCert},
        RootCAs:      opts.RootCAs,
        ServerName:   opts.ServerName,
    }
    return client.Dial(client.Options{
        HostPort:  opts.HostPort,
        Namespace: opts.Namespace,
        ConnectionOptions: client.ConnectionOptions{
            TLS: tlsCfg, // explicit mTLS; Credentials NOT set
        },
        Identity: coalesce(opts.Identity, "skytime/"+defaultBuildID),
    })
}

// DevClientOptions configures a connection to a local dev server (no TLS).
type DevClientOptions struct {
    HostPort  string // default "localhost:7233"
    Namespace string // default "default"
    Identity  string
}

func NewDevClient(opts DevClientOptions) (client.Client, error) {
    return client.Dial(client.Options{
        HostPort:  coalesce(opts.HostPort, "localhost:7233"),
        Namespace: coalesce(opts.Namespace, "default"),
        ConnectionOptions: client.ConnectionOptions{
            TLSDisabled: true, // explicit; v1.39+ default is TLS-on for API-key paths
        },
        Identity: coalesce(opts.Identity, "skytime/"+defaultBuildID),
    })
}
```

**Verification:**
- `client.NewAPIKeyStaticCredentials(apiKey) Credentials` exists in v1.42. Verified via `go doc`. Documentation explicitly states: "TLS is auto-enabled when API key is provided and TLS is not explicitly set/disabled." This is the v1.39 behavior locked into the SDK.
- `client.NewMTLSCredentials(tls.Certificate) Credentials` also exists, but for the self-hosted path we use `ConnectionOptions.TLS` directly — gives more control over `RootCAs` and `ServerName`. Either approach works; the explicit-`tls.Config` route is what most production self-hosted setups use.
- `ConnectionOptions.TLSDisabled bool` field present; setting it to `true` for dev is the v1 idiom.

### Pattern 8: Worker Bootstrap — WORK-01

**Concrete:**
```go
// pkg/worker/worker.go
type WorkerOptions struct {
    RootDir              string // required: filesystem directory of .star files
    BuildID              string // optional: defaults to defaultBuildID (overridable via -ldflags)
    TaskQueue            string // optional: default "skytime"
    UseBuildIDVersioning bool   // optional: default true if BuildID is set
    Extensions           []extension.Extension // for parser registration at boot
    CredentialHandler    extension.CredentialHandler
    // Pass-throughs for tuning:
    MaxConcurrentActivities int
}

type Worker struct {
    sdkWorker worker.Worker
    registry  *interpreter.FlowRegistry
}

func NewWorker(c client.Client, opts WorkerOptions) (*Worker, error) {
    if opts.RootDir == "" {
        return nil, errors.New("WorkerOptions: RootDir is required")
    }
    if opts.BuildID == "" {
        opts.BuildID = defaultBuildID
    }
    if opts.TaskQueue == "" {
        opts.TaskQueue = "skytime"
    }

    // Boot: parse every .star, build registry, freeze.
    registry, err := bootRegistry(opts.RootDir, opts.Extensions)
    if err != nil {
        return nil, fmt.Errorf("worker boot: %w", err)
    }

    // SDK worker
    sdkOpts := worker.Options{
        BuildID:                opts.BuildID,
        UseBuildIDForVersioning: opts.UseBuildIDVersioning || opts.BuildID != "",
        Identity:                "skytime/" + opts.BuildID,
    }
    if opts.MaxConcurrentActivities > 0 {
        sdkOpts.MaxConcurrentActivityExecutionSize = opts.MaxConcurrentActivities
    }
    w := worker.New(c, opts.TaskQueue, sdkOpts)

    // Register the single interpreter workflow + the single ExecuteBatch activity.
    skywf := interpreter.NewWorkflow(registry) // returns SkytimeWorkflow closure
    w.RegisterWorkflowWithOptions(skywf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

    actBatch, err := skyactivity.New(/*dispatch from registry*/, opts.CredentialHandler)
    if err != nil {
        return nil, fmt.Errorf("activity build: %w", err)
    }
    w.RegisterActivityWithOptions(actBatch.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})

    return &Worker{sdkWorker: w, registry: registry}, nil
}

func (w *Worker) Start() error { return w.sdkWorker.Start() }
func (w *Worker) Stop()        { w.sdkWorker.Stop() }
```

**Notes:**
- `worker.New(client, taskQueue, options)` signature verified above.
- `Worker.RegisterWorkflowWithOptions` and `Worker.RegisterActivityWithOptions` are part of the Registry interface (verified above).
- `workflow.RegisterOptions{Name: "SkytimeWorkflow"}` — verified field shape via `go doc go.temporal.io/sdk/internal.RegisterWorkflowOptions`.
- `sdkactivity.RegisterOptions` is the activity-side equivalent; comes from `go.temporal.io/sdk/activity` package (Phase 2 already uses it).
- `Start()` is non-blocking, returns error only on registration / connection issues. `Stop()` is idempotent in v1.42 — but the GoDoc warns "may panic if called a second time." **For v1, just don't call Stop twice; document it.**

### Pattern 9: Build IDs / Worker Versioning

**Current state in v1.42:**
- `worker.Options.BuildID string` — the field is present.
- `worker.Options.UseBuildIDForVersioning bool` — the field is present.
- Both have a "Deprecated: Use [WorkerDeploymentOptions.Version]" GoDoc note.
- `worker.Options.DeploymentOptions WorkerDeploymentOptions{UseVersioning, Version}` — newer alternative.

**Why use BuildID-based versioning for v1:**
1. The deprecation says "deprecated soon" — not "removed" — and the path is well-supported.
2. The `WorkerDeploymentVersion{DeploymentName, BuildID}` model adds a deployment-name concept that Skytime doesn't need at v1 (we have one logical deployment per worker fleet).
3. The assignment-rules API (`UpdateWorkerVersioningRules`) is also deprecated in favor of Worker Deployment Versioning, but for the simple "deploy new build, drain old" flow the old `UpdateWorkerBuildIdCompatibility` API still works and is the documented path in most existing samples.
4. D3-15/D3-20 explicitly say "Skytime exposes Build ID via `worker.Options.BuildID`" — already locked.

**How task routing actually works (verified via SDK GoDoc and Temporal docs):**
- When a workflow is started, the server records the `BuildID` of the worker that picks up its first task. This pins the workflow.
- Subsequent workflow tasks for that workflow are only dispatched to workers with a `BuildID` in the same compatibility set (or the exact same BuildID under default rules).
- Server-side filtering is the mechanism — the SDK doesn't poll separate queues per BuildID; it polls the same task queue, and the server filters tasks to the worker's BuildID.
- **What happens if no BuildID is set:** workers run without versioning; old "binary-checksum"-style versioning may apply, but it's not deterministic and can lead to non-determinism panics on `.star` updates. This is exactly what D3-20's "default = build-time-injected variable" prevents.

**Assignment rules deployment (NOT shipped in v1; documented for Phase 6 README):**
```go
// Sketch — for reference only; v1 uses single-BuildID worker fleets.
client.UpdateWorkerVersioningRules(ctx, client.UpdateWorkerVersioningRulesOptions{
    TaskQueue: "skytime",
    Operation: client.UpdateVersioningRulesAddAssignmentRule{
        Rule: client.VersioningAssignmentRule{TargetBuildID: "v2"},
    },
    ConflictToken: ...,
})
```
This is overkill for v1; just deploy a new worker with a new BuildID and let the server pin new workflows to it. Old workflows continue running on the old worker until drained.

### Pattern 10: workflow.SideEffect Notes (kept here even though we recommend NOT using it for hash recording)

**API shape (verified):** `workflow.SideEffect(ctx Context, f func(ctx Context) interface{}) converter.EncodedValue`. Returns an `EncodedValue` you `Get(&dest)` into.

**Determinism contract:**
- The callback runs **only on first execution**. On replay, the recorded result is returned without invoking the callback.
- The callback **may** be non-deterministic — it CAN call `time.Now`, `rand.Int`, etc.
- The result must be JSON-marshalable (the default DataConverter is JSON).
- Common pitfall (from GoDoc): mutating closure state from inside the callback is broken because on replay the callback doesn't run and the closure variable stays at its initial value. Must use the returned `EncodedValue.Get`.

**If we did use it for hash recording (we shouldn't, but for reference):**
```go
var contentHash string
err := workflow.SideEffect(ctx, func(_ workflow.Context) interface{} {
    return computeSha256(/* somehow */)
}).Get(&contentHash)
```
The problem: `computeSha256` would have to read the .star file from disk inside the workflow, which is exactly what `WorkflowInput`-carries-hash avoids. **Pass it in.**

### Anti-Patterns to Avoid

- **Putting `*starlark.Function` in any payload type** — rejected at registry boundary; not a real risk if the WorkflowInput shape is rigorously enforced.
- **Calling `workflow.GetInfo(ctx)` from inside `bridge.CallLambda`** — would couple the bridge to workflow context. The interpreter passes `parentInfo` values into options structs, never into bridge.
- **Sharing one `*starlark.Thread` across `for_each_parallel` branches** — Phase 1 already mandates fresh thread per `CallLambda` call; the for_each_parallel pattern naturally satisfies this because each branch's lambda eval calls into the bridge separately.
- **Using `workflow.LocalActivity` for `ExecuteBatch`** — local activities have a 60s soft cap and no retries-across-task-failures. Skytime batches can be much longer. Always regular `ExecuteActivity`.
- **Calling `worker.Stop()` twice** — panics in v1.42 (per GoDoc). Idempotency wrapper at the consumer's `main.go` level.
- **Mixing `workflow.NewChannel` (unbuffered) with `for_each_parallel` semaphore** — unbuffered channels deadlock immediately. Must be `NewBufferedChannel`.
- **Embedding `dag.Flow` in WorkflowInput** — D3-05 forbids; the struct rewrite from Phase 1's `Flow *dag.Flow` field is a Wave 0 task.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Cancellation propagation across goroutines | Custom `select`-based cancellation tree | `workflow.WithCancel(parent)` + `cancel()` | Workflow scheduler handles propagation; child contexts auto-cancel on parent close. |
| Coroutine-safe waiting on multiple futures | Custom busy-wait or sleep-loop | `workflow.NewSelector(ctx)` + `Selector.AddFuture(...)` | Native `select` is non-deterministic in workflow code; Selector is the supported primitive. |
| Concurrency bound for fan-out | Native `chan struct{}` semaphore | `workflow.NewBufferedChannel(ctx, n)` | Native channels are non-deterministic. |
| Blocking until a condition is true | Custom polling loop | `workflow.Await(ctx, func() bool { ... })` | Await re-evaluates on each workflow state transition; deterministic. |
| Replay-safe history-event recording | Custom history mutation | `workflow.SideEffect` for genuinely-non-deterministic values; `WorkflowInput` for everything else | SDK records SideEffect results; we get replay safety for free. |
| Workflow logging in replay-safe form | Stdlib `log` or `slog.Default()` directly | `workflow.GetLogger(ctx)` | The SDK logger skips replay logs by default (avoiding double-logging on every replay). |
| Activity scheduling with retry policy | Hand-rolled HTTP retry | `workflow.ExecuteActivity(activityCtxWithRetryPolicy, ...)` | Server-side retry honors the RetryPolicy, integrates with timeouts. |
| Reading workflow execution metadata (run id, attempt, etc.) | Stash in input | `workflow.GetInfo(ctx)` | Replay-safe by definition. |
| Cross-version compatibility for replay | `workflow.GetVersion` patches everywhere | Build IDs (worker versioning) | Per D3-03 and §"Build IDs / Worker Versioning" — operational, not authorial. |
| Replay verification | Custom history diff | `worker.WorkflowReplayer` + `ReplayWorkflowHistory` | Built-in replay engine; identical to production scheduling. See §"Replay-Twice Test." |
| Static-analyze workflow code for non-determinism | Custom AST walker | `go.temporal.io/sdk/contrib/tools/workflowcheck` | Already covers `time.Now`, `rand.*`, native `go`, unsorted map iteration, env access. Run as a `go vet` tool. |
| Filesystem walking for `--rootdir` | Custom recursive walker | `filepath.WalkDir` (stdlib) | Adequate; the boot step is one-shot. No need for fancy. |
| sha256 of file bytes for content hash | Custom hash | `crypto/sha256` (stdlib) — already used for D-18 lambda IDs | Hash already computed during Phase 1 lambda capture (`pkg/dag/lambda.ComputeLambdaID` reuses `sha256.Sum256(fileBytes)`). Worker boot can call the same primitive. |

**Key insight:** Temporal's workflow primitives ARE the standard library for deterministic concurrency. Anything that looks like "goroutine + channel + select" in workflow code MUST go through `workflow.Go` + `workflow.Channel` + `workflow.Selector`. The interpreter is the customer of these APIs, not a re-implementer of them.

## Common Pitfalls

### Pitfall 1: `workflow.Context.Done()` Reads Are Not Free

**What goes wrong:** Naively calling `ctx.Done().Receive(ctx, nil)` from the workflow goroutine to "check cancellation" blocks the goroutine. Same call from inside `for_each_parallel` branches blocks each branch. If the watchdog pattern is implemented wrong, the workflow becomes unresponsive.

**Why it happens:** `workflow.Channel.Receive` is a coroutine-yielding call. It looks like a function but is actually a "wait" point in the deterministic scheduler.

**How to avoid:** Only call `ctx.Done().Receive(...)` inside a `workflow.Go(ctx, ...)` coroutine — never from the main walk loop. The cancellation watchdog (Pattern 6) does this.

**Warning signs:** Workflow that takes longer to complete than expected; CI flakiness; `TestWorkflowEnvironment` reporting "workflow blocked at ...".

### Pitfall 2: Non-Deterministic Map Iteration in the Interpreter — D3-23

**What goes wrong:** Walking a `map[string]any` (e.g., `state.Vars`) inside the interpreter using `for k, v := range m {}` produces different iteration order across runs. Replay diverges.

**Why it happens:** Go's `range` over maps is randomized by language spec. `workflowcheck` flags this.

**How to avoid:** Sort keys before iterating. The bridge's `ToStarlarkStruct` already does this (Phase 1). The interpreter must too — anywhere it touches a map. Pattern: extract `keys := sortedKeys(m); for _, k := range keys { ... }`.

**Warning signs:** Replay-twice CI test fails. `workflowcheck` output mentions "iterates non-deterministic map." Determinism panic at workflow runtime.

### Pitfall 3: Calling `workflow.Now`, `workflow.Sleep`, etc., From Inside `bridge.CallLambda`

**What goes wrong:** Adding a new bridge primitive that uses `workflow.Now` "for convenience" embeds workflow.Context in the bridge. Pitfall #4 from PITFALLS.md (context bleed).

**Why it happens:** Lambda authors might want `ctx.now`. The temptation is to expose a new builtin that reaches for `workflow.Now`.

**How to avoid:** Time-current values are passed via `init_state` if needed (the trigger or the workflow's first SideEffect computes them and stuffs them in state). Lambdas read `ctx.now` as a state attribute, not as a builtin. The lambda-time globals (D-20) intentionally don't include `now()`.

**Warning signs:** PR adds a new entry to `lambda_globals.go`'s allowlist; firewall test for `pkg/bridge` complains about `go.temporal.io/sdk/workflow` import.

### Pitfall 4: `workflow.ExecuteChildWorkflow` Search-Attribute Field Confusion

**What goes wrong:** Setting both `cwo.SearchAttributes` (deprecated, `map[string]interface{}`) and `cwo.TypedSearchAttributes` (`temporal.SearchAttributes`) on `ChildWorkflowOptions` produces undefined behavior — possibly the deprecated one wins, possibly the typed one. Search-attribute propagation may silently break.

**Why it happens:** v1.42 marks `SearchAttributes` deprecated but doesn't remove it; both fields are accepted. Code that copies one but reads the other diverges.

**How to avoid:** Use `TypedSearchAttributes` exclusively. Read parent attrs via `workflow.GetTypedSearchAttributes(ctx)`. Pass directly to `cwo.TypedSearchAttributes`. Leave `cwo.SearchAttributes` as zero value.

**Warning signs:** Phase-3 e2e test that tags parent with a search attr and asserts child has it — fails silently if the wrong field is set.

### Pitfall 5: Worker `Stop()` Called Twice → Panic

**What goes wrong:** Consumer's `main.go` has both a SIGTERM handler and a deferred `w.Stop()` call. On signal, both fire; second one panics.

**Why it happens:** GoDoc explicitly: "This may panic if called a second time."

**How to avoid:** Wrap with `sync.Once`:
```go
var stopOnce sync.Once
shutdown := func() { stopOnce.Do(w.Stop) }
defer shutdown()
go func() { <-sigChan; shutdown() }()
```
Document this idiom in the Phase 6 README.

**Warning signs:** Test that drives clean shutdown panics in CI; production worker process exits with stack trace.

### Pitfall 6: Replay-Twice Test Lies When Workflow Is Trivial

**What goes wrong:** A trivial workflow (e.g., one Step, no lambdas) replays cleanly even if there's a determinism bug somewhere — the bug just doesn't get exercised. Replay-twice test passes; production breaks weeks later.

**Why it happens:** Replay only catches non-determinism on code paths that actually execute. Edge cases (specific input values, branching via if_cond, fan-out > 1) might not be in the test.

**How to avoid:** The replay-twice test must exercise every node type at least once: a fixture flow that includes `step`, `if_cond` with both branches, `script`, `for_each_parallel` with N>1, and `call_flow`. Write this as the canonical "kitchen sink" fixture.

**Warning signs:** Phase-3 verification only includes simple-flow replay tests; coverage matrix shows for_each_parallel + call_flow not exercised.

### Pitfall 7: Build ID Mismatch on First-Time Customer Deploy

**What goes wrong:** Customer deploys the first version of their `.star` flows + worker. They didn't set BuildID (or used a default like `"dev"`). They edit a `.star` file, redeploy, BuildID still `"dev"`. Workflows started under the old code now hit the new worker. Lambda IDs mismatch (cosmetic-edit-changes-D-18-prefix per D-18 design). Determinism panic.

**Why it happens:** D3-20's "default = build-time-injected `defaultBuildID = "dev"`" is great for dev but a footgun in prod. If customer doesn't override via `-ldflags`, they always get `"dev"`.

**How to avoid:**
- Phase 6 README must show the canonical `-ldflags "-X .../skytime/worker.defaultBuildID=$(git rev-parse HEAD)"` invocation.
- Worker bootstrap should `slog.Warn` if `BuildID == "dev"` at startup (visible in worker logs).
- Documentation must emphasize: "running without a real BuildID is for dev only; production deployments must set it."

**Warning signs:** Customer's first prod incident is a determinism panic. Worker logs lack any build-version info.

### Pitfall 8: Activity Schedule From Inside a Lambda

**What goes wrong:** A clever lambda author tries `ctx.activity.run(...)` or similar. None of this exists per D-20 / Phase 1, but if a future builtin slips through review and exposes anything that calls `workflow.ExecuteActivity` from a lambda, the consequences are: (a) batching defeated; (b) mocking single-point-of-interception broken; (c) the lambda's deterministic eval is now coupled to a Temporal coroutine yield.

**Why it happens:** The principle of least surprise pushes toward letting lambdas "do things" beyond pure data shaping.

**How to avoid:** Strict allowlist. Phase 1's D-20 already locks the lambda-time globals. Any new builtin proposal must go through a phase decision (not a code review).

**Warning signs:** PR adds a new entry to `lambda_globals.go`. Phase 6's example code uses a builtin not in the original D-20 list.

## Runtime State Inventory

Phase 3 is **not** a rename/refactor phase — it's net-new code (`pkg/interpreter`, `pkg/worker`) with one shape change (`pkg/dag.WorkflowInput` field rewrite, locked under D3-04/D3-05). No runtime state migration is required: the project has no live workflows, no databases, no registered tasks, no installed packages outside the local Go module. **Section omitted; if Phase 3 is repackaged for a customer migration later, it must be filled in.**

## Code Examples

Verified patterns from official sources, paraphrased for Skytime use.

### Example 1: SkytimeWorkflow Skeleton

```go
// pkg/interpreter/workflow.go
package interpreter

import (
    "fmt"

    "go.temporal.io/sdk/temporal"
    "go.temporal.io/sdk/workflow"

    "github.com/mikelalcon/skytime/pkg/dag"
)

// NewWorkflow returns the SkytimeWorkflow function bound to a frozen registry.
// Worker boot calls NewWorkflow(registry) and passes the result to
// w.RegisterWorkflowWithOptions.
//
// Source: workflow function shape per
// https://pkg.go.dev/go.temporal.io/sdk/workflow#example-package
func NewWorkflow(registry *FlowRegistry) func(workflow.Context, dag.WorkflowInput) (map[string]any, error) {
    return func(ctx workflow.Context, input dag.WorkflowInput) (map[string]any, error) {
        info := workflow.GetInfo(ctx)
        logger := workflow.GetLogger(ctx)
        logger.Info("skytime workflow start",
            "flow_name", input.FlowName,
            "content_hash", input.ContentHash,
            "build_id", info.BinaryChecksum, // populated when UseBuildIDForVersioning=true
            "run_id", info.WorkflowExecution.RunID,
        )

        parsed, ok := registry.lookup(input.FlowName, input.ContentHash)
        if !ok {
            return nil, temporal.NewNonRetryableApplicationError(
                fmt.Sprintf("flow %s@%s not found in worker registry; use Build IDs to drain old workflows",
                    input.FlowName, input.ContentHash),
                "FlowNotInRegistry", nil,
            )
        }

        i := newInterpreter(ctx, parsed, input.InitState, logger)
        if err := i.walkBody(ctx, parsed.Flow.Body); err != nil {
            return nil, err
        }
        return i.state.snapshot(), nil
    }
}
```

### Example 2: Step Walker With Per-Step Activity Options

```go
// pkg/interpreter/walk_step.go
func (i *interpreter) walkStep(ctx workflow.Context, step *dag.Step) error {
    actx := workflow.WithActivityOptions(ctx, i.activityOptionsForStep(step))
    var results []dag.ActionResult
    if err := workflow.ExecuteActivity(actx, "ExecuteBatch", step.Actions).Get(ctx, &results); err != nil {
        return err
    }
    return i.recordStepResults(step, results)
}

func (i *interpreter) activityOptionsForStep(step *dag.Step) workflow.ActivityOptions {
    opts := workflow.ActivityOptions{
        StartToCloseTimeout: i.computeBatchTimeout(step), // D2-15 (Phase 2)
        HeartbeatTimeout:    60 * time.Second,
    }
    if step.TaskQueue != "" {
        opts.TaskQueue = step.TaskQueue
    } else if i.flow.TaskQueue != "" {
        opts.TaskQueue = i.flow.TaskQueue
    }
    if step.Retry != nil {
        opts.RetryPolicy = toTemporalRetryPolicy(step.Retry)
    }
    return opts
}
```

Source: `workflow.WithActivityOptions` + `workflow.ExecuteActivity` GoDoc (verified).

### Example 3: for_each_parallel Cancel-On-Error Pattern

(See full code in §"Pattern 4" above — code is the example.)

### Example 4: Cancellation Watchdog Bridge

```go
// pkg/interpreter/cancel_watchdog.go
func (i *interpreter) makeCancelChannel(ctx workflow.Context) <-chan struct{} {
    ch := make(chan struct{})
    workflow.Go(ctx, func(bctx workflow.Context) {
        bctx.Done().Receive(bctx, nil)
        close(ch)
    })
    return ch
}
```

(Full discussion in §"Pattern 6.")

### Example 5: Three Named Client Constructors

(See full code in §"Pattern 7" above.)

### Example 6: Replay-Twice Integration Test

```go
// pkg/interpreter/replay_test.go
func TestReplay_KitchenSinkFlow(t *testing.T) {
    var ts testsuite.WorkflowTestSuite
    env := ts.NewTestWorkflowEnvironment()

    // Mock ExecuteBatch to return deterministic per-action OK results.
    env.OnActivity("ExecuteBatch", mock.Anything).Return(
        []dag.ActionResult{
            dag.OkResult{Idx: 0, Output: fakeOutput{Value: "ok"}},
        }, nil)

    registry := buildKitchenSinkRegistry(t)
    env.RegisterWorkflowWithOptions(NewWorkflow(registry), workflow.RegisterOptions{Name: "SkytimeWorkflow"})

    input := dag.WorkflowInput{
        FlowName:    "kitchen_sink",
        ContentHash: registry.contentHashFor("kitchen_sink"),
        InitState:   map[string]any{"x": int64(1)},
    }
    env.ExecuteWorkflow(NewWorkflow(registry), input)

    require.True(t, env.IsWorkflowCompleted())
    require.NoError(t, env.GetWorkflowError())

    // Now replay the recorded history twice via WorkflowReplayer.
    history := env.GetWorkflowHistory() // hypothetical accessor; if not exposed, read from RunHistory file
    replayer := worker.NewWorkflowReplayer()
    replayer.RegisterWorkflowWithOptions(NewWorkflow(registry), workflow.RegisterOptions{Name: "SkytimeWorkflow"})
    require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
    require.NoError(t, replayer.ReplayWorkflowHistory(nil, history)) // twice; assert no divergence
}
```

**Note on `env.GetWorkflowHistory`:** the public testsuite API doesn't expose history directly; the standard pattern is to use `client.HistoryFromJSON` against a recorded JSON file. For unit-tests-without-a-real-server, the alternate is to assert determinism via `env.ExecuteWorkflow` running twice on the same input + registry (no replay engine, but verifies same input → same output). The full replay-twice test is integration-tier and lives in `pkg/interpreter/integration_test.go`, gated by a build tag (e.g., `//go:build integration`).

Source: `worker.NewWorkflowReplayer` + `WorkflowReplayer.ReplayWorkflowHistory` GoDoc (verified above).

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `client.Options.HostPort` + `TLSDisabled: false` + manual `Authorization` header | `client.Options.Credentials = NewAPIKeyStaticCredentials(key)` (auto-TLS) | v1.39 (2025) | Skytime's `NewCloudClient` MUST use the new path; old samples that explicitly set TLS configs alongside an API key will silently fight the SDK. |
| `worker.Options.BuildID + UseBuildIDForVersioning` | `worker.Options.DeploymentOptions{Version, UseVersioning}` | "Soon" (deprecation note in v1.42 GoDoc) | For v1, BuildID still works fully and is simpler. Phase 6 README acknowledges deprecation; revisit if customer needs co-versioning. |
| `ChildWorkflowOptions.SearchAttributes` (untyped map) | `ChildWorkflowOptions.TypedSearchAttributes` (`temporal.SearchAttributes`) | v1.40+ | Use typed only; never set both fields. |
| `ChildWorkflowOptions.Namespace` | (deprecated; cross-namespace operations disabled by default) | v1.42 | Ignore; Skytime never spans namespaces. |
| Custom `gob`-encoded payloads via DataConverter | Default JSON DataConverter + structured types | (always) | Skytime sticks with default DataConverter; the WorkflowInput shape is JSON-serializable. |

**Deprecated/outdated (do NOT use):**
- `go.temporal.io/temporal` (pre-1.0 import path) — gone; always `go.temporal.io/sdk/...`.
- `workflow.GetVersion` / `workflow.Patch` for `.star` author-facing versioning — explicitly D3-03 / "no version() builtin."
- `workflow.LocalActivity` for `ExecuteBatch` — wrong fit; batch durations exceed local activity's lifetime cap.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain (1.25+) | All Go code | ✓ (verified — `go 1.25.0` in `go.mod`) | inferred from existing build | — |
| `go.temporal.io/sdk` | All workflow/activity/client code | ✓ | `v1.42.0` (in `go.mod`) | — |
| `go.starlark.net` | Lambda eval (already in use via `pkg/bridge`) | ✓ | pseudo-version pinned | — |
| Temporal dev server (`temporal server start-dev`) | Local integration testing of `NewDevClient` | ✗ on CI; available in dev environments | — | Use `testsuite.TestWorkflowEnvironment` for the canonical CI path; integration tests against real dev server stay local-only and gated behind a build tag. `testsuite.StartDevServer` exists in v1.42 but downloads the binary — only acceptable for local dev, not CI on a fresh runner. |
| `workflowcheck` CLI | INTRP-07 verification | ✗ not pre-installed | install via `go install go.temporal.io/sdk/contrib/tools/workflowcheck@latest` in CI | Make CI install it; failure to install = CI failure. |

**Missing dependencies with no fallback:** None — `workflowcheck` install is one CI step.

**Missing dependencies with fallback:** Real Temporal dev server — fallback is `TestWorkflowEnvironment` for unit/integration tests; an opt-in `//go:build integration` test target hits the real dev server when developers run it locally.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `stretchr/testify@v1.11.1` (already in use) + `go.temporal.io/sdk/testsuite` (Phase-2 already uses) |
| Config file | None — Go tests are auto-discovered |
| Quick run command | `go test ./pkg/interpreter/... ./pkg/worker/... -run TestX` (substitute `TestX` with the specific test) |
| Full suite command | `go test -race ./pkg/interpreter/... ./pkg/worker/... ./pkg/dag/... ./pkg/parser/...` |
| Static analysis | `workflowcheck ./pkg/interpreter/...` (install separately) — INTRP-07 |
| Replay test | `go test -tags=integration ./pkg/interpreter/...` — Pitfall #6 |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INTRP-01 | `SkytimeWorkflow` walks any `dag.Flow` | integration (testsuite) | `go test ./pkg/interpreter/ -run TestSkytimeWorkflow_KitchenSink` | ❌ Wave 0 — `pkg/interpreter/workflow_test.go` |
| INTRP-02 | Replay-twice equality holds | integration (testsuite) | `go test ./pkg/interpreter/ -run TestReplay_KitchenSinkFlow` | ❌ Wave 0 — `pkg/interpreter/replay_test.go` |
| INTRP-03 | `if_cond` and `script` produce zero history events | integration | `go test ./pkg/interpreter/ -run TestInlineEval_NoHistoryEvents` (asserts `env.GetWorkflowHistory().Events` count == expected baseline; lambda eval doesn't add events) | ❌ Wave 0 |
| INTRP-04 | `for_each_parallel` honors max_concurrency, cancels on non-retryable error | integration + unit | `go test ./pkg/interpreter/ -run TestForEach -race` | ❌ Wave 0 — `pkg/interpreter/walk_foreach_test.go` |
| INTRP-05 | `call_flow` invokes child workflow with retry/search-attr inheritance | integration | `go test ./pkg/interpreter/ -run TestCallFlow_ChildInheritsRetryAndSearchAttrs` | ❌ Wave 0 |
| INTRP-06 | Replay-twice CI test (also covers map sort) | integration | `go test ./pkg/interpreter/ -run TestReplay -race` | ❌ Wave 0 |
| INTRP-07 | Interpreter passes `workflowcheck` | static | `workflowcheck ./pkg/interpreter/...` (CI step; non-zero output = failure) | N/A — CI job |
| WORK-01 | Worker registers `SkytimeWorkflow` + `ExecuteBatch` and starts | unit | `go test ./pkg/worker/ -run TestNewWorker_RegistersWorkflowAndActivity` (with a fake client) | ❌ Wave 0 — `pkg/worker/worker_test.go` |
| WORK-02 | Three named client constructors handle Cloud / mTLS / dev-server | unit | `go test ./pkg/worker/ -run TestClientConstructors` (mocks `client.Dial` via `client.NewLazyClient` or in-memory expectations) | ❌ Wave 0 — `pkg/worker/client_test.go` |
| WORK-03 | Library-embed pattern works end-to-end | integration | `go test -tags=integration ./pkg/worker/ -run TestEmbed_FullStack` (uses real `temporal server start-dev` if available, otherwise skip) | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `go test ./pkg/<package_under_change>/ -run TestX -race` (the specific test added/modified)
- **Per wave merge:** `go test -race ./pkg/...` (full repo unit + integration via testsuite; excludes `//go:build integration` real-server tests)
- **Phase gate (`/gsd:verify-work`):** Full suite + `workflowcheck ./pkg/interpreter/...` + the integration-tag replay-twice test

### Wave 0 Gaps

- [ ] `pkg/interpreter/doc.go` — package-level documentation + firewall comment
- [ ] `pkg/interpreter/workflow_test.go` — INTRP-01 kitchen-sink test
- [ ] `pkg/interpreter/replay_test.go` — INTRP-02 / INTRP-06 replay-twice
- [ ] `pkg/interpreter/walk_foreach_test.go` — INTRP-04 cancellation/concurrency
- [ ] `pkg/interpreter/walk_callflow_test.go` — INTRP-05 child workflow
- [ ] `pkg/interpreter/registry_test.go` — registry boot + lookup, content-hash mismatch error
- [ ] `pkg/worker/doc.go` — package docs
- [ ] `pkg/worker/client_test.go` — WORK-02 three constructors
- [ ] `pkg/worker/worker_test.go` — WORK-01 registration
- [ ] `pkg/worker/boot_test.go` — registry-boot fixture coverage
- [ ] `pkg/worker/firewall_test.go` — assert only `pkg/worker` and `pkg/interpreter` (besides `pkg/activity`) import `go.temporal.io/sdk/...`
- [ ] DSL retrofit fixtures: 2 `.star` files with `task_queue=...` (one valid, one invalid: empty string)
- [ ] CI job: `workflowcheck ./pkg/interpreter/...` — INTRP-07
- [ ] CI job: `go test -tags=integration ./pkg/interpreter/...` — replay-twice

## Sources

### Primary (HIGH confidence)

- `go doc go.temporal.io/sdk/workflow.SideEffect` — full semantics + closure-mutation pitfall (verified locally against vendored v1.42.0).
- `go doc go.temporal.io/sdk/internal.WorkerOptions` — full WorkerOptions struct including `BuildID`, `UseBuildIDForVersioning`, `DeploymentOptions`, `Identity`, deprecation notes (verified locally).
- `go doc go.temporal.io/sdk/internal.WorkflowInfo` — full WorkflowInfo with `RetryPolicy`, `SearchAttributes`, `Memo`, `BinaryChecksum`, `WorkflowExecution`, etc. (verified locally).
- `go doc go.temporal.io/sdk/internal.ClientOptions` — full ClientOptions with `Credentials`, `ConnectionOptions{TLS, TLSDisabled}` (verified locally).
- `go doc go.temporal.io/sdk/internal.ConnectionOptions` — TLS-related fields, mutual exclusion of `TLS` and `TLSDisabled` (verified locally).
- `go doc go.temporal.io/sdk/internal.ChildWorkflowOptions` — full struct with `RetryPolicy`, `TypedSearchAttributes`, `Memo`, `TaskQueue`, deprecated `Namespace` (verified locally).
- `go doc go.temporal.io/sdk/internal.RetryPolicy` — fields `InitialInterval`, `BackoffCoefficient`, `MaximumInterval`, `MaximumAttempts`, `NonRetryableErrorTypes` (verified locally).
- `go doc go.temporal.io/sdk/internal.ActivityOptions` — full ActivityOptions with `TaskQueue`, `StartToCloseTimeout`, `RetryPolicy`, `HeartbeatTimeout`, etc. (verified locally).
- `go doc go.temporal.io/sdk/internal.Selector` — `AddReceive`, `AddFuture`, `AddSend`, `AddDefault`, `Select`, `HasPending` (verified locally).
- `go doc go.temporal.io/sdk/workflow.Go` / `WithCancel` / `NewBufferedChannel` / `Await` — confirmed signatures (verified locally).
- `go doc go.temporal.io/sdk/internal.Context` — `Done() Channel` (the workflow.Channel return) confirmed; this is the central fact for the cancellation watchdog (verified locally).
- `go doc go.temporal.io/sdk/internal.Future` / `ChildWorkflowFuture` — `Get(ctx, &val) error` + `IsReady() bool` + child-specific `GetChildWorkflowExecution()` (verified locally).
- `go doc go.temporal.io/sdk/temporal.NewApplicationError` / `NewNonRetryableApplicationError` — error constructors used for FlowNotInRegistry, LambdaNotFound (verified locally).
- `go doc go.temporal.io/sdk/worker.Worker` / `worker.New` / `WorkflowReplayer` — `Start/Stop/Run` lifecycle + `RegisterWorkflowWithOptions` + `ReplayWorkflowHistory` (verified locally).
- `go doc go.temporal.io/sdk/client.NewAPIKeyStaticCredentials` — confirms TLS auto-enable when API key set (v1.39+ behavior, verified locally).
- `go doc go.temporal.io/sdk/internal.RegisterWorkflowOptions` — `Name`, `DisableAlreadyRegisteredCheck`, `VersioningBehavior` (verified locally).
- v1.42.0 release notes (https://github.com/temporalio/sdk-go/releases/tag/v1.42.0) — Go floor 1.24, no Worker Versioning changes, no TLS/API-key changes.
- v1.39.0 release notes (https://github.com/temporalio/sdk-go/releases/tag/v1.39.0) — confirmed "TLS auto-enabled when API key provided" breaking change.

### Secondary (MEDIUM confidence)

- pkg.go.dev for `workflowcheck` — confirmed import path `go.temporal.io/sdk/contrib/tools/workflowcheck`, install via `go install ...@latest`, flags `-config`, `-show-pos`, `-single-line`, `-help`. Status: separate command module, not vendored under main SDK module — confirmed by `ls $(go env GOMODCACHE)/go.temporal.io/sdk@v1.42.0/contrib/` (the directory does not exist in the v1.42.0 module).
- GitHub `temporalio/sdk-go/contrib/tools/workflowcheck` README — usage `workflowcheck ./...`, integration with `go vet -vettool`.

### Tertiary (LOW confidence — flagged for validation)

- Worker Deployment Versioning vs BuildID-based versioning trade-off — the deprecation note in v1.42 GoDoc says BuildID is "deprecated soon"; the actual timeline is uncertain. **Decision for v1: use BuildID; revisit at first customer co-version need.** No higher-confidence source until the SDK ships the removal.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — every pinned dep verified via `go doc` against the local module cache.
- Architecture (lambda serialization, walker structure): HIGH — Option B is locked, the walker maps cleanly onto well-documented Temporal primitives.
- Cancellation watchdog: MEDIUM — the design is sound but combines workflow coroutines with a native goroutine inside `bridge.CallLambda` in a way the SDK doesn't explicitly bless. Falls back to "pre-eval check only" if integration tests show flakiness.
- for_each_parallel concurrency: HIGH — `workflow.Go` + `workflow.NewBufferedChannel` + `workflow.WithCancel` is the textbook pattern.
- Three named client constructors: HIGH — `client.NewAPIKeyStaticCredentials` and `ConnectionOptions{TLS, TLSDisabled}` are verified.
- Build IDs: MEDIUM — current API works in v1.42 but is marked "deprecated soon"; v1 ships with BuildID-based versioning per locked decision.
- Pitfalls: HIGH — the seam-specific risks (cancellation watchdog, search-attr-field-confusion, `Stop()` panic-twice) are concrete and verified.

**Research date:** 2026-04-30
**Valid until:** 30 days for stable Temporal SDK APIs; 7 days for Build ID guidance (deprecation timing may shift in a future minor).
