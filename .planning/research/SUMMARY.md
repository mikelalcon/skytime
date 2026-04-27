# Project Research Summary

**Project:** Skytime
**Domain:** Go library — embedded Starlark DSL compiled to a deterministic DAG and executed inside Temporal workflows; companion CLI + example project for dogfooding
**Researched:** 2026-04-27
**Confidence:** HIGH on the architectural pillars (stack versions, Temporal semantics, Starlark freeze rules, anti-features); MEDIUM on the seam where all three systems share state — Skytime is novel territory and at least one foundational decision (lambda serialization across Temporal history) has no published precedent.

## Executive Summary

Skytime is a two-phase system whose phase boundary is the entire reason it exists. **Parse phase** (pure, off-Temporal) takes a `.star` file plus a Go-side extension registry and emits a `dag.Flow` plus a map of captured `*starlark.Function` lambdas. **Execute phase** (durable, deterministic, inside Temporal) walks that DAG with a generic interpreter workflow, evaluating lambdas in fresh `*starlark.Thread`s on injected state structs and dispatching extension I/O through one block-batched generic activity. Extensions are plain Go functions returning `ActionRef` intents; they never import `go.temporal.io/sdk/activity`, never see a `*starlark.Thread`. Lambdas never see `workflow.Context`. This three-way firewall is the safety guarantee that justifies the project over raw Temporal.

The recommended approach, anchored across the four research files, is a **type-spine-first build**: define the `dag` types and the `extension` contract, then `parser` + `bridge`, then the activity, then the interpreter, then the test bridge, then the CLI/example. The stack is fixed and verified — Go 1.25 (forced by `go.starlark.net`), `go.temporal.io/sdk` v1.42.0, `go.starlark.net` at the latest pseudo-version, with `cobra`/`charmbracelet/log` confined to the CLI tree so the library root stays light. The five differentiators worth committing to in v1 are the two-tier authoring model, lambdas-not-strings, the ActionRef Command Pattern, the `temporal_test` builtin bridging Temporal `testsuite` mocks to Starlark, and sub-second static validation. Everything else (signals, `on_error` hooks, sagas, LSP) is post-v1.

The biggest risks cluster at the three-system seam and must be surfaced now because they reshape phase 1: **(1)** `*starlark.Function` is not JSON-serializable and the workflow input must cross Temporal's history boundary, forcing an early decision between a custom `DataConverter` versus re-parsing on workflow start; **(2)** lambda determinism is invisible to `workflowcheck` and requires a strict two-environment split (parse-time globals vs. lambda-time globals) plus a static validator that walks lambda free vars; **(3)** credentials can leak through error messages, log strings, and Temporal event history despite the "IDs in state" rule, requiring a `Credential` type with redacted `String()` and an error-scrubbing middleware on every activity exit; **(4)** block-batched activities have ambiguous partial-failure semantics, requiring an `Idempotent bool` declaration on every extension and a per-action result list rather than a single batch return value. Each of these has a phase ownership recorded below.

## Key Findings

### Recommended Stack

The architectural triple is fixed. The supporting cast is selected for greenfield 2026 Go ergonomics, with everything CLI-specific (cobra, charm/log) confined to `cmd/skytime/` so the library root has only the three required dependencies. Go 1.25 is the floor (forced by `go.starlark.net`); declare `go 1.25` in `go.mod` (not 1.26, which would cut off the still-supported previous Go release).

**Core technologies:**
- **Go 1.25.x** (toolchain `go1.26.2` available): implementation language; `go 1.25` directive in `go.mod` to honor the Starlark module's floor without cutting off 1.25 consumers.
- **`go.starlark.net@latest`** (pseudo-version, currently `v0.0.0-20260326113308-fadfc96def35`; no tagged releases ever): canonical Google reference Starlark interpreter — DSL parsing, lambda capture, lambda evaluation. Ships `starlarkstruct` (required for `ctx.req.repo_name` dot-access) and `starlarktest` (used by Tier-3 testing).
- **`go.temporal.io/sdk@v1.42.0`** (released 2026-04-08): Temporal client + workflow + activity APIs. v1.42 raised the Go floor to 1.24; v1.39 changed TLS behavior so providing an API key implies TLS — be explicit about `TLSDisabled` only for local dev-server.
- **`go.temporal.io/sdk/testsuite`** (bundled): `TestWorkflowEnvironment` / `TestActivityEnvironment` for the Tier-3 E2E test harness; the `temporal_test` Starlark builtin bridges this back to Starlark mock lambdas.
- **`go.temporal.io/sdk/workflow`** (bundled): `workflow.Go`, `workflow.Await`, `workflow.NewSelector`, child workflow primitives — the only legal way to spawn concurrency inside the interpreter (native `go` is forbidden by determinism).
- **`log/slog`** (stdlib): structured logging *interface* for the library; do not take a hard dep on a backend. Library accepts `*slog.Logger` (or `slog.Default()`); CLI wires `charmbracelet/log/v2@v2.0.0` as the slog handler for pretty terminal output.
- **`spf13/cobra@v1.10.2`** (CLI only): command tree for `skytime run / parse / test / dev-server`; PreRun chain is the reason for choosing it over `urfave/cli`. Stays out of the library root.
- **`stretchr/testify@v1.11.1`** (test only): `require`/`assert` for table-driven tests. Skip `testify/mock` — Temporal's testsuite mocking is sufficient.
- **`golangci-lint@v2.11.4`**: enable `govet`, `staticcheck`, `errcheck`, `revive`, `unused`, `gocritic`, plus **`forbidigo`** to ban `panic` in workflow paths and (ideally) ban `*starlark.Thread` and `workflow.Context` from package combinations that should never see them.

**What NOT to use** (non-obvious, all explicitly rejected): CEL or any string-expression language (re-introduces the parse-time evaluation surface that lambdas eliminate); `go.temporal.io/sdk/activity` imports inside extensions (couples extensions to Temporal, breaks single-generic-activity batching, breaks plain-`go-test` extension testing); native `go` keyword anywhere in workflow code (non-deterministic — `workflowcheck` flags this); `gomock`/`mockery` (competes with `testsuite`); `viper` for v1 (heavy deps; use `koanf/v2@v2.3.4` if config grows beyond flags); the `v1` major of `charmbracelet/log` (predates slog stabilization).

### Expected Features

The feature landscape is tight: Skytime competes against raw Temporal Go (too low-level for non-Go authors), Cadence Starlark Worker (single-tier; treats `.star` as the workflow body), and Zigflow (YAML DSL with the same string-expression trap CEL has). Skytime's wedge is the two-tier authoring model + ActionRef Command Pattern + lambdas-not-strings + `temporal_test` bridge — these four together are the v1 pitch.

**Must have (table stakes — without these the project doesn't justify itself):**
- Starlark DSL primitives: `flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow` (sub-workflow).
- Per-step retries (Temporal `RetryPolicy` exposed as Starlark kwargs) and per-step timeouts.
- Cancellation propagation (inherit from Temporal; verify with explicit cancel test).
- Single generic Temporal activity with **block-batched I/O** (history-size discipline; the differentiator vs. naive one-activity-per-step DSLs).
- ActionRef Command Pattern + extension registry with typed kwargs.
- Just-in-time credential resolver (state holds IDs, secrets resolved inside activity).
- Static validation tier (parse + extension-schema/kwarg checks; sub-second feedback).
- Tier-3 E2E testing via `temporal_test` builtin (Starlark-native tests, Temporal `testsuite` underneath, `attempt` count for retry simulation).
- Dev CLI: `skytime run`, `skytime validate`, `skytime test`, `skytime dev-server` (Temporalite wrapper).
- Example project (HTTP + GitHub + Slack extensions) exercising every primitive.
- Temporal Cloud and self-hosted compatibility (one client factory, three connection variants).

**Should have (competitive differentiators — commit to first five for v1):**
1. **Two-tier authoring model** (Go extension devs + Starlark consultants) — the headline.
2. **Lambdas instead of string expressions** — sidesteps the "every simple language eventually becomes Turing-complete" trap.
3. **ActionRef Command Pattern + single generic activity** — enables batching, single-point mocking, JIT credential resolution.
4. **`temporal_test` Starlark-native E2E bridge** — no comparable tool exists; strong demo material.
5. **Fast static validation** with extension-schema awareness (beats raw Temporal's runtime-only errors and YAML DSLs' structural-only validation).
6. Credential-ID-only state (already standard practice but rarely enforced by the framework — Skytime makes it impossible to do wrong).
7. Implicit anti-versioning posture: ship new flow files, run side-by-side, drain old (no Skytime-specific patching API).

**Defer (v1.x — add when first customer hits the gap):**
- Signals primitive (`wait_for_signal`); `on_error`/`on_failure` hooks; default Query handler; Saga/compensation sugar; search-attribute helpers; schema export to JSON Schema/markdown; better diagnostic line/col error messages.

**Defer (v2+):**
- Tier-2 pure-Starlark unit tests (PROJECT.md already defers this); hot-reload of `.star` files (parser is already a pure function — door open, but full hot-reload requires worker versioning + content-addressed flows); LSP/IDE plugin (starpls-based); flow visualization; gRPC out-of-process plugins; Saga with reverse-DAG generation; Skytime-specific versioning helpers; multi-tenant SaaS.

**Anti-features to defend against scope creep:** string expressions/CEL/JSONPath; web UI/dashboard (Temporal's UI is sufficient); native cron primitives (use Temporal Schedules); asset/lineage tracking (wrong domain — that's Dagster); built-in human-task UI (signals + extension-published callbacks); `eval`/dynamic flow construction (breaks parse/execute separation, the project's whole reason to exist); cross-flow shared state (use signals).

### Architecture Approach

The architecture is a strict pipeline with a hard firewall between three concerns: **(a)** Starlark execution and DAG production live in `parser` + `bridge`; **(b)** Temporal coroutine management and DAG walking live in `interpreter`; **(c)** plain-Go I/O lives in `activity` + `extension`. ActionRef is the wire format from (a) to (b/c); the bridge is the only code that ever touches a `*starlark.Thread` during execute phase, and it constructs a fresh thread for every lambda call. Imports flow strictly downward — `interpreter` imports `dag`, never the reverse — and the firewall is enforced with package boundaries: `activity` does not import `starlark`; `extension` does not import `temporal`.

**Major components (Go packages):**
1. **`pkg/dag`** — pure data: `Node` interface, `Flow`, `Step`, `IfCond`, `ForEach`, `CallFlow`, `Script` node types; `ActionRef{Kind, Kwargs, CredentialID}`; `CapturedLambda{Fn *starlark.Function, Globals starlark.StringDict}`; `WorkflowInput`. Every node carries a `syntax.Position` for error attribution. May not import `starlark` or `temporal`.
2. **`pkg/extension`** — extension SDK: `Extension` interface, `ActionRef` constructor, `CredentialResolver` interface, `Credential` type with redacted `String()`. Extensions declare `Idempotent bool` for batching eligibility. Extensions take `context.Context` (stdlib), never `workflow.Context`.
3. **`pkg/parser`** — Starlark execution at parse time; registers builtins (`flow`, `step`, `if_cond`, `for_each_parallel`, `call_flow`, `script`); handles `load()` resolution; runs static validation (unknown extensions, kwarg schema check, lambda free-variable purity, position-aware errors). Outputs `dag.Flow` + lambda map. May not import `temporal`.
4. **`pkg/bridge`** — state ↔ Starlark conversion: `ToStarlarkStruct(any) *starlarkstruct.Struct` (recursive, sorts map keys deterministically), `FromStarlarkValue`, `CallLambda(thread, fn, ctx)` with a fresh `*starlark.Thread` per invocation, `MaxExecutionSteps` set, `Cancel` wired to `workflow.Context.Done()` via watchdog, `Print` routed to `workflow.GetLogger`.
5. **`pkg/interpreter`** — the generic Temporal workflow `SkytimeWorkflow(ctx, WorkflowInput)`; walks the DAG; collects ActionRefs into batches per Step; dispatches one `workflow.ExecuteActivity("ExecuteBatch", []ActionRef)`; handles `for_each_parallel` via `workflow.Go` + selectors with bounded fan-out; handles `call_flow` via `workflow.ExecuteChildWorkflow(SkytimeWorkflow, sub_input)`; uses bridge for every lambda eval, never a long-lived thread. Designed against an internal `wf` interface (`ExecuteActivity`, `Go`, `Now`, `SideEffect`, `ExecuteChildWorkflow`) so a Cadence backend remains possible — out of scope for v1 but free to design in.
6. **`pkg/activity`** — single generic activity `ExecuteBatch(ctx, []ActionRef) ([]ActionResult, error)`; per-action structured result (`success-with-output | retryable-failure | non-retryable-failure | skipped`); JIT credential resolution; per-batch heartbeats between actions; credential-scrubbing error wrapper as last line of defense before returning to Temporal. Does not import `starlark`.
7. **`pkg/testing`** — Tier-3 harness: `temporal_test` Starlark builtin, `attempt` counter, mock registry mapping `ActionRef.Kind` → Starlark mock function; mocks execute in the same restricted predeclared environment as production lambdas; replay-test helper that runs each E2E test twice and diffs event histories.
8. **`pkg/worker`** — Temporal worker bootstrap; one client factory handling Cloud / self-hosted-mTLS / dev-server variants; registers `SkytimeWorkflow` + `ExecuteBatch`.
9. **`cmd/skytime`** + **`examples/http-github-slack/`** — CLI commands (run/validate/test/dev-server) and dogfooding example with HTTP + GitHub + Slack extensions exercising every primitive.

**Cross-cutting architectural patterns** (each tied to a key risk):
- **Command Pattern via ActionRef** (intent objects): extensions return intents, never execute. Parse phase has zero side effects.
- **Captured-Lambda Late Binding**: `*starlark.Function` stored in DAG keyed by stable ID; bridge invokes with fresh thread + injected struct on each call.
- **Block-Batched Activity Dispatch**: one `ExecuteActivity` per Step (capped at ~50 actions), per-action result list, idempotency-aware (non-idempotent extensions skip batching automatically).
- **JIT Credential Resolution**: workflow state holds only IDs; resolver runs inside activity; `Credential` type prevents accidental log/error leakage.
- **Backend-Pluggable Workflow API** (defensive): interpreter talks to a 5-method `wf` interface; only Temporal impl exists in v1; door is open if Cadence support ever validates.

### Critical Pitfalls

The pitfalls research is opinionated about the three-system seam. The phase mapping (next section) makes ownership concrete.

1. **`*starlark.Function` cannot cross Temporal's serialization boundary** (HIGH severity, HIGH likelihood — pitfalls #1 and the architecture's named Risk #1). The lambda map in `WorkflowInput` is the immediate landmine. **This is the hard decision that blocks Phase 1**: pick (a) custom `DataConverter` that serializes lambdas as source location + closure environment + lookup key, with the parsed `*starlark.Program` available on every replayer, or (b) re-parse on workflow start (`workflow.SideEffect`-wrapped first call) keeping only an opaque `LambdaID` in history, coupling deployment to the `.star` file being available to every worker. Option (a) is recommended; (b) is the simpler fallback. Option (c) "compile lambdas to a serializable IR" is rejected — it re-introduces a string-compilation surface and explicitly violates a spec invariant. **Decision must be made and recorded before activity batching is implemented.**

2. **Lambda non-determinism is invisible to `workflowcheck`** (HIGH severity, MEDIUM likelihood — pitfalls #3, plus #2 mutable captures and #6 freeze audit). The Go SDK's analyzer sees the interpreter calling `starlark.Call` and treats it as a black box. Mitigation requires a **two-environment split**: parse-time globals can be richer (`load`, registry lookups); lambda-time globals are a strict subset (arithmetic, string ops, struct access, frozen-collection iteration only) — no `time.now`, no `random()`, no I/O, no `os.Environ`. The static validator walks every lambda's `NumFreeVars()`/`Freevar(i)` and rejects mutable captures. Every custom `starlark.Value` must implement recursive `Freeze()`. Map iteration in the bridge sorts keys before constructing Starlark dicts. CI runs every E2E test twice and asserts byte-equal command histories.

3. **Context bleed across the three-system firewall** (HIGH severity for the architecture's safety claims — pitfalls #1, #4 and named Risk #3). Two leak directions both forbidden: (i) `workflow.Context` reachable from a `*starlark.Thread` (via `SetLocal` or struct embedding) — would let a clever Starlark builtin call `workflow.ExecuteActivity` directly, defeating ActionRef batching and single-point mocking; (ii) `*starlark.Thread` or `*starlark.Function` reachable from an activity payload — not serializable, silently corrupts on replay. Mitigation: `bridge.CallLambda` uses fresh thread per call; `SetLocal` allowlist is workflow run ID, attempt number, and a derived `<-chan struct{}` cancellation shim — never `workflow.Context`. Extension functions take `context.Context` (stdlib); the generic activity is the only code that adapts `workflow.Context → context.Context`. Lint that no `*starlark.Thread` field appears in any DAG type or activity payload struct.

4. **Block-batch partial-failure semantics + credential leakage at the activity boundary** (HIGH severity, HIGH likelihood — pitfalls #5 and #6). Two failures that compound: (i) batched action B fails, A already succeeded externally, Temporal retries the whole batch → A runs twice; (ii) the failure error string contains a Bearer token → goes into Temporal event history forever. Mitigations are paired: every extension declares `Idempotent bool`; non-idempotent actions are not batched (one-action-per-activity); the activity returns a structured `[]ActionResult` with per-action status; idempotent batches retry whole on retryable failure (extensions must be safe to re-run). Credentials use a `Credential` type with redacted `String()`; an error-scrubbing middleware wraps every activity exit (regex strips token-shaped strings); the example project ships with a Temporal payload codec configured. Per-batch `StartToCloseTimeout` is the sum of per-action timeouts plus headroom; heartbeat between every action.

5. **Cross-language error reporting + static/runtime divergence** (MEDIUM severity, HIGH likelihood — pitfalls #7 and #10). If a workflow failure surfaces a Go stack trace pointing at `interpreter.go:142`, the "consultants don't write Go" promise is broken. Mitigation: every DAG node carries `syntax.Position` from parse; every error wrap at the eval boundary captures `EvalError.Backtrace()`; activities propagate structured failure via `temporal.NewApplicationError` with Starlark callsite details; user-facing errors are formatted `<file.star>:<line>:<col> ('flow_name' > step 'X' > action B): <message>`; `--debug` reveals Go internals. Static validator and runtime parse share **the same parser** — validation is a post-parse pass over the DAG, never a second AST traversal. Differential corpus test in CI: every `.star` in a corpus runs through both static validation and a dry-run interpreter (all actions mocked to zero values); they must agree on accept/reject.

## Implications for Roadmap

Build order is dictated by package dependencies (see ARCHITECTURE.md §6) and reinforced by the pitfall-to-phase mapping (see PITFALLS.md §"Pitfall-to-Phase Mapping"). The hard decision in pitfall #1 (lambda serialization) must happen at the **start of Phase 3**, not during Phase 5 when it would force a rewrite.

### Phase 1: Type Spine + Extension Contract + Parser/Bridge Foundations

**Rationale:** `dag` is imported by every other package; `extension` defines the contract for parser builtins and activity dispatch; parser + bridge can be tested in isolation without any Temporal involvement. Doing all of this together avoids ratcheting changes into types that already have callers. Lambda capture is parser logic and lambda invocation is bridge logic — keeping them in the same phase prevents a co-evolution hazard.

**Delivers:** `pkg/dag` (Node, Flow, Step, IfCond, ForEachParallel, CallFlow, Script, ActionRef, CapturedLambda, WorkflowInput, all carrying `syntax.Position`); `pkg/extension` (Extension interface, ActionRef constructor, CredentialResolver interface, `Credential` type with redacted `String()`, `Idempotent bool` declaration); `pkg/parser` (Starlark execution, all six builtins, `load()` resolver, two-environment-split predeclared globals); `pkg/bridge` (`ToStarlarkStruct` recursive with sorted keys, `CallLambda` with fresh thread, `MaxExecutionSteps` default, deterministic-iteration helpers).

**Addresses (FEATURES.md):** Starlark DSL primitives; extension registry with typed kwargs; lays groundwork for ActionRef Command Pattern, JIT credential resolver, static validation.

**Avoids (PITFALLS.md):** #1 thread reuse (DAG types contain no thread refs), #2 mutable capture (free-var lint scaffolded), #6 freeze audit (every custom value type implements recursive `Freeze()` from day one), #7 error reporting (`syntax.Position` on every node).

**Validation gate:** A unit test parses a `.star` file with every primitive and inspects a complete `dag.Flow` — no Temporal involvement yet.

### Phase 2: Generic Activity + Block-Batch Dispatch + Credential Resolution

**Rationale:** The activity is independent of the interpreter — it can be tested standalone with hand-built `[]ActionRef` inputs. Doing it before the interpreter prevents interpreter risk from contaminating activity testing. The credential and idempotency contracts crystallize here, and the partial-failure protocol (per-action structured result list) becomes the API the interpreter will consume.

**Delivers:** `pkg/activity` (single generic `ExecuteBatch` activity, dispatch routing on `ActionRef.Kind`, JIT credential resolution via injected resolver, per-action result list with `{ok|retryable_err|non_retryable_err|skipped}` shape, heartbeat between actions, error-scrubbing middleware that runs before every error returns to Temporal, batch-size cap of ~50 actions).

**Uses (STACK.md):** `go.temporal.io/sdk/activity` (the *only* package allowed to import this); the Temporal sum-of-timeouts pattern.

**Implements (ARCHITECTURE.md):** Pattern 3 (Block-Batched Activity Dispatch) and Pattern 4 (JIT Credential Resolution).

**Avoids (PITFALLS.md):** #5 partial failure (idempotency contract + per-action result list), #6 credential leakage (Credential type + scrubbing middleware), #4 context bleed (extensions take `context.Context` not `workflow.Context`).

**Validation gate:** Activity runs against a hand-built `[]ActionRef` with a stub extension and a fake credential resolver; an integration test injects a known fake-secret, fails the action, and asserts the secret string never appears in any returned error.

### Phase 3: Interpreter + Lambda Serialization Decision

**Rationale:** This is the high-risk milestone. **The first task in this phase is to resolve the lambda serialization decision (custom `DataConverter` vs. re-parse-on-start) and commit a decision record before any interpreter code is written.** Determinism rules (#3), `workflow.Context` lifecycle (#8), and the fresh-thread pattern (#1) all crystallize here.

**Delivers:** `pkg/interpreter` (the generic `SkytimeWorkflow`, DAG walker, batch collection per Step, `for_each_parallel` via `workflow.Go` with bounded fan-out and `workflow.Selector`, `call_flow` via `workflow.ExecuteChildWorkflow(SkytimeWorkflow, …)`, lambda eval via bridge with fresh thread, cancellation watchdog wiring `workflow.Context.Done()` to `thread.Cancel`); `pkg/worker` (Temporal worker bootstrap, client factory for Cloud/self-hosted/dev-server); chosen lambda-serialization solution implemented end-to-end.

**Uses (STACK.md):** `go.temporal.io/sdk/workflow`, `workflow.Go`, `workflow.Selector`, `workflow.ExecuteChildWorkflow`; if option (a), a custom `converter.DataConverter`; if option (b), `workflow.SideEffect` for parse-on-start.

**Implements (ARCHITECTURE.md):** Pattern 1 (ActionRef indirection complete), Pattern 2 (Captured-Lambda Late Binding), Pattern 5 (internal `wf` interface for backend-pluggability).

**Avoids (PITFALLS.md):** #1 thread reuse (fresh thread per `CallLambda`), #3 lambda non-determinism (restricted lambda predeclared environment), #4 context bleed (`SetLocal` allowlist enforced), #8 lifecycle mismatch (cancellation watchdog, `MaxExecutionSteps` default).

**Validation gate:** A hand-built `dag.Flow` runs end-to-end on a Temporal dev server with a stub extension. `workflowcheck` passes. Replay test (run twice, diff event histories) passes.

### Phase 4: Static Validation Tier + CLI Skeleton

**Rationale:** Static validation must share the parser (pitfall #10 single source of truth). The CLI is the entry point that exercises validation in real workflows for consultants. Doing them together ensures the validator's UX (error format, `.star:line:col`) is end-to-end testable from `skytime validate`.

**Delivers:** Static validator as a post-parse pass over the DAG (kwargs vs. extension schema, lambda free-var purity, predeclared-global-restriction check, position-aware errors); `cmd/skytime` with `validate`, `run`, and `dev-server` subcommands; differential corpus test in CI (every `.star` in corpus must agree between static and a dry-run interpreter).

**Uses (STACK.md):** `spf13/cobra@v1.10.2`, `charmbracelet/log/v2@v2.0.0` as slog handler in CLI only, optional `temporalio/temporalite` wrapper for `dev-server`.

**Avoids (PITFALLS.md):** #2 mutable capture (free-var lint live), #7 error reporting (Starlark-first error format used everywhere), #10 static/runtime drift (shared parser + corpus test).

### Phase 5: Tier-3 E2E Test Harness (`temporal_test`)

**Rationale:** Mocking is dramatically simpler when there's one place to intercept — the single generic activity. The harness must mirror production lambda semantics (same restricted predeclared environment, `attempt` as parameter not closure state, replay-twice determinism assertion) or it lies. The harness needs the production path working before mocking it, so it lands after Phase 3.

**Delivers:** `pkg/testing` (`temporal_test` Starlark builtin bridging `testsuite.OnActivity(...)` to Starlark mock lambdas, `attempt` counter as explicit parameter, mock registry by `ActionRef.Kind`, replay-test helper that runs each E2E test twice and diffs event histories); `skytime test` CLI command.

**Uses (STACK.md):** `go.temporal.io/sdk/testsuite`, `go.starlark.net/starlarktest` for `assert.*` builtins, `stretchr/testify@v1.11.1`.

**Avoids (PITFALLS.md):** #9 mock non-determinism (mocks run in same restricted env as production lambdas; cross-attempt state via `attempt` param + dict, not closure mutation); #3 hidden non-determinism (replay-twice assertion catches both production and mock drift).

### Phase 6: Example Project + Dogfooding (HTTP + GitHub + Slack)

**Rationale:** Dogfooding surfaces the issues that unit tests miss: which primitives feel awkward in real consultant code, which extensions need richer schemas, where error messages fall flat. PROJECT.md mandates this as a v1 requirement, and it is the demo. **Validation gate:** the example exercises every primitive (retries, credentials, parallel for-each, child workflow, signals stub, cancellation); if any feels awkward in Starlark, fix the parser or builtin set before declaring v1 done.

**Delivers:** `examples/http-github-slack/` with three real extensions, four to six `.star` flows covering every primitive, `main.go` wiring the worker, an `.env.example` for credentials, an example payload-codec configuration ("for production, configure this") template, README walkthrough.

**Uses everything from prior phases:** the example is the proof-of-life and the documentation simultaneously.

### Phase 7: Production Hardening + Observability

**Rationale:** Things that matter for the first real customer but don't gate v1 demo: payload codec recommendation, scrubber as documented middleware, Temporal Cloud / self-hosted README guidance, search-attribute helpers, default Query handler auto-generation, structured search-attribute stamping (`skytime/<git-sha>` Identity, flow-name memo). The default Query handler is small enough to slip into this phase rather than create v1.x churn.

**Delivers:** Production deployment guide; codec server example; default `getCurrentNode`/`getState` Query handler auto-generated by the interpreter; structured search attributes; `Identity` stamping; SSRF/resource-exhaustion notes for `MaxExecutionSteps` defaults.

**Avoids (PITFALLS.md):** #6 credential leakage (codec + scrubber both belt-and-suspenders), security mistakes section in pitfalls (audit checklist).

### Phase Ordering Rationale

- **Dependency order** (architecture §6): `dag` → `extension` → (`parser` + `bridge`) → `activity` → `interpreter` → `worker` → `testing` → `cmd` → `examples`. Phases 1–6 above respect this strictly.
- **Risk-front-loading**: the lambda serialization decision (Risk #1) is the single most likely thing to force a rewrite. It is owned at the start of Phase 3, not later, because deferring it would require re-doing `WorkflowInput` plumbing, the bridge, and possibly the parser's lambda-capture format.
- **Activity-before-interpreter inversion**: activity is upstream of interpreter in the dependency graph (interpreter calls `workflow.ExecuteActivity("ExecuteBatch", …)`), so Phase 2 lands first to keep activity tests un-contaminated by interpreter risk.
- **Static validation lands after interpreter** (Phase 4) rather than alongside parser (Phase 1) because the validator must agree with the runtime — co-developing with a working runtime is the only way to enforce that. The parser groundwork (`syntax.Position` on every node, free-var inspection helpers) lands in Phase 1; the validator tier on top lands in Phase 4.
- **Tier-3 testing lands after interpreter** (Phase 5) because mocks must mirror the production execution path; mocking before the production path exists is how mocks lie.
- **Example project lands late** (Phase 6) because it's the integration test for everything. Earlier dogfooding (in Phase 3 or 4 as a sanity check with a single extension) is fine but the comprehensive example with all three extensions belongs at the end of feature work.
- **Pitfall coverage**: every critical pitfall from PITFALLS.md is owned by at least one phase (see "Avoids" lines above); the four cross-cutting risks called out in the executive summary all surface in Phases 1–3 where they can still be designed away.

### Research Flags

**Phases likely needing deeper `/gsd:research-phase` during planning:**
- **Phase 3 (Interpreter + Lambda Serialization):** The custom `DataConverter` path (Risk #1 option a) needs a focused research spike on `*starlark.Program` reconstruction across worker restarts — there is no published precedent for this. Cadence/Temporal Starlark Worker effectively uses option b (script files baked into worker images); confirming whether option a is feasible or whether to default to option b is a phase-blocking decision. Sub-questions: how to identify a captured `*starlark.Function` by stable ID across re-parses; what to do about closures over module-frozen values; how to handle worker-fleet `.star`-file synchronization if option b.
- **Phase 5 (`temporal_test` E2E Harness):** No comparable system exists. The bridge between `testsuite.OnActivity(...)` mock dispatch and a Starlark mock lambda evaluating in the same restricted environment as production lambdas is novel. Needs a design spike — probably a small prototype before committing to the API shape.

**Phases with established patterns (skip research-phase):**
- **Phase 1 (Type Spine + Parser/Bridge Foundations):** `go.starlark.net` API patterns are documented; `starlarkstruct` recursive conversion has a known idiom; AST walking is straightforward.
- **Phase 2 (Generic Activity):** Temporal activity patterns are well-documented; sum-of-timeouts and heartbeat patterns are textbook.
- **Phase 4 (Static Validation + CLI):** cobra patterns are textbook; the validator is a post-parse pass.
- **Phase 6 (Example Project):** dogfooding follows what the prior phases built.
- **Phase 7 (Production Hardening):** Temporal codec server pattern is documented; structured search attributes are textbook.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All versions verified via `proxy.golang.org` `@latest` endpoints and module `go.mod` files (Go 1.25 floor confirmed in `go.starlark.net/go.mod`; SDK v1.42 confirmed via release notes 2026-04-08; cobra/testify/koanf/charm-log all verified to 2025–2026 releases). Two MEDIUM-confidence stylistic calls (koanf vs viper; cobra vs urfave/cli vs kong) — either choice works. |
| Features | HIGH | Anchored to PROJECT.md's already-strong feature list, validated against Uber's Cadence Starlark Worker (closest prior art, 2024) and Zigflow (closest contemporary, 2026). Anti-features are airtight — every "out of scope" item in PROJECT.md is independently corroborated by community pain points. The five-differentiator focus for v1 is opinionated synthesis but well-supported. |
| Architecture | MEDIUM | HIGH on component boundaries, data flow, package layout (validated against `cadence-workflow/starlark-worker` and `temporalio/samples-go/dsl`); MEDIUM on the exact Starlark-lambda-inside-`workflow.Context` behavior — no public prior art evaluates `*starlark.Function` from inside a Temporal coroutine. Risk #1 (lambda serialization) is *the* unresolved architectural question and the reason this section is not HIGH. |
| Pitfalls | HIGH | Temporal determinism / replay rules (`workflowcheck`, replay tests, side effects) sourced from official docs; Starlark freeze and closure semantics from the canonical spec. The seam-specific pitfalls (1, 4, 5, 6, 7, 9) are extrapolated synthesis but each has a clear preventive mechanism that maps to a specific phase. |

**Overall confidence:** HIGH — sufficient to begin Phase 1. The Risk #1 lambda serialization decision is deliberately deferred to the start of Phase 3 (not later); a one-week design spike at that boundary should resolve it.

### Gaps to Address

- **Lambda serialization across Temporal history** (Risk #1, Pitfall #1): The single biggest unresolved question. Owned by Phase 3 with a mandatory decision record before interpreter code is written. Default position if the spike is inconclusive: option (b) re-parse on workflow start with `workflow.SideEffect`, `LambdaID` keys in history, file-content-hash cache. Document the deployment coupling (`.star` files must be available to every worker) explicitly.
- **`temporal_test` API shape** (Phase 5): The mock-bridge ergonomics for `attempt`, retries, and signals across the Temporal `testsuite` boundary into Starlark have no precedent. A small prototype before committing the API is warranted; the API is exposed to `.star` authors and will be hard to change later.
- **Idempotency declaration mechanism** (Phase 2 / Phase 4 boundary): How extension authors declare `Idempotent bool` (Go struct tag? interface method? builder option?) needs validation against the example project's HTTP/GitHub/Slack extensions — Slack `chat.postMessage` is the textbook non-idempotent action and the contract must be ergonomic enough that authors actually use it.
- **Predeclared lambda environment scope** (Phase 1 / Phase 3 boundary): The exact subset of Starlark builtins available *inside lambdas at workflow execution time* (vs. the richer parse-time environment) needs an enumerated list. Initial guess from the research: `len`, `dict`, `list`, `str`, `int`, `float` (off by default per Starlark spec — keep off), comparison ops, struct attribute access, frozen-collection iteration. Anything that touches time, randomness, network, file system, environment, or unbounded computation is forbidden. Lock this list in Phase 1 and freeze it.
- **Backend abstraction depth** (Phase 3, defensive): The internal `wf` interface is a free hedge if it stays at 5 methods. If it starts ballooning to leak Temporal specifics, drop the abstraction and accept Temporal coupling.
- **Bounded `for_each_parallel` fan-out default** (Phase 3): What's the default semaphore size? 10? 100? Document and let `.star` authors override per-flow.

## Sources

### Primary (HIGH confidence)
- pkg.go.dev: go.temporal.io/sdk — current SDK version, workflow/activity/testsuite APIs
- GitHub: temporalio/sdk-go releases — v1.39 → v1.42 release notes (Go-floor bump, TLS-with-API-key default change)
- pkg.go.dev: go.starlark.net, /starlarkstruct, /starlarktest — module structure, `*starlark.Function`, `*starlark.Thread`, `MaxExecutionSteps`, `Cancel`
- GitHub: google/starlark-go go.mod — Go 1.25 floor confirmed
- Starlark in Go: Implementation — freeze mechanism, thread safety
- Starlark in Go: Language definition — determinism, closure capture, freeze of free variables
- Temporal docs: Workflow Definition, Versioning, Side Effects, Multithreading, Selectors, Testing Suite, Workflow Execution limits, Blob size limit, Continue-as-new, Payload codec, Child Workflows, Handling Signals/Queries/Updates
- Temporal Blog: Spooky Stories anti-patterns, Activity timeouts, Idempotency and durable execution, How many Activities should I use
- pkg.go.dev: go.temporal.io/sdk/contrib/tools/workflowcheck — static analysis for workflow non-determinism
- GitHub: cadence-workflow/starlark-worker — closest prior art; package layout, backend abstraction
- GitHub: temporalio/samples-go/dsl/workflow.go — official Temporal DSL DAG sample
- Go module proxy endpoints for all stack versions verified

### Secondary (MEDIUM confidence)
- Code Exchange — Temporal DSL, Zigflow — The Missing Temporal DSL, Why I built a YAML DSL for Temporal — competitor / contemporary positioning
- Uber Blog: Open-Sourcing Starlark Worker for Cadence — single-tier prior art
- A Letter to Cadence/Temporal Community (Long Quanzheng) — iWF rationale, durable-execution alternatives
- Every Simple Language Will Eventually End Up Turing Complete — anti-CEL argument
- On YAML Discussions (Earthly Blog) — YAML-as-DSL critique
- Buck2 — Starlark environments — environment scoping rules
- Embedding Starlark tutorials (Vivien) — predeclared globals patterns
- Koanf vs Viper — dependency / binary-size analysis
- Dash0: Go logging libraries 2026 — slog as default recommendation
- Tilt starkit, tilt-starlark-codegen, stripe/skycfg — extension registration / typed-builtin patterns
- Temporal Community forum threads on DAG patterns and context bleed — concrete community bug reports
- Understanding Non-Determinism in Temporal.io (Sanh Doan, Medium) — closure-variable pitfall

### Tertiary (LOW confidence — directional)
- Temporal Alternatives (ZenML), 10 best Temporal alternatives (Akka), Workflow Orchestration Platforms comparison (procycons) — competitive landscape framing
- withered-magic/starpls — generic Starlark LSP; future Skytime LSP reference
- Buildifier Recommendations (Aspect Build) — Starlark formatting bar
- Best practices article (Beamonte) — community-collated practices

---
*Research completed: 2026-04-27*
*Ready for roadmap: yes*
