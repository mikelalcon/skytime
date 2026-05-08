# Phase 3: Lambda-Serialization Decision + Interpreter + Worker - Context

**Gathered:** 2026-04-29
**Status:** Ready for planning

<domain>
## Phase Boundary

The execute side of Skytime. Decide and implement how `*starlark.Function` lambdas survive Temporal's serialization boundary, then build the generic `SkytimeWorkflow` that walks any parsed `dag.Flow`, plus the worker bootstrap supporting Temporal Cloud / self-hosted-mTLS / local dev-server. **The lambda-serialization decision must be locked BEFORE any interpreter code is written** (per ROADMAP success criterion #1).

Phase 3 owns 10 v1 requirements: INTRP-01..07, WORK-01..03.

What's already in place (do not redo):
- D2-01..18 from Phase 2 — `pkg/activity` is the dispatch target; `dag.ActionResult` sealed sum is the per-action wire format
- D-18 lambda IDs (`sha256(fileBytes)[:8] + ":" + line + ":" + col`)
- D-21 print routing (Phase 1 stubbed; Phase 3 wires the workflow logger)
- D-22 `MaxExecutionSteps = 10_000_000` default per `CallLambda`
- `WorkflowInput` struct skeleton with `TODO(phase3)` marker — Phase 3 fills in the serialization strategy
- Mixed-batch policy (Policy D): blocks are homogeneous-by-construction at parse time, so the interpreter never sees a mixed batch

</domain>

<decisions>
## Implementation Decisions

### Lambda Serialization Mechanism (HEADLINE)

- **D3-01: Strategy is "re-parse on workflow start" (Option B)** — NOT custom DataConverter, NOT inline source embedding. Workflow's first event is a `workflow.SideEffect` that records the `content_hash` of the `.star` file the workflow was started against. On every subsequent workflow tick (including replays), the worker uses `(flow_name, content_hash)` to look up the parsed flow + lambda map from its in-memory registry.
- **D3-02: Workers DO NOT serialize `*starlark.Function` to history.** Function values stay in worker-local memory. Only `LambdaID` strings cross the workflow's boundary. The bridge's `CallLambda` receives the `*starlark.Function` from the worker's flow registry, looked up by `LambdaID`.
- **D3-03: Versioning relies on Temporal Build IDs (NOT Skytime DSL primitives).**
  - Each worker tags itself with a `BuildID` at startup.
  - Temporal routes workflow tasks to workers with compatible Build IDs (workflows pin to the Build ID under which they started).
  - When a `.star` file is edited, customers build a new worker binary, tag with a new Build ID, deploy alongside; old workflows drain on old workers, new workflows run on new workers.
  - Skytime exposes Build ID via `worker.Options.BuildID` (D3-15). No `version()` builtin in Starlark, no `workflow.Patch`-style DSL surface.
  - `workflow.GetVersion`/`Patch` are NOT exposed to `.star` authors. Versioning is operational, not authorial.

### WorkflowInput & Wire Format

- **D3-04: WorkflowInput shape is `{flow_name string, content_hash string, init_state map[string]any}`.**
  - `flow_name` — looked up in the worker's flow registry.
  - `content_hash` — sha256 of the `.star` file bytes that defined `flow_name`. Computed on the worker at startup; recorded on workflow start via `workflow.SideEffect` so it persists across replays.
  - `init_state` — pure data from the workflow trigger (HTTP request, signal, scheduler).
- **D3-05: The full `dag.Flow` is NOT embedded in WorkflowInput.** It's looked up from the registry. This is the smallest payload and matches the Build-ID + filesystem-snapshot deployment model. Phase 1's `WorkflowInput.Flow *dag.Flow` field gets updated to `FlowName string` + `ContentHash string` (backward-incompatible wire-format change; no consumers yet).
- **D3-06: Worker rejects mismatched replays cleanly.** If a worker tries to handle a workflow whose recorded `content_hash` isn't in its registry, fail fast with a clear error (`"workflow expects flow X@<hash>; this worker has flow X@<other_hash>; use Build IDs to drain old workflows"`). Defense in depth — Temporal's task routing should prevent this.

### Source Delivery & Worker Boot

- **D3-07: `.star` source files reach workers via filesystem path (NOT `go:embed`).**
  - Worker takes a `--rootdir` flag (or `SKYTIME_ROOT` env var) at startup.
  - At boot, walks the directory, parses every `.star` file, computes `content_hash` for each, builds the registry.
  - Registry is **frozen after boot** — no hot reload during the worker's lifetime. The Build ID corresponds to "this binary + these files at boot." If `.star` files on disk change after boot, the worker doesn't notice (and shouldn't).
  - Phase 6 example project documents this: deploy = build binary + snapshot `.star` files + tag Build ID.

### Backend Abstraction

- **D3-08: NO `wf` interface for backend pluggability.** The interpreter directly imports `go.temporal.io/sdk/workflow`. Phase 1 had no abstraction; Phase 2's `pkg/activity` directly imports `go.temporal.io/sdk/activity`. Stay consistent. If Cadence or another backend ever surfaces real demand, refactor then. YAGNI.

### `call_flow` Semantics

- **D3-09: `call_flow(name=...)` ALWAYS invokes a Temporal child workflow.** No inline macro-expansion. Each call_flow becomes `workflow.ExecuteChildWorkflow`. Sub-flow gets its own history (key benefit per ARCHITECTURE.md), retry policy, search attributes.
- **D3-10: `call_flow` retry policy is INHERITED from the parent flow's options by default.** Override via `call_flow(name=..., retry_policy=..., timeout=...)` kwargs. (Note: this differs from Phase 1's per-step retry pattern where steps default to "no retry"; child workflows get their parent's policy as the default to keep transitive semantics intuitive.)
- **D3-11: Search attributes / memos PROPAGATE from parent to child by default.** `call_flow` accepts override kwargs to clear or replace them. This matches Temporal's standard child-workflow ergonomics.
- **D3-12: Cross-flow lambda IDs DO NOT need disambiguation by flow name.** Within a single `.star` file, line+col is unique per lambda regardless of which flow contains it. Across files, `fileBytes` hash differs. The current ID format already handles this; no change needed.

### `for_each_parallel` Concurrency Model

- **D3-13: Default fan-out cap = 10.** Configurable per call via `for_each_parallel(items=..., max_concurrency=N, ...)` kwarg. Conservative default prevents accidentally spawning thousands of goroutines from a lambda that returns a giant list. Implement via a semaphore (buffered channel of size N) inside the interpreter's for_each_parallel handler.
- **D3-14: On non-retryable error in any branch, CANCEL siblings and bubble up the error.** Use `workflow.NewSelector` with a per-branch cancel context derived from the workflow context; first non-retryable failure cancels the parent context, all in-flight branches see `ctx.Err() == context.Canceled`. The for_each_parallel returns the original error. Matches Go errgroup-style semantics; predictable failure mode.
- **D3-15: Item access in lambdas is via `ctx.<item_name>`.** `for_each_parallel(items=..., item="row", steps=[script(fn=lambda ctx: ctx.row.id)])`. The bridge injects the item under `ctx` using the `item` kwarg name. Consistent with rest-of-state-via-`ctx` pattern.
- **D3-16: Iteration contract is "stable index order; results in input order".** Branches spawned in input order with index `0..N-1`. Results collected in same order regardless of completion timing. Replay-deterministic by construction. The bridge ensures the items list itself is deterministic (already enforced by D-19 — list values are frozen at parse).

### Worker Bootstrap & Client Factory

- **D3-17: Three named constructors for Temporal clients.**
  - `skytime.NewCloudClient(opts CloudOptions) (*Client, error)` — Temporal Cloud (API key + TLS implied per v1.39 default change)
  - `skytime.NewSelfHostedClient(opts SelfHostedOptions) (*Client, error)` — self-hosted with mTLS
  - `skytime.NewDevClient(opts DevClientOptions) (*Client, error)` — local dev-server (TLS off)
  Three named constructors are more discoverable in IDE autocomplete than a single `NewClient(ConnectionOptions{...})`. Each surfaces only the options that apply to its variant; the v1.39 TLS-with-API-key default change is handled in `NewCloudClient`'s implementation (one place).
- **D3-18: Worker entry point is `worker.Start` non-blocking + `worker.Stop`.** `w.Start()` returns immediately; `w.Stop()` shuts down. Caller manages lifecycle (signal handling, graceful drain). Mirrors `go.temporal.io/sdk/worker.Worker.Start/Stop`. Consumer's `main.go` example (Phase 6):
  ```go
  w, err := skytime.NewWorker(client, dispatch, skytime.WorkerOptions{
      RootDir: "./flows",
      BuildID: getBuildID(), // e.g., commit SHA
  })
  if err != nil { log.Fatal(err) }
  if err := w.Start(); err != nil { log.Fatal(err) }
  // wait on signal
  <-ctx.Done()
  w.Stop()
  ```
- **D3-19: Default task queue is `"skytime"`, with per-flow and per-step overrides.** Hierarchy: step's `task_queue` > flow's `task_queue` > worker default. Override at parser level via new DSL kwargs:
  - `flow(name=..., task_queue="critical")` — workflow runs on this queue
  - `step(action=..., task_queue="slow_io")` — activity for this step dispatched to this queue
  - **DSL retrofit required:** Phase 1's `flow()` and `step()` builtins do NOT currently accept `task_queue` kwarg. Phase 3 backports these to `pkg/parser/builtins.go` and threads the values onto `dag.Flow.TaskQueue` and `dag.Step.TaskQueue` fields (new fields on existing types).
- **D3-20: BuildID is `worker.Options.BuildID` with a sensible default.** Default = a build-time-injected variable (`var defaultBuildID = "dev"` overridable via `-ldflags "-X github.com/mikelalcon/skytime.defaultBuildID=$(git rev-parse HEAD)"`). Documented prominently in Phase 6 README. Optional but strongly recommended; running without one means workflows can't be drained safely on `.star` updates.

### Cancellation Watchdog (D-22 + D2-12 wiring)

- **D3-21: `workflow.Context.Done()` propagates to `thread.Cancel` for in-flight lambdas.** When the workflow context is cancelled (parent cancelled, timeout, deliberate cancel), the interpreter wakes up any active `bridge.CallLambda` invocation by triggering the thread's cancel hook. Implementation: a watchdog goroutine started per `CallLambda` invocation listens on `<-ctx.Done()` and calls `thread.Cancel("workflow context cancelled")`. This is the only legal interaction between `workflow.Context` and the Starlark thread (per the no-context-bleed invariant).

### Print Hook Wiring (D-21 finalization)

- **D3-22: `print()` inside lambdas routes to `workflow.GetLogger(ctx).Info("[skytime/print] " + msg, "lambda_id", id)`.** Phase 1 set up the `Print` thread hook on `bridge.CallLambda`; Phase 3 provides the logger callback. Calls are visible in Temporal worker logs and survive replay (logger calls are side-effect-free from Temporal's perspective).

### Determinism Requirements

- **D3-23: Map iteration in the interpreter sorts keys before reading.** The bridge already does this for `ToStarlarkStruct` (D-09 / DSL-09). The interpreter must also sort when iterating any internal `map[string]X` it walks during workflow execution. Replay-twice CI test asserts byte-equal command histories.
- **D3-24: Interpreter passes `workflowcheck` analysis with no findings.** No native `go`, no `time.*`, no `rand.*`, no map iteration without sort, no I/O. CI runs `go.temporal.io/sdk/contrib/tools/workflowcheck` against `pkg/interpreter` and fails on any flag.

### Claude's Discretion

- Exact name of the watchdog goroutine helper (e.g., `runLambdaWithCancellation` vs `evaluateLambda`).
- Internal layout of the worker registry (`map[string]map[string]*ParsedFlow` keyed by `flow_name → content_hash`, vs flat `map[FlowVersionKey]*ParsedFlow`).
- Specific concurrency primitive for `for_each_parallel` semaphore (buffered channel vs `golang.org/x/sync/semaphore`).
- Default RetryPolicy when nothing is set (none; Temporal's default is fine).
- Whether `dev-server` helper actually spawns `temporal server start-dev` in-process or just connects to localhost (probably the latter for v1).
- Ordering of `pkg/interpreter` files (one big `workflow.go` vs split by node type).

</decisions>

<scope_implications>
## Scope Implications (Beyond Phase 3 Boundary)

These items are NOT Phase 3 deliverables but are flagged for follow-up:

- **DSL retrofit (Phase 1 backport):** Add `task_queue` kwarg to `flow()` and `step()` builtins; thread fields onto `dag.Flow` and `dag.Step`. Either (a) backport into Phase 1 (retrofit commit on the type spine), or (b) add to Phase 3's plan. Recommend (b) — keep the changes scoped to the phase that needs them.
- **Phase 6 README must document the Build ID deployment workflow.** Without that documentation, customers won't know how to deploy safely.
- **WorkflowInput wire-format change:** Phase 1 currently has `WorkflowInput.Flow *dag.Flow`; Phase 3 changes to `FlowName string` + `ContentHash string` and drops the embedded Flow. Internal-only break (no real consumers yet); a clean rewrite of the struct.

</scope_implications>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project specs
- `.planning/PROJECT.md` — Strict directives; Validated Requirements section now includes Phase 1 + 2 deliverables.
- `.planning/REQUIREMENTS.md` — Phase 3 owns INTRP-01..07, WORK-01..03.
- `.planning/ROADMAP.md` — Phase 3 entry: goal, success criteria.

### Prior phase outputs
- `.planning/phases/01-type-spine-extension-contract-parser-bridge-foundations/01-CONTEXT.md` — D-18 lambda ID format, D-19 free-var lint, D-20 lambda-time globals, D-21/D-22 thread setup.
- `.planning/phases/02-generic-activity-block-batch-dispatch-credentials/02-CONTEXT.md` — D2-01 ActionResult, D2-15/16 timeout & heartbeat semantics, D2-17 OperationDispatch.
- `.planning/phases/02-generic-activity-block-batch-dispatch-credentials/02-VERIFICATION.md` — confirms what's wired in `pkg/activity` for the interpreter to dispatch to.
- `pkg/dag/input.go` — existing `WorkflowInput` skeleton with `TODO(phase3)`. Modify to match D3-04.
- `pkg/dag/flow.go`, `pkg/dag/step.go` — add `TaskQueue string` field per D3-19.
- `pkg/dag/control.go` — `IfCond`, `Script`, `ForEachParallel`, `CallFlow` types; the interpreter walks these.
- `pkg/dag/lambda.go` — `CapturedLambda` and `ComputeLambdaID`.
- `pkg/activity/execute_batch.go` — Phase 3 schedules this activity per Step.
- `pkg/bridge/lambda_call.go` — Phase 3 calls this for every IfCond / Script / ForEachParallel item lambda; the watchdog (D3-21) wraps each call.
- `pkg/bridge/struct.go` — `ToStarlarkStruct` for state injection.

### Project-level research
- `.planning/research/SUMMARY.md` §"Phase 3" — original analysis of lambda serialization options. Note: the original recommendation was Option A (DataConverter); this CONTEXT.md picks Option B per discussion + Build ID versioning.
- `.planning/research/PITFALLS.md` §1 (thread reuse), §3 (lambda non-determinism), §4 (context bleed), §8 (cancellation lifecycle).

### External (Temporal-specific)
- Temporal Go SDK `workflow` package: `workflow.Go`, `workflow.NewSelector`, `workflow.Await`, `workflow.SideEffect`, `workflow.ExecuteActivity`, `workflow.ExecuteChildWorkflow`, `workflow.GetLogger`.
- Temporal docs: child workflow patterns, search attribute propagation, Build ID / Worker Versioning.
- `go.temporal.io/sdk/contrib/tools/workflowcheck` — static analysis the CI runs.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/dag.WorkflowInput` skeleton (rewriting in Phase 3 to match D3-04).
- `pkg/dag` six node types with `Kind() string` + `Position() syntax.Position` — Phase 3 type-switches over these in the interpreter.
- `pkg/activity.Activity.ExecuteBatch` — Phase 3 dispatches Step blocks to this via `workflow.ExecuteActivity`.
- `pkg/bridge.CallLambda` — fresh thread per call, MaxExecutionSteps wired; Phase 3 wraps this with the cancellation watchdog.
- `pkg/bridge.ToStarlarkStruct` — Phase 3 calls this to inject workflow state into `ctx` before each lambda evaluation.
- `pkg/parser.Parser` + `WithRoot` option — worker's startup boot calls this to build the registry.

### Established Patterns
- Sealed interfaces, typed errors with Position(), atomic per-task commits, co-located tests + `tests/fixtures/`.
- Functional options for constructors (Phase 1 + 2 examples).
- AST-walking firewall test (Phase 2): Phase 3's `pkg/interpreter` becomes the second package allowed to import `go.temporal.io/sdk/workflow`. The firewall test must be updated.

### Integration Points
- Phase 4 (static validator + CLI) consumes the worker's flow registry indirectly — `skytime validate` uses the same `Parser` the worker uses, ensuring static and runtime agree.
- Phase 5 (test harness) replaces `pkg/activity.Activity.ExecuteBatch` with a Starlark-mock router; the interpreter is unchanged.
- Phase 6 (example) wires real extensions, demonstrates Build ID + filesystem deployment.

</code_context>

<specifics>
## Specific Ideas

- The `dev-server` flow should be very low-friction. `skytime.NewDevClient(skytime.DevClientOptions{Address: "localhost:7233"})` connects to a `temporal server start-dev` instance the user runs themselves. We don't spawn the dev server in-process for v1.
- Lambda print hook: prefix log messages with `[skytime/print]` for greppability. Include `lambda_id` as a structured field so log aggregation can group by lambda. Don't include the `.star` file path (it's redundant — `lambda_id` encodes file content hash + position).
- For_each_parallel semaphore: a buffered channel of size `max_concurrency` is the simplest approach. Tests should run with `-race` and verify N branches actually run concurrently when `max_concurrency >= N`, and serially when `max_concurrency = 1`.
- Worker registry shape: probably `map[string]map[string]*ParsedFlow` (flow_name → content_hash → parsed). Lookup is `registry[flow_name][content_hash]`. If only one version of a flow_name is present (typical case), the inner map has one entry.

</specifics>

<deferred>
## Deferred Ideas

- **Hot-reload of `.star` files** — registry is frozen at boot. Hot-reload moves to v2; design must not preclude (the parser is a pure function of file contents).
- **`workflow.Patch` / `version()` DSL primitives** — versioning is operational (Build IDs), not authorial. Re-evaluate if a real customer needs in-flow branching.
- **In-process `temporal server start-dev` spawning** — `NewDevClient` connects to an externally-running dev server in v1. Auto-spawning is a future convenience.
- **Per-flow / per-step Build IDs** — Build ID is worker-level only in v1. Per-flow is overkill.
- **Multi-version worker registry without Build IDs** (Option B' from discussion) — explicitly NOT in v1; relies on Build IDs.
- **Custom DataConverter** — explicitly NOT in v1. Could be added in v1.x if a customer needs self-contained workflow inputs (e.g., for off-cluster replay tools).
- **`signal_workflow` / `wait_for_signal` primitives** — not in Phase 3 scope; v1.x per REQUIREMENTS.md `DSL-V2-01`.
- **`on_error` / `on_failure` per-step or per-flow hooks** — v1.x per `DSL-V2-02`.
- **Default Query handler** — auto-generated `getCurrentNode` / `getState` queries; planned for Phase 7 (production hardening) per ARCHITECTURE.md.
- **Continue-As-New strategy** — not in v1; surface when long-running flows exceed Temporal's history-event budget.
- **Backend abstraction (`wf` interface)** — explicitly dropped per D3-08. Add only when a real second backend (Cadence, etc.) materializes.

</deferred>

---

*Phase: 03-lambda-serialization-decision-interpreter-worker*
*Context gathered: 2026-04-29*
