---
phase: 05-tier-3-e2e-test-harness-temporal-test
verified: 2026-05-05T00:00:00Z
status: human_needed
score: 6/6 must-haves verified
human_verification:
  - test: "Run `skytime test <dir>` against an example with credentials/extensions and confirm the human-format output is suitable for consultant terminals (no log noise, readable PASS/FAIL block, clear callsite attribution on assertion failures)."
    expected: "Output mirrors `go test` output style; PASS/FAIL/SKIP lines align; FAIL detail lines are indented under their `--- FAIL:` header; per-file footer renders; final summary line clearly distinguishes pass-only vs failure runs."
    why_human: "Visual / UX quality and 'feels right' for consultant ergonomics; cannot be assessed programmatically."
  - test: "Verify exit-code documentation/behavior alignment: `skytime test` (no args) currently exits 1 due to cmd/skytime/main.go's blanket `os.Exit(1)`; docs/reference/cli.md and pkg/cli/test.go comments document exit 2 for usage error."
    expected: "Either main.go differentiates cobra-arg errors (exit 2) from RunE errors (exit 1), or the docs/comments are revised to state 'exit 1 for any error including usage'."
    why_human: "Minor doc/code mismatch; not a functional gap (skytime test still surfaces the cobra usage banner). Decision is stylistic + cross-subcommand (would affect every cobra command, not just test). Defer to maintainer call rather than auto-fix."
---

# Phase 5: Tier-3 E2E Test Harness Verification Report

**Phase Goal:** Build `pkg/testing` so consultants can write E2E tests in `.star` files: `tester.workflow`, `tester.mock_action`, and `tester.run` mock the single generic activity inside `testsuite.TestWorkflowEnvironment`, route per-action calls back to Starlark mock lambdas evaluating in the same restricted predeclared environment as production lambdas, and a replay helper runs each test twice and diffs Temporal event histories. `skytime test <dir>` is the discovery and runner entry point.

**Verified:** 2026-05-05
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (derived from ROADMAP success criteria + plan must_haves)

| #   | Truth                                                                                                                                                          | Status     | Evidence                                                                                                                                                                                          |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `tester.*` parse-time builtins (`workflow`, `mock_action`, `run`) are available only in test mode                                                              | ✓ VERIFIED | `pkg/testing/module.go::NewTesterModule` returns `*starlarkstruct.Module`; `pkg/parser/globals.go:92-99` injects via `p.testModuleBuilder` only when `p.testMode`; `pkg/parser/options.go::WithTestMode/WithTestModule` |
| 2   | Mock router intercepts `ExecuteBatch` in `testsuite.TestWorkflowEnvironment` and routes per-action calls back to Starlark mock lambdas in the restricted env  | ✓ VERIFIED | `pkg/testing/router.go::buildExecuteBatchCallback` (line 61) iterates batch, calls `reg.Match` (line 99) per action, invokes `evalMockLambda` (line 113) which `starlark.Call`s the captured `*dag.CapturedLambda` whose free vars (`ok`/`err`/`nonretryable`) are bound at parse time via the same predeclared dict as production lambdas (`MockLambdaGlobals = lambdaTimeGlobals ∪ {ok,err,nonretryable}`) |
| 3   | `attempt` is passed to mocks as an explicit argument; per-(flow,step,action_idx) counter increments per call                                                   | ✓ VERIFIED | `pkg/testing/attempts.go` defines `AttemptCounter` keyed by (FlowName, StepIdx, ActionIdx); `evalMockLambda` calls with `Tuple{augKwargs, MakeInt(attempt)}`. Substance pinned by `TestRouter_AttemptIncrementsPerCall`; integration `TestAttempts_IncrementOnRetry` deferred to Plan 06 (documented architectural skip) |
| 4   | Replay helper runs each test twice and diffs event histories; first-divergent-event reporter surfaces flow + test callsite                                    | ✓ VERIFIED | `pkg/interpreter/replay_helper.go::RunOnceCapturing` + `EventCapture`; `pkg/testing/replay.go::RunOnceCapturing` thin wrapper (line 38, calls `interpreter.RunOnceCapturing` line 54); `pkg/testing/replay_diff.go::FirstDivergentEvent` walks back to nearest `step_dispatch` reading `pos`/`name` KV; `pkg/interpreter/walk_step.go:37-45` emits `step_dispatch` with `name` + `pos`; `builtin_run.go:88-98` runs twice + reports divergence |
| 5   | `assert.*` builtins from `go.starlark.net/starlarktest` are wired; failures route to `*testing.T`                                                              | ✓ VERIFIED | `pkg/parser/globals.go:133` calls `starlarktest.LoadAssertModule()` when `p.testMode`; `pkg/testing/reporter.go::runOneTest` calls `starlarktest.SetReporter(thread, subT)` line 56                |
| 6   | `skytime test <dir>` discovers `*_test.star` files recursively, supports `--run`/`--format`, and reports pass/fail without Go stack frames                    | ✓ VERIFIED | `pkg/testing/discover.go::DiscoverTestFiles` (`filepath.WalkDir`, line 55); `pkg/testing/runner.go::WithRunFilter` + `WithFormat`; `pkg/testing/output_json.go::JSONEvent` (RFC3339Nano UTC); `pkg/cli/test.go::newTestCommand` + `pkg/cli/root.go:63 root.AddCommand(newTestCommand(cfg))`; `pkg/testing/cli_run.go::RunCLI`; `tests/skytime_test_e2e_test.go::TestSkytimeTestE2E_FailureExitNonzero` asserts no `goroutine`, `runtime.`, or `.go:` strings |

**Score:** 6/6 truths verified.

### Required Artifacts

| Artifact                                | Plan | Status      | Details                                                                                                       |
| --------------------------------------- | ---- | ----------- | ------------------------------------------------------------------------------------------------------------- |
| `pkg/testing/registry.go`               | 01   | ✓ VERIFIED | `MockEntry`, `Frame`, `MockRegistry`, `PushTestFrame`, `PopTestFrame`, `Add`, `Match` all present              |
| `pkg/testing/output.go`                 | 01   | ✓ VERIFIED | `MockOperationOutput` + `MarshalJSON` + `IsOperationOutput` + compile-time assert `_ dag.OperationOutput`     |
| `pkg/parser/options.go`                 | 01   | ✓ VERIFIED | `WithTestMode()` + `WithTestModule(builderFn)` with nil-guard                                                  |
| `tests/firewall_testsuite_test.go`      | 01   | ✓ VERIFIED | `TestPkgTesting_ImportsTestsuite` non-vacuous; activated (no longer skipping after Plan 02)                     |
| `pkg/testing/module.go`                 | 02   | ✓ VERIFIED | `NewTesterModule(reg, ws)` returns `*starlarkstruct.Module` with workflow/mock_action/run                      |
| `pkg/testing/builders.go`               | 02   | ✓ VERIFIED | `MockLambdaGlobals()` returns lambdaTimeGlobals ∪ {ok, err, nonretryable}                                      |
| `pkg/testing/router.go`                 | 02   | ✓ VERIFIED | `buildExecuteBatchCallback`, `evalMockLambda`, "no mock for ..." verbatim error format                         |
| `pkg/testing/attempts.go`               | 02   | ✓ VERIFIED | `AttemptCounter` type with `NextFor(ref) → int`                                                                |
| `pkg/parser/globals.go`                 | 02   | ✓ VERIFIED | `if p.testMode` branch injects tester module; Plan 04 added starlarktest assert injection                       |
| `pkg/interpreter/walk_step.go`          | 03   | ✓ VERIFIED | `step_dispatch` log emits `name` (line 42) + `pos` (line 45)                                                   |
| `pkg/interpreter/replay_helper.go`      | 03   | ✓ VERIFIED | `RunOnceCapturing` exported + `EventCapture` struct + `SetOnActivityStartedListener`/`Completed` wired         |
| `pkg/testing/replay.go`                 | 03   | ✓ VERIFIED | Wraps `interpreter.RunOnceCapturing` (line 54)                                                                  |
| `pkg/testing/replay_diff.go`            | 03   | ✓ VERIFIED | `Divergence` type + `FirstDivergentEvent`; walks backward for nearest `step_dispatch` (line 190)                |
| `pkg/testing/reporter.go`               | 04   | ✓ VERIFIED | `runOneTest` calls `starlarktest.SetReporter(thread, subT)` line 56                                             |
| `pkg/testing/builtin_run.go`            | 04   | ✓ VERIFIED | `makeBuiltinTesterRun` runs `RunOnceCapturing` twice (lines 88, 92) + `FirstDivergentEvent` (line 98); Pitfall 4 file-scope rejection (line 50) |
| `pkg/testing/runner.go`                 | 04/05 | ✓ VERIFIED | `Run(t *testing.T, dir, opts...)`; `WithRunFilter`/`WithFormat`; PASS/FAIL/SKIP human format (lines 265, 275, 285); per-file footer (lines 294, 296) |
| `pkg/testing/discover.go`               | 05   | ✓ VERIFIED | `DiscoverTestFiles` (`filepath.WalkDir`) + `DiscoverTests` (Starlark module enumeration) + `CompileRunFilter`   |
| `pkg/testing/output_json.go`            | 05   | ✓ VERIFIED | `JSONEvent` struct with Time/Action/Package/Test/Elapsed/Output; RFC3339Nano UTC (line 48)                      |
| `pkg/testing/cli_run.go`                | 06   | ✓ VERIFIED | `RunCLI(dir, opts...) (passed, failed int, err error)` non-*testing.T entry-point                              |
| `pkg/cli/test.go`                       | 06   | ✓ VERIFIED | `Use: "test <dir>"`, `cobra.ExactArgs(1)`, calls `testingpkg.RunCLI` line 59, exit-code mapping in RunE        |
| `tests/skytime_test_e2e_test.go`        | 06   | ✓ VERIFIED | `TestSkytimeTestE2E_HappyPath`, `_FailureExitNonzero` (asserts no `goroutine`/`runtime.`/`.go:`), `_JSONFormat` |
| `docs/for-flow-authors/testing.md`      | 06   | ✓ VERIFIED | H2 `## tester.workflow` (line 62), `## tester.mock_action` (103), `## tester.run` (239), `## assert.*` (271)    |
| `docs/reference/cli.md`                 | 06   | ✓ VERIFIED | `## skytime test` section (line 214) with all 6 H3s: Synopsis, Motivation, Flags, Exit Codes, Example, See Also |

24 of 24 plan-declared artifacts verified at Levels 1-3.

### Key Link Verification

| From                                       | To                                                            | Status     | Evidence                                                                                                            |
| ------------------------------------------ | ------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------- |
| `pkg/parser/parser.go`                     | `Parser.testMode` + `Parser.testModuleBuilder`                | ✓ WIRED    | parser.go:97-98 (fields); parser.go:321 `if p.testMode`; options.go:86 sets it; options.go:101-106 binds builder    |
| `pkg/activity/firewall_test.go`            | `allowedPkgs` includes "testing"                              | ✓ WIRED    | firewall_test.go:46 `allowedPkgs := []string{"activity","interpreter","worker","cli","testing"}`                    |
| `pkg/testing/router.go`                    | `pkg/testing/registry.go::Match`                              | ✓ WIRED    | router.go:99 `entry, found := reg.Match(ref, kwargsAsStr)`                                                           |
| `pkg/testing/router.go`                    | mock-lambda call (kwargs, attempt) — production env reuse     | ✓ WIRED    | router.go:229 `starlark.Call(thread, captured.Fn, Tuple{augKwargs, MakeInt(attempt)}, nil)`. (Detail-level deviation: uses `starlark.Call` directly rather than `bridge.CallLambda`; substance — restricted env via parse-time-bound free vars + augmented kwargs — is preserved per Plan 02 deviation D5-bridge-myth-q4 chain) |
| `pkg/parser/globals.go`                    | `p.testModuleBuilder(p, thread)`                              | ✓ WIRED    | globals.go:99 `g["tester"] = p.testModuleBuilder(p, thread)`                                                         |
| `pkg/testing/replay.go`                    | `pkg/interpreter/replay_helper.go::RunOnceCapturing`          | ✓ WIRED    | replay.go:54 `return interpreter.RunOnceCapturing(parsed, hash, init, cb)`                                           |
| `pkg/interpreter/replay_helper.go`         | `testsuite.SetOnActivityStartedListener/CompletedListener`    | ✓ WIRED    | replay_helper.go:108/128 callbacks; verified by gsd-tools key-link check                                             |
| `pkg/testing/replay_diff.go`               | `step_dispatch` event's pos/name KV                           | ✓ WIRED    | replay_diff.go:190 `if !kvEquals(r.KV, "event", "step_dispatch")` walks backward                                     |
| `pkg/parser/globals.go`                    | `starlarktest.LoadAssertModule()`                             | ✓ WIRED    | globals.go:133 `assertGlobals, assertErr := starlarktest.LoadAssertModule()`                                         |
| `pkg/testing/builtin_run.go`               | `RunOnceCapturing × 2 + FirstDivergentEvent`                  | ✓ WIRED    | builtin_run.go:88+92 (twice), :98 `FirstDivergentEvent`                                                              |
| `pkg/testing/runner.go`                    | `pkg/testing/reporter.go::runOneTest`                         | ✓ WIRED    | runner.go:252 `runOneTest(rep, fn, tests.Reg, tests.WS)`                                                             |
| `pkg/cli/test.go::RunE`                    | `pkg/testing.RunCLI`                                          | ✓ WIRED    | test.go:59 `passed, failed, err := testingpkg.RunCLI(dir, opts...)`                                                  |
| `pkg/cli/root.go::NewRootCommand`          | `pkg/cli/test.go::newTestCommand`                             | ✓ WIRED    | root.go:63 `root.AddCommand(newTestCommand(cfg)) // Phase 5 Plan 06`                                                 |
| `tests/skytime_test_e2e_test.go`           | `exec.Command(bin, "test", dir)`                              | ✓ WIRED    | skytime_test_e2e_test.go:73, :93, :120 — three subprocess invocations against freshly-built binary                   |

(Note: gsd-tools key-link checker reported many false negatives due to regex-escape interpretation in YAML-string patterns and `::` notation in source paths. Each link verified manually with grep; substance confirmed in every case.)

### Data-Flow Trace (Level 4)

The phase delivers a test harness — runtime "data flow" is the test event stream traversal. Verified by:

| Artifact                          | Data Variable          | Source                                                          | Produces Real Data | Status     |
| --------------------------------- | ---------------------- | --------------------------------------------------------------- | ------------------ | ---------- |
| `pkg/testing/router.go`           | `mockResult`           | `evalMockLambda` → `starlark.Call(captured.Fn, ...)`            | Yes — real Starlark eval against parse-time-captured lambda; exercised by `TestRouter_AttemptIncrementsPerCall` and `TestRouter_*` family in router_test.go (16 KB of router tests) | ✓ FLOWING  |
| `pkg/testing/replay.go`           | `EventCapture` records | `interpreter.RunOnceCapturing` → testsuite.TestWorkflowEnvironment | Yes — non-empty event stream with step_dispatch + activity_started + activity_completed records, verified by `replay_test.go` and replay_diff_test.go | ✓ FLOWING  |
| `pkg/testing/runner.go`           | `passed`/`failed`      | per-test `t.Run` driver / `subT.Failed()`                        | Yes — exercised by runner_test.go and runner_format_test.go end-to-end | ✓ FLOWING  |
| `pkg/cli/test.go::RunE`           | `passed, failed, err`  | `pkg/testing.RunCLI`                                            | Yes — confirmed by E2E TestSkytimeTestE2E_HappyPath (exit 0 + `--- PASS:` lines) and TestSkytimeTestE2E_FailureExitNonzero (exit 1 + `--- FAIL:` lines + no Go stack frames) | ✓ FLOWING  |

### Behavioral Spot-Checks

| Behavior                                                       | Command                                                            | Result                                  | Status |
| -------------------------------------------------------------- | ------------------------------------------------------------------ | --------------------------------------- | ------ |
| Phase 5 test suite race-clean                                  | `go test -race -count=1 ./pkg/testing/... ./pkg/cli/... ./tests/...` | All packages `ok`; pkg/testing 1.484s, pkg/cli 9.415s, tests 6.280s | ✓ PASS |
| Whole-module vet clean                                         | `go vet ./...`                                                     | No output (clean)                       | ✓ PASS |
| Replay-determinism path tests pass                             | `go test -race -run "TestReplay\|TestRunOnceCapturing\|TestFirstDivergent\|TestEventCapture" ./pkg/testing/... ./pkg/interpreter/...` | All `ok`                                | ✓ PASS |
| Docgen drift gate stays green (Plan 06 dropped docgen integration) | `go test -count=1 -run "TestDocgenDrift" ./tests/...`              | `ok`                                    | ✓ PASS |
| skytime test --help reachable                                  | `go build -o /tmp/skytime_verify ./cmd/skytime && /tmp/skytime_verify test --help` | Synopsis + Flags + Long description rendered | ✓ PASS |
| skytime test (no args) surfaces cobra usage banner             | `/tmp/skytime_verify test`                                         | "Error: accepts 1 arg(s), received 0" + usage; exit code 1 (see Human Verification #2) | ⚠ MINOR — see Human Verification |

### Requirements Coverage

| Requirement | Source Plan(s)            | Description                                                                                                                      | Status      | Evidence                                                                                                                                                         |
| ----------- | ------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| TEST-01     | 01, 02, 04, 05, 06        | `temporal_test` Starlark builtin module exposes `tester.workflow/mock_action/run` from `.star` test files                        | ✓ SATISFIED | `pkg/testing/module.go::NewTesterModule` registers all three; gated by `p.testMode` so production `.star` files never see these names                            |
| TEST-02     | 01, 02, 04, 06            | Mock function executes in same restricted predeclared env as production lambdas; bridge intercepts ExecuteBatch in TestWorkflowEnvironment | ✓ SATISFIED | `MockLambdaGlobals()` reuses `bridge.LambdaTimeGlobals()` ∪ {ok,err,nonretryable}; `buildExecuteBatchCallback` passed into `env.OnActivity(...).Return(...)` via `interpreter.RunOnceCapturing` |
| TEST-03     | 02, 03, 04, 06            | `attempt` count passed to mocks as explicit arg; per-(flow,step,action_idx) counter                                              | ✓ SATISFIED | `pkg/testing/attempts.go::AttemptCounter`; substance pinned by `TestRouter_AttemptIncrementsPerCall`. Integration `TestAttempts_IncrementOnRetry` deferred (Plan 04 architectural skip) — documented deviation, not a gap |
| TEST-04     | 03, 04, 06                | Replay helper runs each test twice and diffs Temporal event history; divergence fails the test                                   | ✓ SATISFIED | `pkg/testing/builtin_run.go::makeBuiltinTesterRun` runs `RunOnceCapturing` twice (lines 88, 92), calls `FirstDivergentEvent` (line 98), reports via `starlarktest.Reporter` |
| TEST-05     | 04, 06                    | `assert.*` builtins from `go.starlark.net/starlarktest` available; failures report into `*testing.T`                             | ✓ SATISFIED | `pkg/parser/globals.go:133 starlarktest.LoadAssertModule()`; `pkg/testing/reporter.go:56 starlarktest.SetReporter(thread, subT)`                                  |
| CLI-03      | 05, 06                    | `skytime test <dir>` discovers `.star` test files, runs the Tier 3 harness, reports pass/fail with Starlark callsite errors      | ✓ SATISFIED | `pkg/cli/test.go::newTestCommand` wired into `pkg/cli/root.go:63`; subprocess E2E `TestSkytimeTestE2E_*` covers happy-path, failure exit, JSON format, no-Go-stack-traces |

All 6 phase requirements present in plan frontmatters; all marked `[x] Complete` in REQUIREMENTS.md; no orphans (every ID claimed by ≥1 plan).

### Anti-Patterns Found

| File                                | Line | Pattern                                                  | Severity    | Impact                                                                                                                                       |
| ----------------------------------- | ---- | -------------------------------------------------------- | ----------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `pkg/testing/builtin_run.go`        | 29   | "replaces Plan 02's placeholder"                         | ℹ Info     | Historical docstring noting prior placeholder is now replaced; no active stub. The function body actually invokes `RunOnceCapturing × 2 + FirstDivergentEvent`. |
| `pkg/testing/router_test.go`        | 303  | `t.Skip("Plan 06 e2e activates this with the bundled http extension; ...")` for `TestAttempts_IncrementOnRetry` | ⚠ Documented deviation | Forward-pointing skip is a documented architectural finding (Plan 04 SUMMARY). Substance is pinned by sibling unit `TestRouter_AttemptIncrementsPerCall`. Not a gap per plan-aligned deferral; flagged here for traceability. |

No TODO/FIXME/XXX/HACK comments in production code paths. No `return null/empty` stubs in plan-modified files. No emptied prop hardcoding.

### Human Verification Required

1. **Visual UX of `skytime test` human-format output.**
   - Test: Run `skytime test <dir>` against an example with credentials/extensions and confirm the human-format output is suitable for consultant terminals.
   - Expected: Output mirrors `go test`-style; PASS/FAIL/SKIP lines align; FAIL detail lines are indented under their `--- FAIL:` header; per-file footer renders; final summary line clearly distinguishes pass-only vs failure runs.
   - Why human: Visual / UX quality and "feels right" for consultant ergonomics; cannot be assessed programmatically.

2. **Exit-code documentation/behavior alignment for usage errors.**
   - Test: `/tmp/skytime_verify test` (no positional arg).
   - Observed: Cobra prints `Error: accepts 1 arg(s), received 0` + usage banner; process exits with code 1 (because `cmd/skytime/main.go::main` does a blanket `os.Exit(1)` on any cobra error).
   - Documented: `pkg/cli/test.go` doc-comment and `docs/reference/cli.md` both state usage error → exit 2.
   - Expected resolution: Either main.go differentiates cobra-arg errors (exit 2) from RunE errors (exit 1), or the docs/comments are revised to state "exit 1 for any error including usage".
   - Why human: Minor doc/code mismatch; not a functional gap (skytime test still surfaces the cobra usage banner). Decision is stylistic + cross-subcommand (would affect every cobra command, not just test). Defer to maintainer call rather than auto-fix.

### Gaps Summary

No goal-blocking gaps. Phase 5 delivers all six observable truths against ROADMAP success criteria. All 24 plan-declared artifacts exist, are substantive, and are wired. All requirement IDs (TEST-01..05, CLI-03) are satisfied with code-level evidence. Two deviations were declared upfront and resolved as planned:

- D5-firewall-q8 (firewall scope expansion for pkg/testing): consummated in `pkg/activity/firewall_test.go::allowedPkgs` and `tests/firewall_testsuite_test.go`.
- D5-bridge-myth-q4 (no `bridge.StructFromDict`): router uses `bridge.FromStarlarkValue` + `MockOperationOutput` wrapping, no new bridge export, exactly as planned.
- D5-runner-q7 (foundation `pkgtesting.Run(t,dir,opts...)`): shipped; CLI wraps via `pkg/testing.RunCLI` per D5-runner-cli-adapter.
- D5-docs-builtins-marker-location (docgen integration deferred): testing.md is the manual source of truth; docgen drift gate stays green (verified above).
- Plan 04 architectural skip on `TestAttempts_IncrementOnRetry`: documented; substance pinned by `TestRouter_AttemptIncrementsPerCall`.

Per the Phase 4 verification convention precedent (when all automated checks pass and only manual TTY/UX testing remains, status is `human_needed` with specific manual items), this verification reports `status: human_needed` with the two manual items above. The items are quality-of-life concerns (visual UX, exit-code-2 doc/code consistency) — neither is goal-blocking, but both warrant a human pass before declaring the phase fully closed.

---

_Verified: 2026-05-05_
_Verifier: Claude (gsd-verifier)_
