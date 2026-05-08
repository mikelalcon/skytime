---
phase: 5
slug: tier-3-e2e-test-harness-temporal-test
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-05
---

# Phase 5 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source of truth for the test pyramid, requirements → tests map, and Wave 0 gaps:
> [`05-RESEARCH.md` § Investigation 12 / § Validation Architecture](./05-RESEARCH.md).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `testify/{require,assert}` v1.11.1 (existing) |
| **Config file** | none — Go stdlib auto-discovers `*_test.go`; `*_test.star` discovery is the harness's own concern |
| **Quick run command** | `go test -race -count=1 ./pkg/testing/...` |
| **Full suite command** | `go test -race -count=1 ./...` |
| **Estimated runtime** | ~30s for `pkg/testing/...` quick run; ~2min full suite (per recent green CI) |

---

## Sampling Rate

- **After every task commit:** Run `go test -race -count=1 ./pkg/testing/...` (quick) — narrowed to packages the task touched.
- **After every plan wave:** Run `go test -race -count=1 ./...` — full module.
- **Before `/gsd:verify-work`:** Full suite must be green AND all firewall tests (`tests/firewall_*.go`, `pkg/activity/firewall_test.go`) green.
- **Max feedback latency:** ~30s for `pkg/testing/...`; ~2min for full module.

---

## Per-Task Verification Map

> **Plans for this phase have not been written yet. This map is filled by the planner during Step 8.**
> Once `05-NN-*-PLAN.md` files exist, each `<task>` cites its `<automated>` test command, and that command is rolled into this table.

Source mapping (research-side, by Phase Requirement):

| Req ID | Behavior | Test Type | Automated Command | File Exists |
|--------|----------|-----------|-------------------|-------------|
| TEST-01 | `tester.workflow/mock_action/run` registered as parse-time globals in test mode | Integration | `go test -race ./pkg/testing -run TestTesterModule_RegistersBuiltins` | ❌ Wave 0 |
| TEST-01 | Calling `tester.run` outside `def test_*` is rejected | Unit | `go test -race ./pkg/testing -run TestTesterRun_OutsideDefTest_RejectsAtParse` | ❌ Wave 0 |
| TEST-02 | Mock callback receives `[]*dag.ActionRef` and routes to matching mock lambda | Integration | `go test -race ./pkg/testing -run TestRouter_DispatchesToMatchingMockLambda` | ❌ Wave 1 |
| TEST-02 | Mock-lambda env = `lambdaTimeGlobals` ∪ {ok, err, nonretryable}; production env unchanged | Unit | `go test -race ./pkg/testing -run TestMockLambdaEnv_IsLambdaTimePlusBuilders` | ❌ Wave 1 |
| TEST-02 | No-mock-found returns `extension.ErrNonRetryable` with flow callsite + step name | Integration | `go test -race ./pkg/testing -run TestRouter_NoMockFound_FailsFast` | ❌ Wave 1 |
| TEST-02 | 3-tier match precedence resolves correctly (tier 1 always wins; recency within tier) | Unit | `go test -race ./pkg/testing -run TestRegistry_TierPrecedence_TestVectors` | ❌ Wave 0 |
| TEST-03 | `attempt` is 1 on first call, 2 on retry, 3 on third retry under Temporal RetryPolicy | Integration | `go test -race ./pkg/testing -run TestAttempts_IncrementOnRetry` | ❌ Wave 1 |
| TEST-04 | Two consecutive `tester.run` calls produce byte-equal event sequences | Integration | `go test -race ./pkg/testing -run TestReplay_DeterministicEventSequence` | ❌ Wave 2 |
| TEST-04 | Replay divergence reports first-divergent event with payload before/after; flow + test callsite | Integration | `go test -race ./pkg/testing -run TestReplay_DivergenceReportFormat` | ❌ Wave 2 |
| TEST-05 | `assert.eq("octocat", actual)` failure surfaces in `*testing.T.Error` with Starlark file:line:col | Integration | `go test -race ./pkg/testing -run TestAssert_FailureSurfacesInSubtestT` | ❌ Wave 2 |
| TEST-05 | Multiple `assert.*` failures in one `def test_*` accumulate (library default) | Integration | `go test -race ./pkg/testing -run TestAssert_AccumulatesMultipleFailuresInSubtest` | ❌ Wave 2 |
| CLI-03 | `skytime test <dir>` discovers `*_test.star`, runs harness, exits 0 on all-pass | E2E | `go test -race ./tests -run TestSkytimeTestE2E_HappyPath` | ❌ Wave 4 |
| CLI-03 | `skytime test <dir>` exits 1 on any-fail | E2E | `go test -race ./tests -run TestSkytimeTestE2E_FailureExitNonzero` | ❌ Wave 4 |
| CLI-03 | `skytime test --run 'users_test\.test_existing'` filters tests | Integration | `go test -race ./pkg/cli -run TestTestCommand_RunFilter` | ❌ Wave 4 |
| CLI-03 | `skytime test --format=json` emits `go test -json`-compatible records | E2E | `go test -race ./tests -run TestSkytimeTestE2E_JSONFormat` | ❌ Wave 4 |
| CLI-03 | Default output has NO Go stack traces | Integration | `go test -race ./pkg/cli -run TestTestCommand_DefaultOutput_NoGoStackTraces` | ❌ Wave 4 |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky — populated per task once plans land.*

---

## Wave 0 Requirements

> Stubs / scaffolding so each task has a concrete file to add tests to. The planner refines these once plans exist.

- [ ] `pkg/testing/doc.go` — package overview + parse/execute split note
- [ ] `pkg/testing/registry.go` + `pkg/testing/registry_test.go` — `MockRegistry`, `Frame`, `PushTestFrame`, `PopTestFrame`, `Match`, `Add`, 3-tier ladder, regex compile-at-registration
- [ ] `pkg/testing/output.go` + `pkg/testing/output_test.go` — `MockOperationOutput` (custom JSON marshaller; round-trip with `RawOperationOutput`)
- [ ] `pkg/parser/options.go` extension — `WithTestMode()`, `WithTestModule(...)` parser options
- [ ] `pkg/parser/parser.go` extension — `Parser.testMode`, `Parser.testGlobals` fields
- [ ] `tests/firewall_testsuite_test.go` — non-vacuous meta-test that `pkg/testing` is allowed `go.temporal.io/sdk/testsuite`; `pkg/activity/firewall_test.go` allow-list updated
- [ ] Framework install: NONE — all dependencies (`go.temporal.io/sdk/testsuite`, `go.starlark.net/starlarktest`) already in `go.mod`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| TTY output formatting (color, width, line-redraw) on a real terminal | CLI-03 (D5-E1) | Static line-per-test format (D5-E1) targets CI piping; visually scanning a real TTY catches truncation/wrapping issues a string-equality test can't | Run `skytime test examples/skeleton-tests/` in a real terminal; eyeball the `--- PASS / --- FAIL` lines, indented assertion detail under FAIL, per-file summary, final summary line |
| Help-text wording (`skytime test --help`) reads naturally | CLI-03 | Style/clarity is subjective; not an assertion target | Run `skytime test --help`; confirm flag descriptions are present and read naturally |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
