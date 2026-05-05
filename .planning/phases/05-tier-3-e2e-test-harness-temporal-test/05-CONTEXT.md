# Phase 5: Tier-3 E2E Test Harness (`temporal_test`) - Context

**Gathered:** 2026-05-05
**Status:** Ready for planning

<domain>
## Phase Boundary

Build `pkg/testing` so consultants write end-to-end tests in `.star` files. Three Starlark builtins (`tester.workflow`, `tester.mock_action`, `tester.run`) drive `testsuite.TestWorkflowEnvironment`; the bridge intercepts the single generic `ExecuteBatch` activity and routes per-action calls back to Starlark mock lambdas evaluating in the same restricted predeclared environment as production lambdas. A replay helper runs each test twice and diffs Temporal event histories. `assert.*` from `go.starlark.net/starlarktest` surfaces failures into Go's `*testing.T`. `skytime test <dir>` is the discovery + runner entry point.

Phase 5 owns 6 v1 requirements: TEST-01..05 + CLI-03.

What's already in place (do not redo):
- `pkg/extension/testing.FakeCredentialHandler` — public, explicitly designed for Phase 5 reuse (Phase 2 doc note).
- `pkg/interpreter` test pattern — `env.OnActivity("ExecuteBatch", ...).Return(dag.ActionResults{...}, nil)` proven across `walk_*_test.go`.
- `pkg/interpreter/replay_determinism_test.go::runOnceCapturing` — already replays-twice and diffs structured events; lift into a public helper.
- D1-20 lambdaTimeGlobals — locked 20-key restricted env + `fail()`. Mock lambdas inherit verbatim; no expansion.
- D1-18 lambda IDs — content-hash + line + col; no closure-mutated state.
- D3-04 `dag.WorkflowInput{FlowName, ContentHash, InitState}` — wire format reused as-is.
- `pkg/cli` reusable + thin `cmd/skytime` + cobra/charmlog firewall (Phase 4) — Phase 5 adds `pkg/cli/test.go` + `cmd/skytime/test.go`; firewall stays intact.
- `${ctx.expr}` parser-time desugarer (Phase 04.1, D4.1-01..05) — applies to test files too (D5-G1).

</domain>

<decisions>
## Implementation Decisions

### Test File Shape & Discovery (Area A)

- **D5-A1: Test declaration = `def test_xxx()` functions, Go-style.** A `.star` test file declares tests as `def test_<name>():` functions; the runner enumerates these via Starlark module introspection. Top-level statements are file-scope setup (e.g., shared `tester.mock_action` calls). Familiar to Go users; multi-test files stay clean; matches `t.Run` ergonomics.

- **D5-A2: File-naming convention = `*_test.star`.** Go-style suffix. `skytime test <dir>` walks `<dir>` recursively and treats only files matching `*_test.star` as test files. Lets consultants keep production `.star` and test `.star` side-by-side without ambiguity.

- **D5-A3: Workflow setup kwargs all on `tester.workflow`.** Signature: `tester.workflow(name=..., init_state=..., retry_policy=..., timeouts=...)`. Maps directly onto `dag.WorkflowInput{FlowName, ContentHash, InitState}` plus Temporal's `StartWorkflowOptions`. `tester.run(flow=name)` just executes the named workflow against the registered mocks. Single declaration site; clearest reading order. Per-test variations of `init_state` are handled by re-declaring `tester.workflow` inside each `def test_*()`.

- **D5-A4: Mock scope = file-level defaults + per-test override.** Top-level `tester.mock_action` calls register file-level mocks visible to every `def test_*()` in the file. A `tester.mock_action` call inside a `def test_*()` shadows the file-level mock for the same `(extension, op)` for that test only. Implementation: stack of mock dicts; entering a `def test_*()` pushes a new layer; exiting pops it.

### Mock Binding & Match Precedence (Area B)

- **D5-B1: Match key = `(extension, op)` + optional `match={kwargs subset}` regex.** `tester.mock_action(extension="gh", op="get", mock_fn=...)` matches every `gh.get` call. Optional `match={"path": "^/users/[a-z]+$"}` narrows: the mock fires only when the call's `kwargs["path"]` matches the regex. Multiple mocks for the same `(extension, op)` are allowed when their `match` keys differ.

- **D5-B2: No-mock-found → fail fast with `NonRetryableErr`.** Workflow fails with `"no mock for gh.delete at <flow_file>:<line>:<col> (step \"<name>\")"`. Forces explicit mocking; no silent successes. Reuses Phase 2's `extension.ErrNonRetryable` wrap so the workflow surfaces a clean Starlark callsite, not a Go panic.

- **D5-B3: Wildcard support = `op="*"` per extension.** `tester.mock_action(extension="gh", op="*", mock_fn=...)` catches any `gh.*` call not matched by a more-specific mock. No cross-extension wildcard (`extension="*"`) for v1 — re-evaluate if Phase 6 demands it.

- **D5-B4: Precedence = specificity ladder; recency breaks ties within a tier.**
  - Tier 1: `(extension, op)` + `match={...}` regex matched
  - Tier 2: `(extension, op)` exact, no `match` constraint
  - Tier 3: `(extension, "*")` wildcard
  - Most-specific tier ALWAYS wins regardless of registration order.
  - Within the same tier, most-recently-registered wins (so per-test `(gh, get)` shadows file-level `(gh, get)`; per-test `(gh, *)` shadows file-level `(gh, *)`).

- **D5-B5: Regex match semantics = Go `regexp` syntax, partial-match by default.** `match={"path": "/users/octocat"}` matches any call whose `kwargs["path"]` contains `/users/octocat`. Anchor explicitly with `^...$` for exact-match (e.g., `match={"path": "^/users/octocat$"}`). Document this footgun in `tester.md`. Compile each pattern once at registration; cache `*regexp.Regexp` to avoid per-call recompile.

- **D5-B6: Match key types restricted to strings.** `match={...}` values must be Starlark strings (regex patterns). Non-string kwargs (ints, dicts) cannot be matched on; consultant disambiguates inside the lambda body via `kwargs["foo"] == 42`. Keeps the matcher simple; matches the real-world need (paths, names, IDs are strings).

### Mock Function I/O Contract (Area C)

- **D5-C1: Mock lambda signature = `lambda kwargs, attempt: ...`.** Two positional args.
  - `kwargs` is a Starlark dict of resolved action kwargs (post-`${ctx.expr}` interpolation, post-credential-resolution; values are strings/ints/lists/dicts as the action's schema dictates). Frozen before the lambda runs.
  - `attempt` is the 1-indexed integer retry count (first call = 1, retried call = 2, ...). Lets `.star` tests simulate transient failures: `if attempt < 3: err(msg="transient") else: ok(value={...})`.
  - No `ctx`, no `credential` — both addressed via the resolved kwargs dict (D5-C1a: credential ID is exposed in `kwargs["_credential_id"]` if the action declared one; tests assert on this for credential-routing checks; raw `Secret` value is NEVER passed to the lambda).

- **D5-C2: Return shape = typed builders `ok(value=)` / `err(msg=)` / `nonretryable(msg=)`.** Three predeclared globals in the mock lambda's environment, mirroring `dag.ActionResult` sealed sum:
  - `ok(value=...)` → `dag.OkResult{Output: <converted value>}`
  - `err(msg=...)` → retryable error; Temporal's RetryPolicy fires; `attempt` increments on next call
  - `nonretryable(msg=...)` → `dag.NonRetryableErrResult` via `extension.ErrNonRetryable` wrap; workflow fails immediately
  - Self-documenting; matches Phase 2 4xx/5xx classification (D4-14); satisfies TEST-03 (retry simulation needs retryable err path).
  - These three builders are added to a NEW restricted predeclared env — the **mock-lambda env** — which extends `lambdaTimeGlobals` with `ok`/`err`/`nonretryable` plus the mock receives `attempt` argument. Production lambdas' env stays unchanged (D1-20 holds).

- **D5-C3: Output mapping = bridge dict → `*starlarkstruct.Struct`.** `ok(value={"login": "octocat"})` converts the Starlark dict to a `*starlarkstruct.Struct` via Phase 1's existing bridge conversion (`bridge.StructFromDict` or equivalent). Downstream code in the flow reads `ctx.step_output.login` dot-notation identically to real activity output. Symmetry with prod path; reuses verified code; `*starlarkstruct.Struct` round-trips through Temporal serialization.
  - Lists, scalars, nested dicts all convert per the existing bridge rules.
  - Implementation note for planner: confirm whether `bridge.StructFromDict` is exported; if not, expose it (or a wrapper) through `pkg/bridge`.

- **D5-C4: None / no return = error `"mock must return ok/err/nonretryable"`.** The harness wraps the mock lambda call; if it returns Starlark `None`, raise `NonRetryableErr` with the lambda position. Forces consultants to be intentional; None is almost always a forgotten `return`.

### Replay Determinism Check (Area D)

- **D5-D1: Replay model = always-on.** Every `tester.run` runs the workflow twice: once for behavior, once for replay-determinism. Both runs use the same registered mocks and same `init_state`. TEST-04 explicitly mandates this — opt-in or runner-flag would let tests pass without exercising the check, defeating Tier-3's whole purpose. Acceptable cost (~2x test time); the determinism guarantee is what makes Skytime workflows safe to deploy.

- **D5-D2: Divergence report = first-divergent event diff with payload before/after.** Format:
  ```
  FAIL users_test.test_existing_user (replay diverged)
    event 7 (ActivityTaskScheduled) diverged:
      run1.payload.kwargs.path = "/users/octocat"
      run2.payload.kwargs.path = "/users/foo"
    flow callsite: users_flow.star:14:5 (step "fetch user")
    test callsite: users_test.star:23:5 (tester.run)
  ```
  Stops at first divergent event (cascade of downstream divergences would be noise). Includes both flow callsite (where the divergent action was emitted) and test callsite (which `tester.run` failed) for triage.

- **D5-D3: Starlark callsite attribution = originating `step()` callsite in the flow `.star`.** The divergence error points at the flow file/line/col that produced the divergent event (e.g., `users_flow.star:14:5 (step "fetch user")`), NOT the mock or the `tester.run`. Most consultants think in flow code; pointing at the suspect action call leads directly to the bug. Implementation hint: each `dag.ActionRef` already carries a `Pos` field (Phase 1); the harness threads it into the divergence report.

- **D5-D4: Diff scope = Temporal event types + sequence + payload byte-equality.** Mirrors Temporal SDK's own replay-check semantics. Catches:
  - structural divergence (extra/missing events, different event type at a position)
  - payload divergence (kwargs ordering, map iteration, time.now() leakage)
  - the Phase 04.2 `result_bound` payload-divergence bug class.
  - Final workflow state is downstream of events; if events are byte-equal, state is byte-equal by construction. Don't double-check.

### Runner UX (`skytime test`) — Area E

- **D5-E1: Output format on TTY = static line-per-test, Go-test style.**
  ```
  --- PASS: users_test.test_existing_user (0.04s)
  --- FAIL: users_test.test_default_user (0.03s)
      assertion failed at users_test.star:31:5
        expected: "octocat"
        got:      "default-user"
  --- PASS: users_test.test_create_issue (0.06s)
  PASS  users_test.star  3 tests  1 failed (0.13s)
  ```
  Familiar; CI-readable; plays well with log capture; no ANSI complications. Phase 4's live progress block exists for one long-running flow execution, which doesn't fit many short tests.
  - One status line per test (`--- PASS` / `--- FAIL` / `--- SKIP`).
  - Indented assertion-failure detail under FAIL lines.
  - Per-file summary footer.
  - Final overall PASS/FAIL summary line.

- **D5-E2: CI integration = `--format=json` flag mirroring `go test -json`.** Default human output is D5-E1's Go-test style. `skytime test --format=json` emits one JSON record per event:
  ```json
  {"action":"start","package":"users_test.star","test":"test_existing_user"}
  {"action":"output","package":"users_test.star","test":"test_existing_user","output":"--- PASS\n"}
  {"action":"pass","package":"users_test.star","test":"test_existing_user","elapsed":0.04}
  ```
  Compatible schema means GitHub Actions, gotestsum, tparse, and existing CI tools work without writing Skytime-specific parsers. JUnit XML conversion can be deferred to gotestsum or similar.

- **D5-E3: Test selection flag = `--run <regex>`, Go-style.** `skytime test --run 'users_test\.test_existing'` filters test names. Regex matches against `<file_basename>.<test_name>` (e.g., `users_test.test_existing_user`). Familiar to Go developers; one flag covers single-test, subset, and file-only matches. Empty / absent = run all.

- **D5-E4: Exit code semantics = 0 on all-pass, 1 on any-fail.** Standard Go test convention. CI pipelines wire this without reconfiguration.

- **D5-E5 (Claude's Discretion): Test parallelization within a file = sequential for v1.** Temporal `testsuite.TestWorkflowEnvironment` has internal serialization concerns for shared mocks; running tests in parallel within a single file risks race conditions in the registry. Cross-file parallelism may be safe — planner discretion based on whether the registry can be safely cloned per file. Document the choice in tester.md.

### `assert.*` Surfacing (Area F)

- **D5-F1: `assert.*` failure routing = one sub-`*testing.T` per `def test_*` via `t.Run`.** The runner creates a parent `*testing.T`-like context (or a real `testing.T` if invoked from `go test` integration), and for each discovered `def test_*` calls `t.Run("test_<name>", ...)`. Inside the subtest, `starlarktest.SetReporter(thread, subtestT)` binds the subtest's `*testing.T`. Each `def test_*` fails independently; one bad test doesn't poison siblings. Mirrors Go's `t.Run` ergonomics; gives per-test pass/fail granularity in JSON output (D5-E2).

- **D5-F2 (Claude's Discretion): Multi-assertion behavior within a single test = `starlarktest`'s default (accumulate).** `assert.*` from `go.starlark.net/starlarktest` accumulates all assertion failures in a single test by default; the test fails at the end if any accumulated. Planner should keep the library's default unless TEST-05 review reveals a need to change it.

- **D5-F3: `assert.*` callsite format = Starlark file:line:col + assertion detail.** Format (matches D5-E1 indented detail):
  ```
      assertion failed at users_test.star:31:5
        assert.eq("octocat", actual_login)
        actual: "default-user"
  ```
  Pure Starlark surface; no Go stack traces in default output (CLI-03 explicit requirement: "no Go stack traces in default output"). `--debug` flag (Phase 4 D4-19) escalates to Go traces.

### Interpolation in Tests (Area G)

- **D5-G1: `${ctx.expr}` works in test `.star` files identically to production.** Test files go through the same parser as production `.star` files; the desugarer (Phase 04.1, D4.1-01..05) sees `${ctx.user_id}` and synthesizes `lambda ctx: str(ctx.user_id)` regardless of which file class the source belongs to.
  - In `tester.workflow(init_state={"user_id": "${ctx.input.user}"}, ...)`: typically NOT used because `init_state` is the seed; ctx isn't yet bound. Interpolation here would surface as a parse-time validation error per D4-03 ("ctx not in scope at parse time").
  - In `tester.workflow(name="${ctx.test_name}")`: meaningful when consultants table-drive over a fixture set.
  - Inside `mock_fn` lambda body strings: works the same way (lambdas evaluate at workflow time with full ctx).
  - Zero new code; consistency with prod path; contradicting this would surprise consultants who learned `${...}` as the universal pattern.

### Claude's Discretion

The planner decides:
- Exact internal layout of `pkg/testing` (file structure, helper names) within the conventions established by `pkg/parser` / `pkg/interpreter`.
- Whether `bridge.StructFromDict` or an analog needs to be newly exported through `pkg/bridge`.
- Internal storage shape of the mock registry (stack of dicts? layered map?) — must satisfy D5-A4 + D5-B4 invariants.
- `assert.*` multi-failure accumulation vs. fail-fast within a single test (default to library behavior; D5-F2).
- Whether tests can opt out of replay determinism for very-long workflows (NOT v1; revisit if Phase 6 surfaces a real perf complaint).
- Cross-file test parallelization in `skytime test` (D5-E5; sequential within a file is fixed).
- Internal representation of the JSON output records (D5-E2) — mirror `go test -json` exactly.

### Folded Todos

None — `gsd-tools todo match-phase 5` returned 0 matches.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase Goal & Requirements

- `.planning/ROADMAP.md` §"Phase 5: Tier-3 E2E Test Harness (`temporal_test`)" — phase goal, 5 success criteria, dependency on Phase 3
- `.planning/REQUIREMENTS.md` — TEST-01..05 + CLI-03 (Phase 5 requirements catalog)
- `.planning/PROJECT.md` — vision, hard rules ("no string compilation", "no dynamic activities", "no context bleed"), validated requirements through Phase 04.3

### Prior-Phase Context (locked decisions Phase 5 inherits)

- `.planning/phases/01-type-spine-extension-contract-parser-bridge-foundations/01-CONTEXT.md` — D1-18 lambda IDs (content-hash + line + col), D1-20 lambdaTimeGlobals (locked 20-key restricted env + `fail()`), `*starlarkstruct.Module` extension contract
- `.planning/phases/02-generic-activity-block-batch-dispatch-credentials/02-CONTEXT.md` — D2-* generic activity dispatch via `ExecuteBatch`, `dag.ActionResults` sealed sum (`OkResult` / `NonRetryableErrResult` / etc.), `pkg/extension/testing.FakeCredentialHandler` for reuse, D4-14 idempotence + 4xx/5xx classification, `extension.ErrNonRetryable` sentinel
- `.planning/phases/03-lambda-serialization-decision-interpreter-worker/03-CONTEXT.md` — D3-04 `dag.WorkflowInput{FlowName, ContentHash, InitState}` wire format, D3-07 filesystem registry frozen-after-boot, D3-* interpreter walk_* patterns
- `.planning/phases/04-static-validation-tier-cli-skeleton/04-CONTEXT.md` — `pkg/cli` reusable + thin `cmd/skytime` + cobra/charmlog firewall (allow-list `[cmd/skytime, pkg/cli]`); CLI subcommand patterns
- `.planning/phases/04.1-dynamic-step-kwargs-lambda-accepting-step-action-fn-variant-for-runtime-built-action-kwargs/04.1-CONTEXT.md` — D4.1-01..05 `${ctx.expr}` parser-time desugaring (applies to test files too); D4.1-22 carve-out (parser-time desugaring permitted, runtime template engines forbidden)
- `.planning/phases/04.2-if-cond-as-expression-with-strict-equality-result-binding/04.2-CONTEXT.md` — D4.2-05 dual `fail()` semantics (parse-time node-emit vs. lambda-time `fail` global); replay-determinism payload-divergence bug class

### Existing Code (read before designing internals)

- `pkg/extension/testing/fake_handler.go` — public Phase 5 reuse target; doc note explicitly mentions Phase 5
- `pkg/interpreter/replay_determinism_test.go` — `runOnceCapturing` pattern (lift into a public replay helper)
- `pkg/interpreter/walk_step_actionfn_test.go` — proven `env.OnActivity("ExecuteBatch", ...).Return(...)` pattern; Phase 5 routes the mock callback into a Starlark thread instead of stub Go data
- `pkg/interpreter/test_helpers_test.go` — `helperBuildActionFnFlow`, `helperRegisterFakeExecuteBatch`, lambda-compilation helpers (templates for harness internals)
- `pkg/parser/{globals,builtins}.go` — `*starlarkstruct.Module` registration pattern; `tester` module follows the same shape
- `pkg/parser/template.go` (or wherever D4.1-01 desugarer lives) — `${ctx.expr}` pipeline applies to test files too
- `pkg/bridge/struct.go` — dict ↔ `*starlarkstruct.Struct` conversion path Phase 5 reuses
- `pkg/cli/{root,run,validate,info,dev_server}.go` — subcommand patterns for `pkg/cli/test.go`

### External Docs (Temporal SDK + Starlark)

- [`go.temporal.io/sdk/testsuite` package docs](https://pkg.go.dev/go.temporal.io/sdk/testsuite) — `WorkflowTestSuite`, `TestWorkflowEnvironment.OnActivity` / `RegisterWorkflow` / `ExecuteWorkflow` / `IsWorkflowCompleted` / `GetWorkflowError` / `GetWorkflowResult`
- [`go.starlark.net/starlarktest` package docs](https://pkg.go.dev/go.starlark.net/starlarktest) — `LoadAssertModule()`, `SetReporter(thread, t)`, `GetReporter(thread)`; the `*testing.T` integration contract
- [Go test JSON output schema](https://pkg.go.dev/cmd/test2json) — D5-E2 reference; `{action, package, test, output, elapsed}` records
- [Temporal "Replay" docs](https://docs.temporal.io/develop/go/testing-suite#replay) — replay-equality contract Phase 5 mirrors

### Documentation Targets (Phase 04.3 hand-off → Phase 5 extends)

- `docs/for-flow-authors/extensions/http.md` — sibling reference for the testing builtin module; Phase 5 adds `docs/for-flow-authors/testing.md` (or similar) following the same template
- `docs/architecture.md` §"The Parse/Execute Split" — required reading; tester builtins straddle the split (mocks are runtime constructs registered at parse time)
- `docs/reference/cli.md` — Phase 5 adds a `## skytime test` section (D5-E1..E4) following the same 6-H3 template (Synopsis, Motivation, Flags, Exit Codes, Example, See Also)
- `docs/reference/builtins.md` — auto-generated; Phase 5's `tester.*` builtins gain `// skytime:doc` markers in `pkg/parser/builtins.go` (or a new `pkg/parser/builtins_tester.go` registered alongside production globals)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

- **`pkg/extension/testing/FakeCredentialHandler`** — public CredentialHandler keyed by ID; designed for Phase 5 + Phase 6 reuse. Tests inject `&FakeCredentialHandler{Creds: map[string]extension.Credential{...}}` and let production credential resolution flow through.
- **`pkg/interpreter/replay_determinism_test.go::runOnceCapturing`** — already replays-twice + records events. Lift into `pkg/testing/replay.go` (public `RunOnceCapturing` or similar).
- **`pkg/interpreter/walk_*_test.go::env.OnActivity("ExecuteBatch", ...).Return(...)`** — proven pattern. Phase 5's mock router replaces the static `.Return(stub)` with a callback that invokes the matched Starlark mock lambda.
- **`pkg/parser` `*starlarkstruct.Module` pattern** — `tester` is a new Module exposing `workflow`, `mock_action`, `run` builtins; mirrors the bundled HTTP extension's Module shape.
- **`pkg/bridge.StructFromDict` (or equivalent)** — dict → `*starlarkstruct.Struct`; D5-C3 reuses this.
- **`pkg/cli` cobra subcommand pattern** — `pkg/cli/test.go` follows `pkg/cli/run.go`'s structure; persistent flags (D4-09..13) carry over.

### Established Patterns

- **Restricted predeclared env (D1-20) for lambda evaluation** — Phase 5 extends `lambdaTimeGlobals` with `ok` / `err` / `nonretryable` builders (mock-lambda env) but never expands the production lambda env. Two distinct envs; same content-hash + line+col ID scheme.
- **Position-aware errors (`*dag.ValidationError`, `NonRetryableErr` with Pos)** — divergence reports (D5-D2) and no-mock errors (D5-B2) thread position into the failure message; never surface Go stack traces (CLI-03).
- **Bridge dict ↔ Struct conversion (Phase 1)** — symmetric prod/test path for action output (D5-C3).
- **`syntax.Walk` over file bytes** — D4-02 `ctx.<name>` walker pattern reused for any test-time validation Phase 5 needs.
- **`go test -race` clean** — all new concurrency (mock dispatch goroutine, replay capture) MUST pass `-race`.
- **`workflowcheck`-clean** — no native `go` keyword inside workflow code; `workflow.Go` only.

### Integration Points

- **`pkg/cli/root.go`** — `RegisterTestCommand(...)` (functional option) added alongside `Run`/`Validate`/`Info`/`DevServer`. Cobra subcommand wiring matches Phase 4.
- **`cmd/skytime/main.go`** — calls the new functional option to register the test subcommand.
- **`pkg/parser/globals.go`** — `tester` Module registered as a NEW `starlark.NewBuiltin` family; ONLY when the parse mode is "test" (NOT for production flow files). The parser distinguishes via a flag or a separate `ParseTest` entrypoint.
- **`pkg/activity/execute_batch.go`** — Phase 5 does NOT touch the production activity. The harness intercepts at `testsuite.TestWorkflowEnvironment.OnActivity` level, BEFORE the activity package even sees the call.
- **`pkg/interpreter`** — registry + `NewWorkflow(registry)` reused as-is; `tester.run` constructs an in-memory `Registry`, registers the parsed flow under its content hash, freezes, then ExecuteWorkflow inside `TestWorkflowEnvironment`.
- **`tests/firewall_*.go`** — `pkg/testing` may import `go.temporal.io/sdk/testsuite` (NEW allow-list entry) but NOT `go.temporal.io/sdk/activity`. Update the activity-firewall allow-list and add a sibling firewall test for the testsuite import.

</code_context>

<specifics>
## Specific Ideas

- **`assert.*` failure format borrows Go test's indented detail style** — readability and CI-tool compatibility both win. See D5-F3 example.
- **`go test -json`-mirror schema** is the explicit reference for D5-E2 — same field names, same record shape; ecosystem of CI consumers (gotestsum, tparse, GitHub Actions test annotations) Just Works.
- **Test files must use `*_test.star` suffix** — exact Go convention. No special "skytime suffix" experiments.
- **`def test_*` discovery is via Starlark module enumeration**, not regex over file source. Lambdas / nested defs are not tests; only top-level `def test_*` — name-prefix match on the symbol table after parse.

</specifics>

<deferred>
## Deferred Ideas

- **Cross-extension wildcards (`extension="*"`)** — out of scope for v1. Re-evaluate if Phase 6 demands it.
- **Per-kwarg type matching beyond strings** — D5-B6 restricts `match={...}` values to strings; matching on int/dict kwargs is YAGNI for v1, consultant disambiguates inside the lambda.
- **JUnit XML output** — D5-E2 ships JSON only; JUnit consumers can convert via gotestsum.
- **Snapshot testing** — comparing `tester.run` results to a checked-in JSON snapshot. Not in TEST-01..05; future v1.x or v2.
- **Fixture frameworks** — pytest-style `@fixture` / `conftest.py` analog. YAGNI for v1; file-level setup via top-level statements is sufficient.
- **Mock-call assertion (`tester.assert_called(extension="gh", op="get", times=2)`)** — like testify/mock's `AssertExpectations`. Not in TEST-01..05; consultants assert via `assert.eq(captured_count, 2)` patterns inside `def test_*`. Revisit if Phase 6 reveals a real ergonomic gap.
- **Per-test `--debug` escalation to Go traces** — Phase 4's `--debug` flag (D4-19) handles this at runner level; Phase 5 doesn't add a per-test flag.
- **`tester.replay(...)` standalone builtin** — D5-D1 makes replay always-on; no separate builtin. If a future use case wants a deterministic-replay smoke without re-running, revisit.
- **Live progress block for tests** — D5-E1 is static line-per-test; Phase 4's live block is for one long-running flow. Don't try to unify.

### Reviewed Todos (not folded)

None — phase has no pending todos via `gsd-tools todo match-phase 5`.

</deferred>

---

*Phase: 05-tier-3-e2e-test-harness-temporal-test*
*Context gathered: 2026-05-05*
