---
phase: 05-tier-3-e2e-test-harness-temporal-test
plan: 02
subsystem: testing

tags: [starlark, temporal, testsuite, mock-router, attempt-counter, mock-lambda-env, parse-time-globals]

# Dependency graph
requires:
  - phase: 05-tier-3-e2e-test-harness-temporal-test
    provides: "MockRegistry with 3-tier ladder (Plan 01); Parser.testMode + WithTestModule option (Plan 01); MockOperationOutput JSON wire format (Plan 01); pkg/testing firewall allow-list (Plan 01)"
provides:
  - "tester *starlarkstruct.Module with workflow/mock_action/run members (D5-A3, D5-B1)"
  - "tester.workflow populates *WorkflowSpec; last-write-wins (RESEARCH Open Q5)"
  - "tester.mock_action registers MockEntry: extension/op + regex-pre-compiled match + captured mock_fn lambda + RegisterPos for D5-D2 attribution"
  - "Mock-lambda env = bridge.LambdaTimeGlobals ∪ {ok, err, nonretryable} (D5-C2); production lambda env unchanged (D1-20 invariant)"
  - "buildExecuteBatchCallback: Go callback signature exactly matching pkg/activity.ExecuteBatch (Pitfall 1); panic-recovery wraps with extension.ErrNonRetryable (Pitfall 8)"
  - "WireMockCallback: env.RegisterActivityWithOptions + env.OnActivity('ExecuteBatch', mock.Anything, mock.Anything).Return(callback) (Pitfall 1)"
  - "AttemptCounter: per-(FlowName, StepIdx, ActionIdx) attempt dispenser (TEST-03); -race-clean via sync.Mutex"
  - "WithTestPredeclared parser option: Plan-02 addition that injects ok/err/nonretryable into the parse-time globals so mock_fn lambda bodies resolve at parse time (does NOT mutate production lambda env)"
  - "D5-B2 verbatim no-mock-found message: `no mock for <ext>.<op> at <file>:<line>:<col> (step \"<name>\")` wraps extension.ErrNonRetryable"
  - "D5-C4 enforcement: None / non-mock-result returns NonRetryableErr 'mock must return ok/err/nonretryable' citing lambda BodyPos"
  - "D5-C1a credential exposure: kwargs['_credential_id'] populated before mock lambda runs (raw Secret never passed)"
  - "Firewall meta-test activated: TestPkgTesting_ImportsTestsuite no longer skips; pkg/testing imports go.temporal.io/sdk/testsuite as required"
affects: [05-03, 05-04, 05-05, 05-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "starlarkstruct.Module ↔ Phase-5 builder closures: tester namespace exposed via Members; ok/err/nonretryable as TOP-LEVEL parse-time globals (D5-C2); same closures invoked at execute time"
    - "WithTestPredeclared option for cycle-broken parse-time-only globals (cli/test.go wires it without parser→testing import)"
    - "MockResult sealed sum (MockOk/MockErr/MockNonRetryable) wrapped in mockResultValue starlark.Value sentinel; AsMockResult unwraps for D5-C4 enforcement"
    - "ActionKey = (FlowName, StepIdx, ActionIdx) structural triple for replay-deterministic attempt counting"
    - "Pitfall-8 panic recovery: deferred recover() inside callback converts Go panic → single NonRetryableErrResult with ErrNonRetryable wrap"
    - "Position-based mock CapturedLambda IDs: mock:<file>:<line>:<col> instead of D-18 content-hash (parser session not in scope at builtin closure)"

key-files:
  created:
    - "pkg/testing/builders.go (MockResult sealed sum + builder closures + MockLambdaGlobals + MockLambdaParseTimeBuilders)"
    - "pkg/testing/builders_test.go (D5-C2 named test + arity/positional/wrong-type rejection tests)"
    - "pkg/testing/attempts.go (AttemptCounter + ActionKey)"
    - "pkg/testing/attempts_test.go (per-key increment + race-clean + snapshot semantics)"
    - "pkg/testing/module.go (NewTesterModule + tester.run placeholder)"
    - "pkg/testing/module_test.go (10 named module tests; helperParseTestSrc helper)"
    - "pkg/testing/builtin_workflow.go (tester.workflow → *WorkflowSpec)"
    - "pkg/testing/builtin_mock_action.go (tester.mock_action → MockEntry; D5-B3 / D5-B5 / D5-B6 enforcement)"
    - "pkg/testing/router.go (buildExecuteBatchCallback + WireMockCallback + evalMockLambda + buildMockOutput)"
    - "pkg/testing/router_test.go (12 router tests covering D5-B2/C1a/C4 + Pitfall 1 + Pitfall 8)"
  modified:
    - "pkg/parser/globals.go (test-mode injection of g['tester'] + p.testPredeclared)"
    - "pkg/parser/parser.go (testPredeclared field added to Parser struct)"
    - "pkg/parser/options.go (new WithTestPredeclared option)"

key-decisions:
  - "[D5-bridge-myth-q4] Router uses bridge.FromStarlarkValue (Starlark→Go), NOT a hypothetical bridge.StructFromDict; MockOperationOutput wrapping happens in pkg/testing"
  - "[Plan 02 deviation, Rule 2] Added WithTestPredeclared parser option to bind ok/err/nonretryable at parse time — Starlark's resolver requires those names visible AT PARSE-OF-FILE TIME so mock_fn lambda free vars resolve. Production parses (testMode=false) NEVER see these names; D1-20 lambdaTimeGlobals invariant preserved verbatim. Cycle break preserved (parser still does not import pkg/testing); pkg/cli/test.go (Plan 06) wires MockLambdaParseTimeBuilders via the new option."
  - "Position-based mock CapturedLambda IDs (`mock:<file>:<line>:<col>`) instead of D-18 content-hash format because builtin closures don't have access to the parser session's FileBytes cache. Router only reads captured.Fn (and uses ID for thread-name), so the position-derived ID is functionally equivalent."
  - "Test attribution preference: callerPosFromThread walks thread.CallFrame from innermost outward, skipping <builtin> + tester.mock_action frames so RegisterPos lands on the user's .star line (mirrors Phase 04.1 fail()-callsite preservation)."
  - "fail() in mock body inherits PARSE-time builtinFail (D4.2-05 dual-semantics) — the value at execute-time is *dag.Fail, surfaces through D5-C4 'mock must return ok/err/nonretryable' rather than a fail()-style EvalError. Test (TestRouter_MockEvalErr_NonRetryable) was rewritten to use integer division by zero to exercise the EvalError surface."
  - "AttemptCounter is keyed by structural (FlowName, StepIdx, ActionIdx) triple, NOT ref.Pos — content-hash IDs change with cosmetic edits but slot identity is structural; replay-equality (D5-D1) requires the structural form."
  - "Per-action NonRetryableErrResult on no-mock-found instead of returning an activity-level error: keeps batched dispatch consistent (mirrors quick-260502-onc 4xx classification)."
  - "Last-write-wins for tester.workflow re-call (RESEARCH Open Q5); inside def test_*() the new spec shadows file-scope; explicit override is opt-in by the test author."

patterns-established:
  - "WithTestPredeclared(starlark.StringDict) + WithTestModule(builder fn): pair of cycle-broken parser options; pkg/cli/test.go (Plan 06 entry point) imports pkg/testing and wires both"
  - "MockResult sealed-sum + mockResultValue wrapper: identical pattern to dag.ActionResult ↔ nodeValue / dag.StarlarkLambda; sealed via unexported isMockResult() to gate API evolution"
  - "buildExecuteBatchCallback signature MUST match pkg/activity.ExecuteBatch verbatim — testify/mock dispatches reflectively; mismatched arity panics deep inside the SDK"
  - "Pitfall-8 panic recovery template: defer-recover at top of callback wraps with ErrNonRetryable so workflow-side classification remains correct"

requirements-completed: [TEST-01, TEST-02, TEST-03]

# Metrics
duration: 35min
completed: 2026-05-05
---

# Phase 5 Plan 02: Tester Module + Mock Router Summary

**`tester.*` parse-time builtins with regex-compiled match registry, mock-lambda env (ok/err/nonretryable) extending bridge.LambdaTimeGlobals without mutation, and ExecuteBatch callback that intercepts TestWorkflowEnvironment activity calls and routes to Starlark mock lambdas with per-(flow,step,action) attempt counting.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-05-05T17:55:54Z
- **Completed:** 2026-05-05T18:30:00Z
- **Tasks:** 3
- **Files created:** 10
- **Files modified:** 3

## Accomplishments

- `tester` namespace landed as a parse-time-only builtin in test mode (production parses are unchanged; verified by `TestTesterModule_ProductionParsePathUnchanged`).
- Three sealed-sum MockResult builders (`ok` / `err` / `nonretryable`) plus `MockLambdaGlobals()` and `MockLambdaParseTimeBuilders()` helpers; D1-20 lambdaTimeGlobals invariant preserved (verified by `TestMockLambdaEnv_DoesNotMutateProduction`).
- `tester.workflow(name, init_state, retry_policy, timeouts)` populates a `*WorkflowSpec` (last-write-wins); `tester.workflow(name="users") then tester.workflow(name="admins")` overrides cleanly (verified by `TestTesterModule_WorkflowReCallLastWriteWins`).
- `tester.mock_action` registers a `MockEntry` in the active frame: regex compiled at registration (D5-B5), `extension="*"` rejected (D5-B3), non-string match values rejected with the verbatim `D5-B6` token (D5-B6), arity-checked (`mock_fn must accept exactly 2 positional args`).
- `buildExecuteBatchCallback` routes per-action calls back to Starlark via `starlark.Call(captured.Fn, (kwargs, attempt))`; D5-B2 message shape verbatim (`no mock for gh.delete at users.star:14:5 (step "fetch user")`); D5-C4 None-return surface; D5-C1a `kwargs["_credential_id"]` exposure; Pitfall-8 panic recovery converts any Go panic into a single `NonRetryableErrResult`.
- `WireMockCallback` wires `env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(callback)` with EXACTLY two `mock.Anything` matchers (Pitfall 1).
- `AttemptCounter.NextFor(ActionKey)` dispenses 1-indexed attempt counts under `-race`; `TestAttemptCounter_RaceClean` exercises 100 goroutines.
- Phase 5 firewall meta-test (`TestPkgTesting_ImportsTestsuite`) now ACTIVATES — no longer t.Skip; logs `verified pkg/testing imports go.temporal.io/sdk/testsuite (Phase 5 D5-firewall-q8 deviation honored)`.

## Task Commits

Each task committed atomically:

1. **Task 1: Mock-lambda env (ok/err/nonretryable) + AttemptCounter** — `82c381b` (feat)
2. **Task 2: tester Module + parser test-mode injection** — `970d0d6` (feat)
3. **Task 3: Mock router callback (buildExecuteBatchCallback)** — `d350e91` (feat)

## Files Created/Modified

**Created (pkg/testing):**

- `pkg/testing/builders.go` — `MockResult` sealed sum, `MockOk` / `MockErr` / `MockNonRetryable`, builder closures (`builtinOk` / `builtinErr` / `builtinNonRetryable`), `MockLambdaGlobals()` runtime env, `MockLambdaParseTimeBuilders()` parse-time env, `AsMockResult()` unwrapper.
- `pkg/testing/builders_test.go` — D5-C2 named test + arity / positional / wrong-type rejection.
- `pkg/testing/attempts.go` — `ActionKey` triple + `AttemptCounter` with sync.Mutex.
- `pkg/testing/attempts_test.go` — per-key increment + -race + snapshot semantics + fresh-counter starting.
- `pkg/testing/module.go` — `NewTesterModule(reg, ws)` returns `*starlarkstruct.Module` with workflow/mock_action/run; `tester.run` ships as a clear "not yet implemented (Plan 04)" placeholder.
- `pkg/testing/module_test.go` — 10+ tests + `helperParseTestSrc` shared helper.
- `pkg/testing/builtin_workflow.go` — `tester.workflow` populates `*WorkflowSpec.Name + .InitState` via bridge.FromStarlarkValue.
- `pkg/testing/builtin_mock_action.go` — `tester.mock_action` registers `MockEntry`; rejects bad arity, extension="*", non-string match values, bad regex.
- `pkg/testing/router.go` — `buildExecuteBatchCallback` (per-action dispatch + D5-B2 + D5-C4 + D5-C1a), `evalMockLambda`, `buildMockOutput`, `kwargsAsStringMap`, `WireMockCallback`, `joinNonRetryable`.
- `pkg/testing/router_test.go` — 12 router tests + parked Plan-03 integration test.

**Modified (pkg/parser):**

- `pkg/parser/globals.go` — test-mode block: injects `g["tester"]` from `p.testModuleBuilder` + iterates `p.testPredeclared` for additional globals (ok/err/nonretryable). Defensive WithTestModule-required check.
- `pkg/parser/parser.go` — added `testPredeclared starlark.StringDict` field.
- `pkg/parser/options.go` — added `WithTestPredeclared(starlark.StringDict) Option` (Plan 02 addition).

## Decisions Made

See frontmatter `key-decisions` for the full list. Most-load-bearing:

1. **`WithTestPredeclared` parser option (Plan 02 addition).** Starlark's resolver binds free variables in lambda bodies AT PARSE-OF-FILE TIME. `lambda kwargs, attempt: ok(value={...})` references `ok` as a free variable; if `ok` isn't in the parse-time predeclared dict the resolver errors with "undefined: ok". The mock-lambda env (D5-C2) is documented as a *runtime* env, but Starlark's contract requires the names to be visible at parse time too. The fix: add a parser option that injects an additional StringDict into the parse-time globals only when test mode is active. Production parses NEVER see `ok` / `err` / `nonretryable` — the production lambda env (`bridge.LambdaTimeGlobals` D1-20) is unchanged. Cycle break preserved: pkg/parser does NOT import pkg/testing; cli/test.go (Plan 06) wires `MockLambdaParseTimeBuilders()` via the new option.

2. **`fail()` inside mock_fn body resolves to PARSE-time `builtinFail` (D4.2-05 dual-semantics).** The test that initially exercised the EvalError surface via `fail("kaboom")` returned a `*dag.Fail` value (because the parse-time predeclared `fail` is `builtinFail`); the router surfaced this through D5-C4 (`mock must return ok/err/nonretryable`) instead. Documented as intentional behavior; rewrote the test to use integer division by zero (`1 // 0`) which actually raises a Starlark *EvalError* at execute time.

3. **Position-based mock CapturedLambda IDs.** Builtin closures don't carry the parser session, so D-18 content-hash IDs are out of reach without plumbing changes. Router-side dispatch only reads `captured.Fn` (function pointer) and uses the ID for thread-naming, so a position-derived ID (`mock:<file>:<line>:<col>`) is functionally equivalent. Plan 04 may revisit if content-hash IDs become required for replay-determinism diagnostics.

4. **D5-B2 message via per-action `NonRetryableErrResult`** (not activity-level error). Mirrors Phase 4 quick-260502-onc 4xx classification: the activity itself succeeds (returns the result slice), and the workflow's walk_step path observes the NonRetryable in the slice and fails the step. Keeps batched dispatch consistent.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] `WithTestPredeclared` parser option**

- **Found during:** Task 2 (running module tests).
- **Issue:** Starlark's resolver binds free variables AT PARSE-OF-FILE TIME. The plan's task 2 design captured the mock_fn lambda directly via `tester.mock_action`, but the lambda body `lambda kwargs, attempt: ok(value={...})` failed to parse with `undefined: ok`. The plan's D5-C2 documentation about the "mock-lambda env" describes runtime resolution; without parse-time injection, the plan is unparseable.
- **Fix:** Added a new parser option `WithTestPredeclared(starlark.StringDict)` and a corresponding `Parser.testPredeclared` field; pkg/testing exports `MockLambdaParseTimeBuilders()` to expose the closure triple as a StringDict; pkg/cli/test.go (Plan 06) will wire both options together. Production parses (testMode=false) never see these names — the D1-20 invariant is preserved verbatim.
- **Files modified:** pkg/parser/options.go, pkg/parser/parser.go, pkg/parser/globals.go, pkg/testing/builders.go, pkg/testing/module_test.go (test helper updated to use the new option).
- **Verification:** All 12 module tests + 12 router tests pass. `TestTesterModule_ProductionParsePathUnchanged` pins production parse path. Cycle still broken: `grep -c '"github.com/mikelalcon/skytime/pkg/testing"' pkg/parser/*.go` → 0.
- **Committed in:** `970d0d6` (Task 2 commit).

**2. [Rule 1 - Bug] `TestRouter_MockEvalErr_NonRetryable` test rewrite**

- **Found during:** Task 3 (router tests).
- **Issue:** The plan suggested `fail("kaboom")` inside the mock body to exercise the EvalError surface. In test mode, `fail` resolves to the PARSE-time `builtinFail` (D4.2-05 dual-semantics) which returns `*dag.Fail` at execute time — the lambda return-type check fires D5-C4 ('mock must return ok/err/nonretryable') instead of the EvalError path.
- **Fix:** Rewrote the test to use `ok(value={"x": 1 // 0})`; integer division by zero raises a runtime Starlark *EvalError, exercising the intended router branch.
- **Files modified:** pkg/testing/router_test.go.
- **Verification:** `TestRouter_MockEvalErr_NonRetryable` passes; verifies err message mentions "zero"/"division" and wraps `extension.ErrNonRetryable`.
- **Committed in:** `d350e91` (Task 3 commit).

---

**Total deviations:** 2 auto-fixed (1 Rule 2 missing critical, 1 Rule 1 bug-fix in test).
**Impact on plan:** No scope creep; the WithTestPredeclared option is a Plan-02-scoped addition that makes the plan's design parseable while preserving all stated invariants. The test rewrite preserves the test's INTENT (exercising the EvalError surface) while picking a trigger that doesn't collide with D4.2-05 dual-semantics.

## Issues Encountered

None blocking.

The integration test `TestAttempts_IncrementOnRetry` (workflow-level retry → 3 attempts → final OkResult) is parked with `t.Skip("integration test activates after Plan 03 ships interpreter.RunOnceCapturing")` — plan-aligned deferral; per-attempt increment is unit-tested via `TestRouter_AttemptIncrementsPerCall` directly against `buildExecuteBatchCallback`.

## User Setup Required

None.

## Next Phase Readiness

- **Plan 03 (Replay determinism + divergence diff):** can lift `pkg/interpreter/replay_determinism_test.go::runOnceCapturing` into a public helper and consume `pkg/testing.WireMockCallback` to wire the mock router into both replay-twice runs.
- **Plan 04 (`tester.run` + assert.* wiring):** replaces `makeBuiltinTesterRunPlaceholder` with the real ExecuteWorkflow driver that:
  - reads `*WorkflowSpec` for `dag.WorkflowInput` + `StartWorkflowOptions`,
  - calls `WireMockCallback(env, buildExecuteBatchCallback(...))`,
  - uses Plan 03's RunOnceCapturing for replay determinism,
  - threads `starlarktest.SetReporter(thread, t)` for assert.* surfacing.
  - The `*WorkflowSpec.RetryPolicy` and `.Timeouts` fields are reserved (currently dropped on read); Plan 04 extends the struct.
- **Plan 06 (CLI subcommand):** pkg/cli/test.go imports pkg/testing and constructs the parser with `WithTestMode + WithTestModule(NewTesterModule wired to a fresh registry+spec) + WithTestPredeclared(MockLambdaParseTimeBuilders())`.

## Self-Check: PASSED

Verified file-existence and commit-presence for every claim in this Summary.

**Files created (verified via `[ -f path ]` after the Plan-02 commits):**

- `pkg/testing/builders.go` ✓
- `pkg/testing/builders_test.go` ✓
- `pkg/testing/attempts.go` ✓
- `pkg/testing/attempts_test.go` ✓
- `pkg/testing/module.go` ✓
- `pkg/testing/module_test.go` ✓
- `pkg/testing/builtin_workflow.go` ✓
- `pkg/testing/builtin_mock_action.go` ✓
- `pkg/testing/router.go` ✓
- `pkg/testing/router_test.go` ✓

**Files modified:**

- `pkg/parser/globals.go` ✓
- `pkg/parser/parser.go` ✓
- `pkg/parser/options.go` ✓

**Commits (verified via `git log --oneline | grep`):**

- `82c381b` feat(05-02): mock-lambda env (ok/err/nonretryable) + AttemptCounter ✓
- `970d0d6` feat(05-02): tester module + parser test-mode injection ✓
- `d350e91` feat(05-02): mock router callback wired against TestWorkflowEnvironment ✓

**Test gates:**

- `go test -race -count=1 ./pkg/testing/... ./pkg/parser/... ./tests/... ./pkg/activity/...` → all packages OK.
- `go vet ./pkg/testing/... ./pkg/parser/...` → clean.
- `gofmt -d` on touched files → clean.
- `TestPkgTesting_ImportsTestsuite` → no longer t.Skip; logs `verified pkg/testing imports go.temporal.io/sdk/testsuite (Phase 5 D5-firewall-q8 deviation honored)`.
- `TestPkgTesting_DoesNotImportSDKWorker` → PASS (no go.temporal.io/sdk/worker import).

---
*Phase: 05-tier-3-e2e-test-harness-temporal-test*
*Completed: 2026-05-05*
