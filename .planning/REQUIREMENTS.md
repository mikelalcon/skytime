# Requirements: Skytime

**Defined:** 2026-04-26
**Core Value:** A consultant team can take an extension catalog and a customer brief, write a `.star` file, and have a production-grade durable workflow running on Temporal — without touching Go and without giving up Temporal's retry/timeout/child-workflow guarantees.

## v1 Requirements

Requirements for initial release. Each maps to roadmap phases.

### DSL — Starlark Authoring Surface

- [x] **DSL-01**: A Starlark file can declare a flow with `flow(name=..., inputs={...}, steps=[...])`, generating a deterministic `dag.Flow` at parse time
- [x] **DSL-02**: A flow can express a sequential step with `step(action=...)` that wraps a single `ActionRef`
- [x] **DSL-03**: A flow can express a sequential block with `step(block=[a, b, c])` that batches multiple `ActionRef`s for a single activity invocation
- [x] **DSL-04**: A flow can branch with `if_cond(cond=lambda ctx: ..., then=[...], else_=[...])` evaluated entirely inside the workflow (zero Temporal history events)
- [x] **DSL-05**: A flow can transform state with `script(id=..., fn=lambda ctx: {...}, output_alias=...)` evaluated entirely inside the workflow (zero Temporal history events)
- [x] **DSL-06**: A flow can fan out with `for_each_parallel(items=..., item=..., steps=[...])` accepting a static list or a lambda producer, with bounded fan-out
- [x] **DSL-07**: A flow can invoke a subflow with `call_flow(name=..., inputs=..., child_options=...)`, isolating its history as a Temporal child workflow
- [x] **DSL-08**: A step accepts Temporal `RetryPolicy` kwargs (initial interval, backoff, max attempts, non-retryable errors) and timeouts (start-to-close, schedule-to-start)
- [x] **DSL-09**: Lambdas access workflow state via dot-notation (`ctx.req.repo_name`) — the bridge recursively converts Go state maps into nested `*starlarkstruct.Struct` instances with deterministic key order
- [x] **DSL-10**: `resolve.AllowLambda = true` is set explicitly before any Starlark parse; `lambda` is the only legal expression-evaluation surface (no CEL, no string parsers)
- [x] **DSL-11**: A flow can dynamically build a single action via `step(action_fn=lambda ctx: ext.op(...))` returning a single `ActionRef`; the lambda evaluates inside the workflow with the locked predeclared globals (D-20) and cancellation watchdog (D3-21)
- [x] **DSL-12**: A flow can dynamically build a batch via `step(block_fn=lambda ctx: [ext.op(...) for ...])` returning a Starlark list of `ActionRef`; empty results short-circuit (no activity dispatch); mixed-idempotency batches reject at parse time (best-effort, D4.1-11) or at runtime (defense-in-depth, D4.1-12)
- [x] **DSL-13**: String kwargs accept `${ctx.expr}` interpolation that desugars at parse time into `lambda ctx: "..." + str(ctx.expr) + "..."`; honors `$$` escape, single-line only, empty `${}` is a parse error; the synthesized lambda flows through the existing D4-02 ctx.<name> walker so typos surface as `*dag.ValidationError` at parse time
- [x] **DSL-14**: A flow can use `if_cond` as a value-producing expression by supplying `output_alias="X"`; both branches must end in `result(value={...})` or `fail(...)`; the bound value lands at `ctx.X` post-branch and is type-checked at parse time. Procedural-mode (no `output_alias`) is preserved verbatim
- [x] **DSL-15**: Two new top-level parse-time builtins: `result(value={...})` (kwarg-only, dict-literal value, string-literal keys, per-key expression captured as synthesized lambda) and `fail("msg")` (top-level node-emit; same name as lambda-time `fail` global, different predeclared environment; supports `${ctx.expr}` interpolation in messages via Message/MessageFn pattern)

### Extension SDK

- [x] **EXT-01**: An `Extension` Go interface exposes `Name()`, `Initialize(thread, kwargs)`, and `Operations() map[string]OperationFunc`
- [x] **EXT-02**: Extension factory methods (Starlark-callable) return `ActionRef` intents — they never register Temporal activities
- [x] **EXT-03**: Operation functions take `context.Context` (stdlib), never `workflow.Context`; they may not import `go.temporal.io/sdk/activity`
- [x] **EXT-04**: Each extension declares per-operation `Idempotent bool` so the activity can decide whether to batch the action
- [x] **EXT-05**: A `Credential` typed value (with redacted `String()`) is the only legal way for an extension to receive a secret — workflow state stores only the credential's string ID
- [x] **EXT-06**: Extensions are registered statically (Go imports) or dynamically (runtime registry calls) before parsing — no plugin / gRPC / out-of-process loading in v1

### Parser & Bridge

- [x] **PARSE-01**: The parser injects core DSL primitives (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`) as naked `*starlark.Builtin` globals (not namespaced)
- [x] **PARSE-02**: The parser supports `load()` for splitting flows across multiple `.star` files; load resolution is sandboxed to a configured root directory
- [x] **PARSE-03**: The parser separates *parse-time globals* (richer: registry lookups, load) from *lambda-time globals* (restricted: arithmetic, comparison, struct access, frozen-collection iteration only — no time, no random, no I/O)
- [x] **PARSE-04**: The parser captures `*starlark.Function` lambdas keyed by stable IDs and stores them on `dag` nodes with each node's `syntax.Position` for error attribution
- [x] **PARSE-05**: Parsing a `.star` file with no extensions registered or with malformed primitives produces a position-aware error (`<file>:<line>:<col>: <message>`) and never panics
- [x] **PARSE-06**: The bridge's `CallLambda` always uses a fresh `*starlark.Thread` per invocation, sets `MaxExecutionSteps`, wires `thread.Cancel` to `workflow.Context.Done()`, and routes `Print` to the workflow logger

### Temporal Interpreter

- [x] **INTRP-01**: A single generic Temporal workflow (`SkytimeWorkflow(ctx, WorkflowInput)`) walks any parsed `dag.Flow` and produces final state
- [x] **INTRP-02**: A documented decision (custom `DataConverter` vs. re-parse-on-start with `LambdaID` keys) governs how lambdas survive Temporal serialization; the chosen mechanism passes a replay-twice equality test
- [ ] **INTRP-03**: `if_cond` and `script` evaluate lambdas inline and produce zero Temporal history events
- [ ] **INTRP-04**: `for_each_parallel` spawns concurrent branches via `workflow.Go` + `workflow.Selector` with a configurable bounded fan-out (default documented), and uses `workflow.Await` to barrier on completion
- [ ] **INTRP-05**: `call_flow` invokes the same generic workflow as a `workflow.ExecuteChildWorkflow`, isolating sub-flow history
- [x] **INTRP-06**: Map iteration in the interpreter (and in the bridge's struct conversion) sorts keys before reading — verified by a CI replay test that runs every E2E test twice and asserts byte-equal command histories
- [x] **INTRP-07**: The interpreter passes `workflowcheck` analysis (no native `go`, no time/random calls, no map iteration without sort)

### Generic Activity & Credentials

- [x] **ACT-01**: A single Temporal activity `ExecuteBatch(ctx, []ActionRef) ([]ActionResult, error)` dispatches all extension I/O — extensions never register their own activities
- [x] **ACT-02**: The activity returns a structured per-action result list (`ok-with-output | retryable_err | non_retryable_err | skipped`); the interpreter consumes per-action results, not a single batch outcome
- [x] **ACT-03**: Non-idempotent operations are never batched — they execute as one action per activity invocation, even when the user wrote them in a `block`
- [x] **ACT-04**: A `CredentialResolver` interface is injected into the activity environment; secrets are resolved just-in-time inside the activity, never stored in workflow state
- [x] **ACT-05**: Credentials use a `Secret` wrapper type whose `String()`, `GoString()`, and `MarshalJSON()` always return `"<redacted>"`. Operations extract the raw value via an explicit `.Reveal()` call. An integration test injects a known fake-secret as a `Secret`, fails an op mid-batch, and asserts the secret string never appears in any returned `ActionResult`, error wrapper, or activity heartbeat payload — relying entirely on type-level protection (no regex scrubber in v1; deferred). Op authors are responsible for wrapping or filing bugs against third-party libraries that leak revealed secrets into their error paths. *(Amended 2026-04-27 per Phase 2 discussion — original required regex scrubber; replaced with type-level protection.)*
- [x] **ACT-06**: The activity heartbeats between actions and uses a per-batch `StartToCloseTimeout` equal to the sum of per-action timeouts plus headroom

### Worker & Temporal Compatibility

- [x] **WORK-01**: A worker bootstrap function registers `SkytimeWorkflow` and `ExecuteBatch` with one Temporal worker
- [x] **WORK-02**: One client factory handles three Temporal connection variants — Temporal Cloud (API key + TLS), self-hosted with mTLS, and local dev-server (TLS off) — surfacing the v1.39 TLS-with-API-key default change in exactly one place
- [x] **WORK-03**: A consumer Go service can embed Skytime as a library: `import` packages, register extensions, call `worker.Run(client, flowDir)` — no service binary or framework required

### Static Validation (Tier 1)

- [x] **VAL-01**: `skytime validate <file.star>` parses and checks every flow without executing — verifies kwargs match each extension's declared schema, every input maps to a registered schema, every lambda's free variables reference declared state, and the lambda predeclared-global subset is honored
- [x] **VAL-02**: Static validation and runtime parsing share the same parser code path — a CI corpus test runs every `.star` file in `examples/` through both static `validate` and a dry-run interpreter (all actions mocked) and asserts they agree on accept/reject
- [x] **VAL-03**: Validation errors are formatted `<file>:<line>:<col> [flow > step > action]: <message>` and exit non-zero; `--debug` reveals Go internals only
- [x] **VAL-04**: `block_fn` lambdas pass best-effort static idempotency analysis at parse time via `pkg/parser/block_fn_lint.go::classifyBlockFn`; opaque shapes (helper functions, indirect dispatch) defer to the runtime fallback in `pkg/activity/validate_batch.go`
- [x] **VAL-05**: Strict-equality branch validation across `if_cond` expression-mode branches with permissive type inference: parser walks each result-value AST expression, infers TypeInfo (sealed sum: Scalar/Dict/List/Tuple/Opaque); typed-typed mismatches reject (no LUB; explicit casts widen); typed-Opaque or Opaque-Opaque defer. State schema widens from name-set to typed map; visibility-only D4-02 walker behavior preserved

### E2E Test Harness (Tier 3)

- [x] **TEST-01**: A `temporal_test` Starlark builtin module exposes `tester.workflow(...)`, `tester.mock_action(extension=..., op=..., mock_fn=...)`, and `tester.run(flow=...)` from `.star` test files
- [x] **TEST-02**: A Starlark mock function executes in the *same* restricted predeclared environment as production lambdas; the bridge intercepts the corresponding `ExecuteBatch` activity in `testsuite.TestWorkflowEnvironment` and routes per-action calls back to the Starlark mock
- [x] **TEST-03**: The `attempt` count is passed to mocks as an explicit argument so `.star` tests can simulate transient failures and assert Temporal's retry behavior without leaving Starlark
- [x] **TEST-04**: A replay helper runs each test twice and diffs the resulting Temporal event history; any divergence fails the test
- [x] **TEST-05**: The `assert.*` builtins from `go.starlark.net/starlarktest` are available inside test `.star` files; the harness reports failures into Go's `*testing.T` so they're CI-visible

### CLI

- [x] **CLI-01**: `skytime validate <file.star>` runs static validation (Tier 1) and exits with structured errors
- [x] **CLI-02**: `skytime run <file.star> --flow=<name> --input=<json>` parses, validates, and triggers a workflow on a configured Temporal cluster, then streams progress
- [x] **CLI-03**: `skytime test <dir>` discovers `.star` test files, runs the Tier 3 harness, and reports pass/fail with Starlark callsite errors
- [x] **CLI-04**: `skytime dev-server` spawns a local Temporal dev server (Temporalite or `temporal server start-dev`) for local development of the example project
- [x] **CLI-05**: The CLI lives under `cmd/skytime/`; cobra and charmbracelet/log are CLI-only dependencies — they are not reachable from the library root
- [x] **CLI-06**: `skytime run` renders progress as a multi-line live block on TTY + non-verbose; spinner cadence 100 ms; max 10 active rows + `... and M more` truncation; cursor-up + line-clear ANSI preserves scrollback
- [x] **CLI-07**: The renderer falls back to static line-per-event mode when stdout is non-TTY OR `--verbose` is set OR the build is Windows

### Example Project (Dogfood + Demo)

- [x] **EX-01**: `examples/http-github-webhook/` ships three real extensions — generic HTTP (already shipped in Phase 4 as `pkg/extension/builtin/http`), GitHub, and Webhook (POST JSON to any URL; demonstrated against webhook.site for zero-setup non-idempotency proof) — each declaring `Idempotent bool` per operation. GitHub + Webhook live under `examples/http-github-webhook/extensions/{github,webhook}/` and are registered alongside HTTP in the example's custom CLI binary at `examples/http-github-webhook/cmd/extbin/main.go`. Slack was originally named here but swapped to Webhook during Phase 6 discuss-phase to remove account-creation friction; Webhook covers the same non-idempotency-demo intent and is broader-purpose (consultants point it at Discord/Slack/Teams webhooks too).
- [x] **EX-02**: The example contains four to six `.star` flows that collectively exercise every DSL primitive (sequential, block batch, `if_cond`, `script`, `for_each_parallel`, `call_flow`) and every concern (retries, timeouts, credentials, cancellation)
- [x] **EX-03**: The example contains at least one `.star` test file using `temporal_test` that exercises retries via `attempt` and asserts replay determinism
- [x] **EX-04**: A README walkthrough takes a reader from `git clone` to a successfully-executed flow against `skytime dev-server` in under five commands
- [x] **EX-FIX-01**: `examples/skeleton/simple_check.star` and `examples/skeleton/parallel_fanout.star` demonstrate `--input`-driven dynamic kwargs end-to-end via `${ctx.expr}` interpolation and `step(action_fn=...)` / `step(block_fn=...)` (D4.1-23 + D4.1-24); differential corpus (D4.1-25) pins parse + dryrun agreement

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### Authoring Ergonomics

- **DSL-V2-01**: `wait_for_signal` primitive for human-in-the-loop and external-trigger flows
- **DSL-V2-02**: `on_error` / `on_failure` per-step and per-flow hooks
- **DSL-V2-03**: Default Query handler auto-generated by the interpreter (`getCurrentNode`, `getState`)
- **DSL-V2-04**: Saga / compensation sugar (declarative reverse-DAG)
- **DSL-V2-05**: Schema export — generate JSON Schema or markdown docs from extension declarations

### Testing

- **TEST-V2-01**: Tier 2 — pure-Starlark unit testing of named `def` blocks via `starlarktest.assert`, fully offline (no `testsuite`)
- **TEST-V2-02**: Time-skipping helpers in `temporal_test` so tests can advance virtual time

### Operations

- **OPS-V2-01**: Hot-reload of `.star` files (parser is already a pure function — door open, but full hot-reload requires worker versioning + content-addressed flows)
- **OPS-V2-02**: Search-attribute helpers (declarative tagging on flows for visibility in Temporal UI)
- **OPS-V2-03**: Better error diagnostics — pretty-printed multi-line context, suggestion engine for typos in extension/operation names
- **OPS-V2-04**: Documented payload codec configuration shipped as a reusable `pkg/codec` for production credential-payload encryption

### Tooling

- **TOOL-V2-01**: Starpls-based LSP for `.star` flows (autocomplete, jump-to-definition for extensions)
- **TOOL-V2-02**: Buildifier-based formatter recommendation in example README

## Out of Scope

Explicitly excluded for v1 (and likely beyond). Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| String-expression languages (CEL, JSONPath, Jinja) | Re-introduces parse-time evaluation surface that lambdas eliminate; explicitly forbidden by spec |
| Web UI / dashboard | Temporal's UI is sufficient; building one duplicates effort and creates a hosting product surface |
| Multi-tenant hosted SaaS | Skytime is a library, not a service; productizing as SaaS is a separate decision |
| Cron / scheduling primitives | Use Temporal Schedules directly; baking them into the DSL duplicates Temporal's surface |
| Asset / lineage tracking | Wrong domain — that's Dagster's territory |
| Built-in human-task UI | Signals + extension-published callbacks are sufficient; UI is a downstream product |
| `eval` / dynamic flow construction at execute time | Breaks parse/execute separation, the project's whole reason to exist |
| Cross-flow shared state | Use signals or a customer-owned datastore; sharing state across workflows breaks isolation |
| Plugin / gRPC / out-of-process extensions | Not justified before a real customer asks; static or dynamic-local Go extensions only in v1 |
| `go.temporal.io/sdk/activity` imports inside extensions | Forbidden by spec; couples extensions to Temporal, breaks single-generic-activity batching, breaks plain-Go extension testing |
| Native `go` keyword in workflow code | Non-deterministic — `workflowcheck` flags it; only `workflow.Go` is legal |
| Skytime-specific workflow versioning helpers | Temporal patching primitives exist; the side-by-side-flows posture is enforced by content-addressing flows |

## Traceability

Which phases cover which requirements. Populated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| DSL-01 | Phase 1 | Complete |
| DSL-02 | Phase 1 | Complete |
| DSL-03 | Phase 1 | Complete |
| DSL-04 | Phase 1 | Complete |
| DSL-05 | Phase 1 | Complete |
| DSL-06 | Phase 1 | Complete |
| DSL-07 | Phase 1 | Complete |
| DSL-08 | Phase 1 | Complete |
| DSL-09 | Phase 1 | Complete |
| DSL-10 | Phase 1 | Complete |
| EXT-01 | Phase 1 | Complete |
| EXT-02 | Phase 1 | Complete |
| EXT-03 | Phase 1 | Complete |
| EXT-04 | Phase 1 | Complete |
| EXT-05 | Phase 1 | Complete |
| EXT-06 | Phase 1 | Complete |
| PARSE-01 | Phase 1 | Complete |
| PARSE-02 | Phase 1 | Complete |
| PARSE-03 | Phase 1 | Complete |
| PARSE-04 | Phase 1 | Complete |
| PARSE-05 | Phase 1 | Complete |
| PARSE-06 | Phase 1 | Complete |
| ACT-01 | Phase 2 | Complete |
| ACT-02 | Phase 2 | Complete |
| ACT-03 | Phase 2 | Complete |
| ACT-04 | Phase 2 | Complete |
| ACT-05 | Phase 2 | Complete |
| ACT-06 | Phase 2 | Complete |
| INTRP-01 | Phase 3 | Complete |
| INTRP-02 | Phase 3 | Complete |
| INTRP-03 | Phase 3 | Pending |
| INTRP-04 | Phase 3 | Pending |
| INTRP-05 | Phase 3 | Pending |
| INTRP-06 | Phase 3 | Complete |
| INTRP-07 | Phase 3 | Complete |
| WORK-01 | Phase 3 | Complete |
| WORK-02 | Phase 3 | Complete |
| WORK-03 | Phase 3 | Complete |
| VAL-01 | Phase 4 | Complete |
| VAL-02 | Phase 4 | Complete |
| VAL-03 | Phase 4 | Complete |
| CLI-01 | Phase 4 | Complete |
| CLI-02 | Phase 4 | Complete |
| CLI-03 | Phase 5 | Complete |
| CLI-04 | Phase 4 | Complete |
| CLI-05 | Phase 4 | Complete |
| TEST-01 | Phase 5 | Complete |
| TEST-02 | Phase 5 | Complete |
| TEST-03 | Phase 5 | Complete |
| TEST-04 | Phase 5 | Complete |
| TEST-05 | Phase 5 | Complete |
| EX-01 | Phase 6 | Complete |
| EX-02 | Phase 6 | Complete |
| EX-03 | Phase 6 | Complete |
| EX-04 | Phase 6 | Complete |
| DSL-11 | Phase 04.1 | Complete |
| DSL-12 | Phase 04.1 | Complete |
| DSL-13 | Phase 04.1 | Complete |
| VAL-04 | Phase 04.1 | Complete |
| CLI-06 | Phase 04.1 | Complete |
| CLI-07 | Phase 04.1 | Complete |
| EX-FIX-01 | Phase 04.1 | Complete |
| DSL-14 | Phase 04.2 | Complete |
| DSL-15 | Phase 04.2 | Complete |
| VAL-05 | Phase 04.2 | Complete |

**Coverage:**
- v1 requirements: 65 total (55 original + 7 added in Phase 04.1: DSL-11/12/13, VAL-04, CLI-06/07, EX-FIX-01 + 3 added in Phase 04.2: DSL-14/15, VAL-05)
- Mapped to phases: 65 ✓
- Unmapped: 0

**Phase summary:**
- Phase 1 (Type Spine + Extension Contract + Parser/Bridge Foundations): 22 requirements (DSL-01..10, EXT-01..06, PARSE-01..06)
- Phase 2 (Generic Activity + Block-Batch Dispatch + Credentials): 6 requirements (ACT-01..06)
- Phase 3 (Lambda-Serialization Decision + Interpreter + Worker): 10 requirements (INTRP-01..07, WORK-01..03)
- Phase 4 (Static Validation Tier + CLI Skeleton): 7 requirements (VAL-01..03, CLI-01, CLI-02, CLI-04, CLI-05)
- Phase 04.1 (Dynamic step kwargs + interpolation + live progress block): 7 requirements (DSL-11/12/13, VAL-04, CLI-06/07, EX-FIX-01)
- Phase 04.2 (if_cond as expression with strict-equality result binding): 3 requirements (DSL-14, DSL-15, VAL-05)
- Phase 5 (Tier-3 E2E Test Harness): 6 requirements (TEST-01..05, CLI-03)
- Phase 6 (Example Project): 4 requirements (EX-01..04)

---
*Requirements defined: 2026-04-26*
*Last updated: 2026-05-04 — Phase 04.2 added DSL-14, DSL-15, VAL-05 (3 new requirements; v1 total 62→65)*
