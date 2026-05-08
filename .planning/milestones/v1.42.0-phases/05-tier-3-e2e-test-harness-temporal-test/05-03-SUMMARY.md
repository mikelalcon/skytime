---
phase: 05-tier-3-e2e-test-harness-temporal-test
plan: 03
subsystem: testing
tags: [starlark, temporal, testsuite, replay-determinism, divergence-diff, event-capture, log-logger]

# Dependency graph
requires:
  - phase: 05-tier-3-e2e-test-harness-temporal-test
    provides: "MockRegistry + MockOperationOutput (Plan 01); buildExecuteBatchCallback + WireMockCallback + AttemptCounter + StepIndexLookup (Plan 02); WithTestPredeclared parser option (Plan 02)"
  - phase: 04.2-if-cond-as-expression-with-strict-equality-result-binding
    provides: "pkg/interpreter/replay_determinism_test.go::runOnceCapturing — the lift target for the public RunOnceCapturing"
  - phase: 03-lambda-serialization-decision-interpreter-worker
    provides: "ParsedFlow + FlowRegistry + NewWorkflow — RunOnceCapturing reuses these unchanged at execute time"
provides:
  - "interpreter.RunOnceCapturing(parsed, hash, init, mockCallback) — public lift of the file-private helper; mockCallback==nil supported for activity-free flows"
  - "interpreter.EventCapture (satisfies temporal log.Logger; concurrency-safe Snapshot/Serialize) + EventRecord struct (Kind ∈ {slog, activity_started, activity_completed})"
  - "Activity-boundary listeners auto-attached: SetOnActivityStartedListener / SetOnActivityCompletedListener route into the same capture buffer alongside slog events (Investigation 2 substitute for the missing GetWorkflowHistory)"
  - "step_dispatch event extended with `pos` (syntax.Position) + `name` (string) KV pairs — D5-D3 prerequisite for flow-callsite attribution"
  - "pkg/testing.RunOnceCapturing thin wrapper threading buildExecuteBatchCallback into interpreter.RunOnceCapturing"
  - "pkg/testing.FirstDivergentEvent + Divergence + Divergence.Format() — D5-D2 first-divergent-event report with verbatim multi-line shape"
  - "lookupOriginatingStep — backward walk from the divergent record reading step_dispatch.pos+name for D5-D3 attribution"
  - "helperParseProductionFlow — concrete (parser-driven) helper for assembling *interpreter.ParsedFlow + content hash from a .star source string"
affects: [05-04-tester-run-driver, 05-05-discovery-runner, 05-06-firewall-e2e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "EventCapture as the public log.Logger implementation: workflow.GetLogger(ctx).Info routes through testsuite.SetLogger → EventCapture; activity-boundary listeners append to the same buffer so the diff helper sees a single ordered stream"
    - "Per-record serialization for diffing: Kind|Level|Message|sorted-kvs (slog) / Kind|...|kind1(kwargs1),kind2(kwargs2) (activity_started) / Kind|...|n=N|err=... (activity_completed); KV pairs sorted alphabetically per record to neutralize Go map iteration randomness"
    - "lookupOriginatingStep backward-walk: reads pos+name from the nearest preceding step_dispatch event's KV slice — Plan 03 Task 0 made this mechanically possible by extending the emission"
    - "Forward-pointing skip with named successor plan: TestAttempts_IncrementOnRetry now points at Plan 04 Task 3 (was generic 'Plan 03' in Plan 02); preserves the must_haves contract while allowing the integration body to land where it belongs"

key-files:
  created:
    - pkg/interpreter/replay_helper.go
    - pkg/interpreter/replay_helper_test.go
    - pkg/testing/replay.go
    - pkg/testing/replay_test.go
    - pkg/testing/replay_diff.go
    - pkg/testing/replay_diff_test.go
  modified:
    - pkg/interpreter/walk_step.go
    - pkg/interpreter/walk_step_actionfn_test.go
    - pkg/testing/router_test.go

key-decisions:
  - "Lift target: interpreter.RunOnceCapturing (Option A, RESEARCH Open Q1). Implemented as a NEW non-test file (replay_helper.go) — the existing private runOnceCapturing in replay_determinism_test.go is left UNTOUCHED. Migrating internal callers would have churn-cascaded the eventCapturingLogger struct's call sites for zero functional gain; the new public surface is for downstream consumers (Plan 04's tester.run + Plan 03 Task 2's diff helper)."
  - "EventCapture is its own log.Logger implementation, distinct from the existing eventCapturingLogger in test_helpers_test.go. The two coexist cleanly: legacy internal Phase 04.2 tests use eventCapturingLogger.snapshot()→capturedRecord{Attrs map}, public Plan 03 consumers use EventCapture.Snapshot()→EventRecord{KV flat-slice}. Different shapes serve different needs (legacy tests want map-by-key access; the diff helper wants ordered KV for stable serialization)."
  - "Per-record serialization for the diff helper sorts KV pairs alphabetically. Go map iteration is randomized; without sorting two byte-equal-event-stream runs would produce different serialized strings → false-positive divergence. The slog producer side does NOT need to sort — the consumer side does."
  - "writeSortedKwargs uses fmt %v on the *starlark.Dict directly. Starlark *Dict.String() emits a stable {k1:v1,k2:v2} form in insertion order per the language contract (documented in Plan 04.1-05a). For replay-determinism diffing structural equality is sufficient; deep parse would buy nothing."
  - "Activity-boundary listeners decode args via converter.EncodedValues.Get(&[]*dag.ActionRef) and converter.EncodedValue.Get(&dag.ActionResults). EncodedValue.HasValue() is checked before Get to handle the err-nil-result completion case where result may be nil but the listener still fires."
  - "TestAttempts_IncrementOnRetry remains a t.Skip() with a forward-pointer naming Plan 04 (was 'Plan 03' in Plan 02). Plan 03 ships the RunOnceCapturing scaffolding the test depends on, but the runner-driven path (a real flow + fake gh extension) lives in Plan 04. The substance of replay-determinism + divergence reporting is exercised by FOUR named tests across pkg/interpreter and pkg/testing; the integration test will activate in Plan 04 with the full fake-extension wiring."
  - "writeSortedKwargs initial sketch had a vestigial 'kwIter' interface placeholder — removed in the same task before commit (the inline %v rendering is sufficient and self-explanatory). No commit-history noise."

patterns-established:
  - "Public-helper lift WITHOUT touching the private original: when an internal _test.go helper must become a public API, drop a NEW non-test file that exports the public shape and leave the private original intact for the existing tests. Cuts churn radically; the two implementations share semantics but not symbols."
  - "Activity-boundary capture sans GetWorkflowHistory: testsuite.TestWorkflowEnvironment exposes SetOnActivityStartedListener / SetOnActivityCompletedListener pairs as the canonical workaround for the missing history accessor. Plan 03's EventCapture pattern is the template for any future Phase 5/6 capture seam."
  - "step_dispatch.pos + step_dispatch.name as the universal flow-callsite attribution channel: any future divergence reporter, error annotator, or live-renderer enrichment that needs 'which originating step emitted this event' walks backward to the nearest step_dispatch and reads the two KV pairs."
  - "Substantive forward-pointing skips: t.Skip messages name the SUCCESSOR plan AND task explicitly ('Plan 04 Task 3 wires tester.run end-to-end'), giving anyone running grep on the test name a one-line breadcrumb to the real owner."

requirements-completed: [TEST-03, TEST-04]

# Metrics
duration: 9min
completed: 2026-05-05
---

# Phase 5 Plan 03: Replay determinism + divergence reporter Summary

**`interpreter.RunOnceCapturing` lifted public with EventCapture (slog + activity-boundary records via SetOnActivityStartedListener/CompletedListener); `pkg/testing.RunOnceCapturing` thin wrapper threading the Plan 02 mock router; `FirstDivergentEvent` + `Divergence.Format()` produce the verbatim D5-D2 multi-line report with D5-D3 flow-callsite attribution sourced from a step_dispatch event extension (`pos`+`name` KV pairs).**

## Performance

- **Duration:** ~9 min
- **Started:** 2026-05-05T19:40:16Z
- **Completed:** 2026-05-05T19:49:29Z
- **Tasks:** 3 (all atomic; TDD red-green for Tasks 0+1)
- **Files created:** 6
- **Files modified:** 3

## Accomplishments

- **Task 0 (D5-D3 prerequisite):** `pkg/interpreter/walk_step.go`'s `step_dispatch` event now emits `pos` (syntax.Position) and `name` (string, equals the resolved label) KV pairs alongside the existing `kind/label/idx/total/path`. Verified by new `TestWalkStep_StepDispatch_CarriesPosAndName` (asserts both keys land in captured records and `pos.Filename()` matches the originating step's filename).
- **Task 1 (RunOnceCapturing lift):** `pkg/interpreter/replay_helper.go` exposes `EventCapture` (concurrency-safe; satisfies temporal `log.Logger`) and `RunOnceCapturing(parsed, hash, init, mockCallback)`. Activity-boundary listeners attach automatically; `mockCallback=nil` supported for activity-free flows. `Serialize()` emits a stable per-record line with KV pairs alphabetically sorted. New `TestReplay_DeterministicEventSequence` (TEST-04 baseline) + 3 sibling unit tests pass.
- **Task 2 (replay-diff helper):** `pkg/testing/replay.go` thin wrapper; `pkg/testing/replay_diff.go` ships `Divergence` struct, `FirstDivergentEvent`, `Divergence.Format()`, and `lookupOriginatingStep` (backward walk for the nearest preceding `step_dispatch` reading `pos`+`name`). 5 new named tests cover identical streams, structural divergence, payload divergence, the verbatim D5-D2 message format with D5-D3 flow-callsite assertions, and an end-to-end no-divergence integration on a real RunOnceCapturing pair.
- **TestAttempts_IncrementOnRetry** rewrite: skip message now points forward at Plan 04 Task 3 (the runner-driven path with a fake `gh` extension), per the plan's scope-split convention. Plan 03 ships the scaffolding; Plan 04 lands the integration body.
- **Phase 04.2 internal replay tests** (`pkg/interpreter/replay_determinism_test.go`) untouched and still passing — the existing private `runOnceCapturing` + `eventCapturingLogger` continue to serve internal Phase 04.2 fixture coverage; the new public surface coexists cleanly.

## Task Commits

Each task committed atomically:

1. **Task 0: step_dispatch event extended with pos+name attrs (D5-D3 prereq)** — `9490eee` (feat)
2. **Task 1: lift runOnceCapturing → public interpreter.RunOnceCapturing** — `7fb15f3` (feat)
3. **Task 2: pkg/testing.RunOnceCapturing wrapper + replay-diff reporter** — `44c5055` (feat)

## Files Created/Modified

**Created:**

- `pkg/interpreter/replay_helper.go` — `EventCapture` struct + `EventRecord` shape + `Snapshot/Serialize` accessors + `onActivityStarted/onActivityCompleted` callbacks + `RunOnceCapturing(parsed, hash, init, mockCallback)` exported function
- `pkg/interpreter/replay_helper_test.go` — `TestReplay_DeterministicEventSequence`, `TestRunOnceCapturing_NilCallback_BackwardCompat`, `TestRunOnceCapturing_NilParsed`, `TestEventCapture_LogLogger`
- `pkg/testing/replay.go` — `RunOnceCapturing` thin wrapper threading `buildExecuteBatchCallback` into `interpreter.RunOnceCapturing`
- `pkg/testing/replay_test.go` — `TestReplay_RunOnceCapturing_NoActivities`, `TestReplay_RunOnceCapturing_TwoRunsByteEqual`, `helperParseProductionFlow` (concrete; sha256-hex hash; real parser session)
- `pkg/testing/replay_diff.go` — `Divergence` struct + `FirstDivergentEvent` + `serializeRecordForDiff` + `lookupOriginatingStep` + `kvEquals` / `extractPosKV` / `extractStringKV` helpers + `Divergence.Format()`
- `pkg/testing/replay_diff_test.go` — `TestReplay_FirstDivergentEvent_IdenticalStreams_ReturnsNil`, `TestReplay_FirstDivergentEvent_StructuralDivergence`, `TestReplay_FirstDivergentEvent_PayloadDivergence`, `TestReplay_DivergenceReportFormat`, `TestReplay_RunOnceCapturing_NoDivergence`

**Modified:**

- `pkg/interpreter/walk_step.go` — `logger.Info("skytime", "event", "step_dispatch", ...)` call gains two new KV pairs: `"name", label` and `"pos", step.Pos`
- `pkg/interpreter/walk_step_actionfn_test.go` — added `TestWalkStep_StepDispatch_CarriesPosAndName` confirming the two new KV pairs land in captured records
- `pkg/testing/router_test.go` — `TestAttempts_IncrementOnRetry` skip message rewritten to point forward at Plan 04 Task 3

## Decisions Made

See frontmatter `key-decisions` for the full list. Most-load-bearing:

1. **Lift target = NEW non-test file `replay_helper.go`** instead of touching `replay_determinism_test.go`. The existing private `runOnceCapturing` + `eventCapturingLogger` continue to power the Phase 04.2 internal replay tests. The new public `RunOnceCapturing` + `EventCapture` are a strict superset; coexistence avoids cascading test-file churn for zero functional gain.
2. **Per-record serialization sorts KV pairs alphabetically** (consumer-side, not producer-side). Without sorting, two truly byte-equal-event-stream runs would render to different serialized strings due to Go map iteration randomness — a false-positive divergence the diff helper must NOT produce. The producer (`logger.Info(... kvs...)`) preserves source order; only the diff helper imposes the canonical ordering.
3. **Activity-boundary listener pair as the GetWorkflowHistory substitute.** `testsuite.TestWorkflowEnvironment` does NOT expose `GetWorkflowHistory`. The plan's Investigation 2 finding (cited in the must_haves) names `SetOnActivityStartedListener` + `SetOnActivityCompletedListener` as the canonical pair; both attach automatically inside `RunOnceCapturing` and append to the same `EventCapture` buffer. This pattern is now established for any future Phase 5/6 capture seam.
4. **`step_dispatch.pos` + `step_dispatch.name`** is the universal flow-callsite attribution channel. Any future divergence reporter / error annotator / live-renderer enrichment that needs "which originating step emitted this event" walks backward to the nearest `step_dispatch` and reads the two KV pairs. Plan 03 Task 0 made this mechanically possible; Plan 03 Task 2 is the first consumer.

## Deviations from Plan

None — plan executed exactly as written.

The plan's three tasks landed on the first try with TDD red-green for Tasks 0 and 1. Task 2 had no failing-test gate (the test was authored alongside the implementation per the plan's test list). All acceptance criteria verbatim:

- [x] `pkg/interpreter/walk_step.go` contains `"pos", step.Pos` AND `"name", label` inside the step_dispatch `logger.Info` call
- [x] `pkg/interpreter/replay_helper.go` exists (NON-test) with `func RunOnceCapturing(...) (*EventCapture, map[string]any, error)` matching the plan's signature exactly
- [x] `replay_helper.go` calls `env.SetOnActivityStartedListener(cap.onActivityStarted)` AND `env.SetOnActivityCompletedListener(cap.onActivityCompleted)`
- [x] `replay_helper.go` contains `var _ log.Logger = (*EventCapture)(nil)` compile-time assertion
- [x] Existing `pkg/interpreter/replay_determinism_test.go::Test*` tests ALL still pass
- [x] `TestReplay_DeterministicEventSequence` runs from the new public API (verified via `-run TestReplay_DeterministicEventSequence`)
- [x] `pkg/testing/replay.go` contains the planned `RunOnceCapturing` signature
- [x] `pkg/testing/replay_diff.go` contains `type Divergence struct {`, `func FirstDivergentEvent(...) *Divergence`, `func (d *Divergence) Format() string` matching D5-D2 verbatim
- [x] `lookupOriginatingStep` reads the `pos` and `name` KV pairs added by Plan 03 Task 0; `TestReplay_DivergenceReportFormat` confirms `d.FlowCallsite.Line == 14` + `d.StepName == "fetch user"`
- [x] `helperParseProductionFlow` fully implemented (no panic stub); the smoke test `TestReplay_RunOnceCapturing_NoActivities` passes
- [x] `TestAttempts_IncrementOnRetry` carries a `t.Skip(...)` whose message names "Plan 04" verbatim
- [x] `go test -race -count=1 ./pkg/testing/... ./pkg/interpreter/...` exits 0
- [x] `go vet ./pkg/testing ./pkg/interpreter` exits 0

## Issues Encountered

None blocking. The only minor cleanup was the vestigial `kwIter` interface placeholder in `writeSortedKwargs` (Task 1 first-draft) — removed in the same task before commit because the inline `%v` rendering is sufficient and self-explanatory. No commit-history noise.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- **Plan 04 (`tester.run` end-to-end driver):** ready. `pkg/testing.RunOnceCapturing` + `FirstDivergentEvent` provide the substrate for the always-on replay-twice path. The driver flow is:
  - Read `*WorkflowSpec` for `dag.WorkflowInput` + `StartWorkflowOptions`
  - Build a `StepIndexLookup` from the parsed flow's Steps (so D5-D3 attribution doesn't fall back to (-1, idx))
  - Call `pkg/testing.RunOnceCapturing(parsed, hash, init, reg, attempts, lookup)` TWICE
  - Compare via `FirstDivergentEvent(run1, run2, testCallsite)` — if non-nil, surface `d.Format()` through `starlarktest.SetReporter(thread, t)` so D5-F1 routes the failure to the correct sub-`*testing.T`
  - On success, return the first run's final state to the test author
  - `TestAttempts_IncrementOnRetry` activation in Plan 04 Task 3: build a 1-step flow that calls `gh.get`; mock returns err on attempts 1+2, ok on attempt 3; assert `attempts.Snapshot()` shows 3 calls for the structural key
- **Plan 05 (discovery + JSON output + human format):** ready. EventCapture is the native carrier for any future per-event renderer; the existing `pkg/cli` Bazel renderer already consumes step_dispatch events with `pos`+`name` KV pairs (no breaking change — additive only).
- **Plan 06 (CLI subcommand + e2e firewall):** unaffected.
- **No blockers.** All Wave-2 contracts are pinned by named tests; Plan 04 can compose without re-litigating field names or shapes.

## Self-Check: PASSED

Verified file-existence and commit-presence for every claim in this Summary.

**Files created (verified via `[ -f path ]` after the Plan-03 commits):**

- `pkg/interpreter/replay_helper.go` — FOUND
- `pkg/interpreter/replay_helper_test.go` — FOUND
- `pkg/testing/replay.go` — FOUND
- `pkg/testing/replay_test.go` — FOUND
- `pkg/testing/replay_diff.go` — FOUND
- `pkg/testing/replay_diff_test.go` — FOUND

**Files modified:**

- `pkg/interpreter/walk_step.go` — contains `"pos", step.Pos` and `"name", label` inside the step_dispatch logger.Info call
- `pkg/interpreter/walk_step_actionfn_test.go` — contains `func TestWalkStep_StepDispatch_CarriesPosAndName(`
- `pkg/testing/router_test.go` — `TestAttempts_IncrementOnRetry` t.Skip message names "Plan 04" verbatim

**Commits (verified via `git log --oneline | grep`):**

- `9490eee` feat(05-03): extend step_dispatch event with pos+name attrs (D5-D3 prereq) — FOUND
- `7fb15f3` feat(05-03): lift runOnceCapturing → public interpreter.RunOnceCapturing — FOUND
- `44c5055` feat(05-03): pkg/testing.RunOnceCapturing wrapper + replay-diff reporter — FOUND

**Test gates:**

- `go test -race -count=1 ./pkg/interpreter/... ./pkg/testing/... ./tests/...` → all packages OK
- `go vet ./pkg/interpreter ./pkg/testing` → clean
- `gofmt -d` on touched files → clean
- `TestReplay_DeterministicEventSequence` → PASS (TEST-04 baseline)
- `TestReplay_DivergenceReportFormat` → PASS (D5-D2 verbatim shape + D5-D3 attribution)
- `TestPkgTesting_ImportsTestsuite` → PASS (firewall meta-test still green; pkg/testing's testsuite import unchanged)
- `TestPkgTesting_DoesNotImportSDKWorker` → PASS

---
*Phase: 05-tier-3-e2e-test-harness-temporal-test*
*Completed: 2026-05-05*
