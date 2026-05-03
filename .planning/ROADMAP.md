# Roadmap: Skytime

## Overview

Skytime delivers a Go library that lets consultant teams declare durable Temporal workflows in Starlark. The journey is risk-front-loaded: lock the type spine and the parser/bridge contract before anything depends on it, build the generic activity in isolation so its partial-failure protocol can be tested standalone, then resolve the lambda-serialization decision and stand up the interpreter against a real Temporal dev server. After the production execution path works end-to-end, layer the static validator and CLI on top of the same parser, then build the Tier-3 test harness that mocks the single generic activity, then dogfood everything through the HTTP + GitHub + Slack example project. Six phases, 55 v1 requirements, no UI.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 1: Type Spine + Extension Contract + Parser/Bridge Foundations** - Pure data types, extension SDK, Starlark parser with all six primitives, and the state/lambda bridge — no Temporal yet (completed 2026-04-27)
- [ ] **Phase 2: Generic Activity + Block-Batch Dispatch + Credentials** - Single `ExecuteBatch` activity with per-action result list, JIT credential resolution, and error-scrubbing middleware
- [ ] **Phase 3: Lambda-Serialization Decision + Interpreter + Worker** - Resolve how lambdas survive Temporal serialization, then build the generic interpreter workflow and the multi-cluster worker bootstrap
- [ ] **Phase 4: Static Validation Tier + CLI Skeleton** - `skytime validate` / `run` / `dev-server`, sharing the parser with the runtime via differential corpus testing
- [ ] **Phase 5: Tier-3 E2E Test Harness (`temporal_test`)** - Starlark-native E2E tests with `attempt`-aware mocks, replay-determinism assertion, and `skytime test`
- [ ] **Phase 6: Example Project (HTTP + GitHub + Slack)** - Three real extensions, four-to-six `.star` flows exercising every primitive, README walkthrough — the proof-of-life and the demo

## Phase Details

### Phase 1: Type Spine + Extension Contract + Parser/Bridge Foundations
**Goal**: Lock the data types that everything else depends on (`pkg/dag`, `pkg/extension`), implement the Starlark parser with all six DSL primitives, and stand up the state/lambda bridge so a `.star` file can be parsed into an inspectable `dag.Flow` with no Temporal involvement.
**Depends on**: Nothing (first phase)
**Requirements**: DSL-01, DSL-02, DSL-03, DSL-04, DSL-05, DSL-06, DSL-07, DSL-08, DSL-09, DSL-10, EXT-01, EXT-02, EXT-03, EXT-04, EXT-05, EXT-06, PARSE-01, PARSE-02, PARSE-03, PARSE-04, PARSE-05, PARSE-06
**Success Criteria** (what must be TRUE):
  1. A consultant can write a `.star` file using all six primitives (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`) and produce a complete `dag.Flow` whose nodes can be inspected in a Go unit test, with every node carrying a `syntax.Position`
  2. A library developer can implement a working `Extension` (with `Name()`, `Initialize`, `Operations()` returning `OperationFunc`s that take `context.Context` and declare `Idempotent bool`), register it statically or dynamically, and have its factory return an `ActionRef` intent from Starlark — with no path that lets the extension import `go.temporal.io/sdk/activity` or register a Temporal activity
  3. A malformed `.star` file (missing kwargs, unknown extension, broken `load()`) produces a position-aware error of the form `<file>:<line>:<col>: <message>` and never panics
  4. The bridge converts a nested Go state map to `*starlarkstruct.Struct` with deterministic key order, evaluates a captured lambda using `ctx.req.repo_name`-style dot access on a fresh `*starlark.Thread` per call (with `MaxExecutionSteps` set), and returns a frozen result — verified by an iteration-determinism unit test that converts the same map twice and asserts byte-equal Starlark dicts
  5. The parser uses two distinct predeclared environments — a richer parse-time environment (with `load`, registry lookups) and a strict lambda-time subset (no `time`, no `random`, no I/O) — and `resolve.AllowLambda = true` is set explicitly before any parse
**Plans**: 5 plans
  - [x] 01-01-PLAN.md — Wave 0: Toolchain + module skeleton + typed errors (`pkg/dag/errors.go`) + 16 .star fixtures + 2 golden JSON
  - [x] 01-02-PLAN.md — Wave 1: `pkg/dag` type spine — Node interface + 6 node types (Flow, Step, IfCond, Script, ForEachParallel, CallFlow) + ActionRef (starlark.Value with recursive Freeze) + CapturedLambda + RetryPolicy/Timeout (Unpacker)
  - [x] 01-03-PLAN.md — Wave 1: `pkg/extension` SDK contract — Extension interface + OperationSpec (Idempotent *bool) + sealed Credential interface (3 kinds, redacted String) + CredentialHandler + per-parser Registry + ~150 LOC reflection-based kwarg validator
  - [x] 01-04-PLAN.md — Wave 2: `pkg/bridge` — ToStarlarkStruct (deterministic key order) + lambdaTimeGlobals (D-20 locked subset, 20 keys) + CallLambda (fresh thread, MaxExecutionSteps, Print hook)
  - [x] 01-05-PLAN.md — Wave 3: `pkg/parser` — six DSL primitive builtins + load() with .git-ancestor root + traversal rejection + lambda capture (D-18 IDs) + free-var validation (D-19) + multi-flow + cross-flow resolution + fixture corpus tests (8 valid + 8 invalid)
**UI hint**: no

### Phase 2: Generic Activity + Block-Batch Dispatch + Credentials
**Goal**: Build the single generic Temporal activity (`pkg/activity`) that dispatches all extension I/O, batches idempotent actions, returns a structured per-action result list, resolves credentials just-in-time, and scrubs token-shaped strings out of every error before they reach Temporal — testable standalone with hand-built `[]ActionRef` inputs.
**Depends on**: Phase 1
**Requirements**: ACT-01, ACT-02, ACT-03, ACT-04, ACT-05, ACT-06
**Success Criteria** (what must be TRUE):
  1. A test harness can call `ExecuteBatch(ctx, []ActionRef)` with a hand-built batch against stub extensions and a fake credential resolver, and receive a `[]ActionResult` where each entry is one of `{ok-with-output, retryable_err, non_retryable_err, skipped}` — never a single batch outcome
  2. A batch containing a non-idempotent action runs that action one-per-activity-invocation even when the user wrote it inside a `block`, while batches of idempotent actions execute sequentially in a single activity invocation with a heartbeat between every action
  3. A test that injects a known fake-secret as a `Credential`, fails the action mid-batch, and inspects every returned error confirms the secret string never appears in any error or result payload (error-scrubbing middleware verified)
  4. The activity's `StartToCloseTimeout` is computed as the sum of per-action timeouts plus headroom, and a long batch heartbeats often enough that an artificially-low `HeartbeatTimeout` does not cause spurious failures in tests
  5. Workflow state passed into the activity contains only credential string IDs; the resolver is invoked inside the activity and the resolved `Credential` value never escapes the activity's stack frame
**Plans**: 3 plans
  - [x] 02-01-PLAN.md — Wave 0: Type spine (`pkg/dag/result.go` ActionResult sealed sum + `pkg/dag/output.go` OperationOutput marker) + `pkg/extension/secret.go` Secret wrapper (full format coverage incl. `Format` for %+v) + Credential field type narrowing + OperationFunc return type narrow + OperationSpec.DefaultTimeout + ErrUnknownCredential sentinel + `pkg/extension/testing` sub-package (FakeCredentialHandler) + parser linter passes (mixed-idempotency reject D2-05 + block-size cap D2-07) + WithMaxBlockSize option + go.temporal.io/sdk@v1.42.0 dep + 2 invalid fixtures
  - [x] 02-02-PLAN.md — Wave 1: `pkg/activity` foundations — package skeleton + OperationDispatch type alias + per-worker credential cache (sync.RWMutex + lazy TTL + race-clean) + retry-aware bypass seam + heartbeat emitter (BatchProgress JSON) + attemptFunc injection seam + classifyResolveError (D2-12) + Activity struct + functional options + firewall test (only pkg/activity may import go.temporal.io/sdk/*)
  - [x] 02-03-PLAN.md — Wave 2: `ExecuteBatch` integration — `validate_batch.go` (defense in depth) + `action_executor.go` (per-action timeout + cred resolve + classify) + `execute_batch.go` (D2-13 retryable short-circuit + D2-14 full-list non-retryable + D2-16 heartbeat + cancellation cooperation) + 12 integration tests via testsuite.TestActivityEnvironment incl. ACT-05 secret-leak test (3 leak channels)
**UI hint**: no

### Phase 3: Lambda-Serialization Decision + Interpreter + Worker
**Goal**: Decide and implement how `*starlark.Function` lambdas survive Temporal's serialization boundary (custom `DataConverter` vs. re-parse-on-start with `LambdaID` keys), then build the generic interpreter workflow that walks any `dag.Flow`, plus the worker bootstrap that handles Temporal Cloud / self-hosted-mTLS / local dev-server connection variants.
**Depends on**: Phase 2
**Requirements**: INTRP-01, INTRP-02, INTRP-03, INTRP-04, INTRP-05, INTRP-06, INTRP-07, WORK-01, WORK-02, WORK-03
**Success Criteria** (what must be TRUE):
  1. A documented decision record commits to one lambda-serialization mechanism (custom `DataConverter` or re-parse-on-start) **before any interpreter code is written**, and that mechanism passes a replay-twice equality test where the same `dag.Flow` is run twice and produces byte-equal Temporal command histories
  2. A hand-built `dag.Flow` containing every primitive runs end-to-end on a Temporal dev server: `if_cond` and `script` evaluate lambdas inline producing zero Temporal history events; `for_each_parallel` fans out via `workflow.Go` + `workflow.Selector` with a configurable bounded fan-out and a documented default; `call_flow` invokes the same generic workflow as `workflow.ExecuteChildWorkflow`
  3. The interpreter passes `go.temporal.io/sdk/contrib/tools/workflowcheck` analysis with no findings — no native `go`, no `time.*` or `rand.*`, no map iteration without sort
  4. Cancelling the workflow mid-lambda terminates the lambda within a bounded time: a watchdog wires `workflow.Context.Done()` to `thread.Cancel`, and `MaxExecutionSteps` is set on every interpreter-side `starlark.Call` — verified by a cancellation test that completes within N seconds
  5. A consumer Go service can `import` Skytime, register one extension, and call `worker.Run(client, flowDir)` with a single client factory choosing among Temporal Cloud (API key + TLS), self-hosted mTLS, or local dev-server (TLS off) — surfacing the v1.39 TLS-with-API-key default change in exactly one place
**Plans**: 4 plans
  - [x] 03-01-PLAN.md — Wave 0: DSL retrofit (task_queue kwarg) + WorkflowInput rewrite (D3-04) + Phase 2 firewall expansion to allowlist pkg/interpreter + pkg/worker
  - [x] 03-02-PLAN.md — Wave 1: pkg/interpreter foundations — package skeleton + FlowRegistry + cancellation watchdog (D3-21) + SkytimeWorkflow skeleton with walker stubs
  - [x] 03-03-PLAN.md — Wave 2: All five node walkers (Step / IfCond / Script / ForEachParallel / CallFlow) + lambda eval helper + replay-twice integration test (kitchen sink)
  - [x] 03-04-PLAN.md — Wave 3: pkg/worker bootstrap + three named client constructors (D3-17) + Build ID (D3-20) + library-embed integration test (WORK-03)
**UI hint**: no

### Phase 4: Static Validation Tier + CLI Skeleton
**Goal**: Build the static validator (`pkg/parser` + post-parse pass) and the CLI tree under `cmd/skytime/` so `skytime validate`, `skytime run`, and `skytime dev-server` work against the runtime parser, with a CI corpus differential test proving static and runtime agree on accept/reject for every `.star` file under `examples/`.
**Depends on**: Phase 3
**Requirements**: VAL-01, VAL-02, VAL-03, CLI-01, CLI-02, CLI-04, CLI-05
**Success Criteria** (what must be TRUE):
  1. `skytime validate <file.star>` parses every flow without executing, verifies kwargs match each extension's declared schema, every input maps to a registered schema, every lambda's free variables reference declared state, and the lambda predeclared-global subset is honored — exiting non-zero on any failure with errors formatted `<file>:<line>:<col> [flow > step > action]: <message>`
  2. The static validator and the runtime parser share the same parser code path: a CI corpus test runs every `.star` file in `examples/` through both static `validate` and a dry-run interpreter (all actions mocked) and asserts they agree on accept/reject — drift fails CI
  3. `skytime run <file.star> --flow=<name> --input=<json>` parses, validates, and triggers a workflow on a configured Temporal cluster, then streams progress to the terminal
  4. `skytime dev-server` spawns a local Temporal dev server (Temporalite or `temporal server start-dev`) suitable for the example project — `cobra` and `charmbracelet/log` are reachable only from `cmd/skytime/`, never from the library root
  5. The `--debug` flag is the only path that reveals Go internals in error output; default error rendering is Starlark-first
**Plans**: 7 plans
  - [x] 04-01-PLAN.md — Wave 0: Add cobra/charmlog deps + AST firewall extension + ValidationError.Action field + Parser.FileBytes() accessor + empty pkg/cli + pkg/validator + pkg/extension/builtin/http skeletons
  - [x] 04-02-PLAN.md — Wave 1: D4-02 ctx.<name> AST visitor (re-parse via syntax.FileOptions.Parse) + state-schema accumulator + finalize wiring + D-11 kwarg cross-validate
  - [x] 04-03-PLAN.md — Wave 2: pkg/validator thin facade + AlwaysOkDispatch (pkg/validator/internal/dryrun) + TestDifferentialCorpus (skip-on-empty until W4 lands corpus)
  - [x] 04-04-PLAN.md — Wave 3: pkg/cli root command + persistent flags + env-var binding + Starlark-first renderer + charm-log slog handler + skytime validate subcommand
  - [x] 04-05-PLAN.md — Wave 4: skytime run subcommand (embedded transient worker) + connectClient variant routing + progressHandler slog shim + temporal-firewall allow-list extension for pkg/cli
  - [x] 04-06-PLAN.md — Wave 4: skytime dev-server subcommand (subprocess wrapper around  with SIGINT forwarding and missing-binary install instructions)
  - [x] 04-07-PLAN.md — Wave 4: HTTP extension implementation (5 ops, D4-14 idempotence) + cmd/skytime binary + examples/skeleton/ corpus + docs/cli-binary.md + differential test wiring
**UI hint**: no

### Phase 04.1: Dynamic step kwargs — lambda-accepting step(action_fn=...) variant for runtime-built action kwargs (INSERTED)

**Goal:** [Urgent work - to be planned]
**Requirements**: TBD
**Depends on:** Phase 4
**Plans:** 7/8 plans executed

Plans:
- [ ] TBD (run /gsd:plan-phase 04.1 to break down)

### Phase 5: Tier-3 E2E Test Harness (`temporal_test`)
**Goal**: Build `pkg/testing` so consultants can write E2E tests in `.star` files: `tester.workflow`, `tester.mock_action`, and `tester.run` mock the single generic activity inside `testsuite.TestWorkflowEnvironment`, route per-action calls back to Starlark mock lambdas evaluating in the same restricted predeclared environment as production lambdas, and a replay helper runs each test twice and diffs Temporal event histories. `skytime test <dir>` is the discovery and runner entry point.
**Depends on**: Phase 3 (the production execution path must exist before mocks can mirror it)
**Requirements**: TEST-01, TEST-02, TEST-03, TEST-04, TEST-05, CLI-03
**Success Criteria** (what must be TRUE):
  1. A `.star` test file using `tester.workflow(...)`, `tester.mock_action(extension=..., op=..., mock_fn=...)`, and `tester.run(flow=...)` runs end-to-end inside `testsuite.TestWorkflowEnvironment`; the bridge intercepts the corresponding `ExecuteBatch` activity and routes each per-action call back to the matching Starlark mock lambda
  2. A mock lambda receives the `attempt` count as an explicit argument so a `.star` test can simulate transient failures (succeed on third try) and assert Temporal's retry behavior — without leaving Starlark and without using closure-mutated state
  3. The replay helper runs each test twice and any divergence in Temporal event history fails the test with a Starlark-callsite-aware error message
  4. The `assert.*` builtins from `go.starlark.net/starlarktest` are available inside test `.star` files and assertion failures surface in Go's `*testing.T` so they appear in CI output
  5. `skytime test <dir>` discovers `.star` test files, runs the harness, and reports pass/fail with Starlark callsite errors — no Go stack traces in default output
**Plans**: TBD
**UI hint**: no

### Phase 6: Example Project (HTTP + GitHub + Slack)
**Goal**: Ship `examples/http-github-slack/` as the dogfooding vehicle and proof-of-life: three real extensions (HTTP, GitHub, Slack) with per-operation `Idempotent bool`, four-to-six `.star` flows exercising every primitive and every concern, at least one `.star` test using `temporal_test`, and a README walkthrough that takes a reader from `git clone` to a successfully-executed flow against `skytime dev-server` in under five commands.
**Depends on**: Phase 5
**Requirements**: EX-01, EX-02, EX-03, EX-04
**Success Criteria** (what must be TRUE):
  1. A reader can `git clone`, follow the README walkthrough, and execute a `.star` flow against `skytime dev-server` in under five commands — verified by a CI smoke test that exercises the documented commands
  2. The example contains four-to-six `.star` flows that collectively exercise every DSL primitive (sequential `step`, `block` batch, `if_cond`, `script`, `for_each_parallel`, `call_flow`) and every concern (retries, timeouts, credentials, cancellation) — coverage demonstrated by a primitive-coverage matrix in the README
  3. Each of the three extensions (HTTP, GitHub, Slack) declares `Idempotent bool` per operation, with Slack `chat.postMessage` correctly declared non-idempotent and verified to execute one-action-per-activity-invocation even when the author wrote it inside a `block`
  4. At least one `.star` test file uses `temporal_test` to exercise retries via `attempt` and asserts replay determinism — and any awkward Starlark ergonomics surfaced by writing this example are fed back as fixes to the parser or builtin set before v1 is declared done
**Plans**: TBD
**UI hint**: no

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5 → 6

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Type Spine + Extension Contract + Parser/Bridge Foundations | 5/5 | Complete   | 2026-04-27 |
| 2. Generic Activity + Block-Batch Dispatch + Credentials | 2/3 | In Progress|  |
| 3. Lambda-Serialization Decision + Interpreter + Worker | 3/4 | In Progress|  |
| 4. Static Validation Tier + CLI Skeleton | 0/7 | Not started | - |
| 5. Tier-3 E2E Test Harness (`temporal_test`) | 0/TBD | Not started | - |
| 6. Example Project (HTTP + GitHub + Slack) | 0/TBD | Not started | - |

---
*Roadmap created: 2026-04-26*
*Coverage: 55/55 v1 requirements mapped*
