---
phase: 03-lambda-serialization-decision-interpreter-worker
plan: 01
subsystem: dsl-foundation
tags: [dag, parser, task_queue, max_concurrency, workflow_input, firewall, retrofit]

# Dependency graph
requires:
  - phase: 01-type-spine-extension-contract-parser-bridge-foundations
    provides: dag.Flow, dag.Step, dag.ForEachParallel, dag.WorkflowInput skeleton, parser builtin scaffolding (UnpackArgs + kwarg presence detection idiom)
  - phase: 02-generic-activity-block-batch-dispatch-credentials
    provides: pkg/activity firewall test (renamed + expanded here), lintMixedIdempotency / lintBlockSize finalize-pass shape (mirrored by lintEmptyTaskQueue)
provides:
  - dag.Flow.TaskQueue + dag.Step.TaskQueue (D3-19)
  - dag.ForEachParallel.MaxConcurrency (D3-13 backport)
  - dag.WorkflowInput rewrite to {FlowName, ContentHash, InitState} (D3-04, D3-05) — no more embedded *Flow / Lambdas
  - parser kwargs: flow(task_queue=...), step(task_queue=...), for_each_parallel(max_concurrency=...) with validation
  - Phase 2 firewall renamed to TestNoTemporalImportsOutsideAllowList; allowlist expanded from {activity} to {activity, interpreter, worker}
  - 2 new fixtures (1 valid task-queue-overrides; 1 invalid empty-task-queue)
affects:
  - 03-02-PLAN.md (pkg/interpreter foundations — the firewall now permits SDK imports there)
  - 03-03-PLAN.md (interpreter walkers — consume TaskQueue + MaxConcurrency)
  - 03-04-PLAN.md (pkg/worker bootstrap — firewall permits SDK imports; consumes WorkflowInput shape via the registry lookup)

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Kwarg-presence detection helper `hasKwarg(kwargs, name)` — distinguishes 'kwarg supplied as zero-value' from 'kwarg omitted'; lifts the inline pattern that builtinStep / builtinForEachParallel used into a named helper for reuse across the task_queue retrofit"
    - "Defense-in-depth lint pass + builtin-side primary check — the linter pass (lintEmptyTaskQueue) is a documented stub today because dag types carry no presence flag post-construction; symmetrically named with lintMixedIdempotency / lintBlockSize so the finalize-pass family stays uniform and a future presence flag has an obvious home"
    - "Custom MarshalJSON shape extension for new optional fields — when a node type has a custom MarshalJSON (Flow, Step, ForEachParallel), new omitempty fields must be added to BOTH the struct and the marshal-time shape struct to surface in JSON output; demonstrated for TaskQueue and MaxConcurrency"
    - "Forward-compatible firewall allowlist — the firewall test loops a slice of allowed pkg/* prefixes rather than hard-coding one, so future plans (here: 03-02 / 03-04) can ship without touching the firewall"

key-files:
  created:
    - tests/fixtures/valid/08-task-queue-overrides.star
    - tests/fixtures/valid/08-task-queue-overrides.golden.json
    - tests/fixtures/invalid/11-empty-task-queue.star
  modified:
    - pkg/dag/input.go (rewrite: WorkflowInput {FlowName, ContentHash, InitState})
    - pkg/dag/input_test.go (rewrite: round-trip + exact-keyset + nil InitState)
    - pkg/dag/flow.go (TaskQueue field added)
    - pkg/dag/flow_test.go (TestFlow_TaskQueueRoundTrip added)
    - pkg/dag/step.go (TaskQueue field added)
    - pkg/dag/step_test.go (TestStep_TaskQueueRoundTrip added)
    - pkg/dag/control.go (MaxConcurrency field added)
    - pkg/dag/control_test.go (2 MaxConcurrency tests added)
    - pkg/dag/marshal.go (flowJSON / stepJSON / forEachParallelJSON shapes extended)
    - pkg/parser/builtins.go (task_queue + max_concurrency kwargs; hasKwarg helper)
    - pkg/parser/builtins_test.go (6 new tests across the three retrofits)
    - pkg/parser/linter.go (lintEmptyTaskQueue + walk helper)
    - pkg/parser/linter_test.go (3 new tests for empty-task-queue rejection + linter stub)
    - pkg/parser/finalize.go (wire lintEmptyTaskQueue into finalize order)
    - pkg/activity/firewall_test.go (rename + expand allowlist)

key-decisions:
  - "Empty-string task_queue rejected at the BUILTIN level (not the linter) — kwarg presence detection (hasKwarg) distinguishes 'omitted' from 'supplied as empty', which is impossible post-construction. The matching linter pass is a documented no-op stub kept for symmetry with D2-05/D2-07 lints."
  - "Custom MarshalJSON shapes for Flow/Step/ForEachParallel were extended with the new omitempty fields rather than removed in favor of default marshaling. The Phase 1 marshal.go file-header comment about cross-machine stability (Pos exclusion) keeps the custom path necessary, so the cleanest delta is mirroring the new fields into the shape structs."
  - "WorkflowInput's custom MarshalJSON was deleted entirely. Phase 1 needed it to omit *starlark.Function values from Lambdas; Phase 3's three-field shape (FlowName, ContentHash, InitState) is JSON-natural — defaults produce stable, sorted-key output."
  - "MaxConcurrency=0 is allowed at parse time and means 'interpreter applies the default (10 per D3-13)'. Negative values are rejected with a position-aware ParseError. Splitting 'unset' from 'explicitly 0' was unnecessary because both have identical interpreter semantics."
  - "Firewall allowlist driven by a slice literal `allowedPkgs := []string{\"activity\", \"interpreter\", \"worker\"}` rather than hard-coded prefixes. Future plans only touch the slice, not the loop body. Until plans 03-02 / 03-04 land, the latter two entries are no-op skips — harmless."

patterns-established:
  - "DSL retrofit pattern (D3-19 + D3-13): add field to dag type → mirror into custom MarshalJSON shape → add `kwarg?` to UnpackArgs → reject zero/negative when supplied → update tests + 1 valid fixture + 1 invalid fixture. Future v1.x retrofits (e.g. per-step search-attribute kwargs) can follow the same recipe."
  - "Builtin-side kwarg presence detection (`hasKwarg(kwargs, \"foo\")`) for kwargs whose 'absent' and 'supplied-as-zero-value' meanings must diverge. Promoted from the inline `for _, kv := range kwargs` idiom that builtinStep used for retry/timeout."

requirements-completed:
  - INTRP-01
  - WORK-01

# Metrics
duration: 8min
completed: 2026-04-30
---

# Phase 3 Plan 01: Wave 0 — DSL retrofits + WorkflowInput rewrite + firewall expansion Summary

**Threaded `task_queue` and `max_concurrency` kwargs through `flow()` / `step()` / `for_each_parallel()` builtins to new dag fields (D3-19, D3-13), rewrote `WorkflowInput` to `{FlowName, ContentHash, InitState}` per D3-04/D3-05, and expanded the Phase 2 firewall to allow `pkg/{activity,interpreter,worker}` to import the Temporal SDK.**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-04-30T02:39:39Z
- **Completed:** 2026-04-30T02:47:36Z
- **Tasks:** 4
- **Files modified:** 16 (13 modified + 3 created)

## Accomplishments

- **DSL retrofit (D3-19):** `flow(name=..., task_queue="critical", ...)` and `step(action=..., task_queue="slow_io")` now thread the override into `dag.Flow.TaskQueue` and `dag.Step.TaskQueue`. Empty-string is rejected at parse time with a position-aware `*dag.ParseError`. Hierarchy at execute time (Phase 3 interpreter): `step > flow > worker default`.
- **MaxConcurrency backport (D3-13):** `for_each_parallel(..., max_concurrency=N)` threads to `dag.ForEachParallel.MaxConcurrency`. Negative values rejected; zero means "interpreter default (10)". Backported into Wave 0 to keep plan 03-03 under its file-count threshold.
- **WorkflowInput rewrite (D3-04, D3-05):** Phase 1's `{Flow *Flow, Lambdas map[string]*CapturedLambda, InitState map[string]any}` (with custom MarshalJSON to omit non-serializable function values) is replaced with `{FlowName string, ContentHash string, InitState map[string]any}`. The worker resolves the parsed flow + lambda map from its in-memory registry by `(FlowName, ContentHash)` at every workflow tick. No custom MarshalJSON; default encoding/json is JSON-natural.
- **Firewall expansion:** `TestNoTemporalImportsOutsidePkgActivity` renamed to `TestNoTemporalImportsOutsideAllowList` and the allowlist switched from hard-coded `pkg/activity` to a slice-driven `[activity, interpreter, worker]`. Forward-compatible: plans 03-02 / 03-04 will ship SDK imports without touching this test.
- **2 new fixtures:** `tests/fixtures/valid/08-task-queue-overrides.star` (per-flow + per-step overrides) and `tests/fixtures/invalid/11-empty-task-queue.star` (empty-string rejection). All 11 invalid fixtures and all 8 valid fixtures continue to parse/fail as expected.

## Task Commits

Each task was committed atomically:

1. **Task 1: pkg/dag — TaskQueue fields + WorkflowInput rewrite** — `8210dc6` (feat)
2. **Task 2: pkg/parser — task_queue kwarg + linter validation + 2 fixtures** — `2f5d5d2` (feat)
3. **Task 3: Phase 2 firewall expansion to {activity, interpreter, worker}** — `ae8cbbb` (test)
4. **Task 4: pkg/dag + pkg/parser — MaxConcurrency backport on for_each_parallel** — `fc3823d` (feat)

## Files Created/Modified

**Created:**
- `tests/fixtures/valid/08-task-queue-overrides.star` — per-flow `task_queue="critical"` + per-step `task_queue="slow_io"` exercise
- `tests/fixtures/valid/08-task-queue-overrides.golden.json` — golden output (auto-generated via `UPDATE_GOLDEN=1`)
- `tests/fixtures/invalid/11-empty-task-queue.star` — `flow(task_queue="")` rejected with `# expects: task_queue must be non-empty`

**Modified — pkg/dag:**
- `input.go` — rewrite to `{FlowName, ContentHash, InitState}`; custom MarshalJSON deleted
- `input_test.go` — rewrite for round-trip + exact-keyset + nil InitState assertions
- `flow.go` — `TaskQueue string \`json:"task_queue,omitempty"\`` field
- `flow_test.go` — `TestFlow_TaskQueueRoundTrip`
- `step.go` — `TaskQueue string \`json:"task_queue,omitempty"\`` field
- `step_test.go` — `TestStep_TaskQueueRoundTrip` (+ added `require` import)
- `control.go` — `MaxConcurrency int \`json:"max_concurrency,omitempty"\`` field on ForEachParallel
- `control_test.go` — `TestForEachParallel_MaxConcurrencyRoundTrip` + `TestForEachParallel_MaxConcurrencyDefaultZero`
- `marshal.go` — `flowJSON.TaskQueue`, `stepJSON.TaskQueue`, `forEachParallelJSON.MaxConcurrency` mirrored into the marshal-shape structs

**Modified — pkg/parser:**
- `builtins.go` — `task_queue?` kwarg on `flow()` + `step()`; `max_concurrency?` on `for_each_parallel()`; new `hasKwarg(kwargs, name)` helper; rejection of `task_queue=""` and `max_concurrency<0` with position-aware errors
- `builtins_test.go` — 6 new tests (3 task_queue + 3 max_concurrency)
- `linter.go` — `lintEmptyTaskQueue` + `walkLintEmptyTaskQueue` (documented stub for symmetry with D2-05/D2-07)
- `linter_test.go` — 3 new tests (flow-level rejection, step-level rejection, linter stub no-op)
- `finalize.go` — wire `lintEmptyTaskQueue` into the finalize-pass order between `lintBlockSize` and `validateActionRefKwargs`

**Modified — pkg/activity:**
- `firewall_test.go` — rename `TestNoTemporalImportsOutsidePkgActivity` → `TestNoTemporalImportsOutsideAllowList`; replace hard-coded `pkg/activity` skip with `allowedPkgs := []string{"activity", "interpreter", "worker"}` loop

## Decisions Made

See frontmatter `key-decisions` for the full set with rationale. Highlights:

- **Empty-string `task_queue` rejection lives in the BUILTIN, not the linter.** Post-construction, dag types have no presence flag, so the linter pass is a stub kept for naming symmetry. The kwarg-presence detection (`hasKwarg`) at the builtin level is the only place where "absent" and "supplied empty" can be distinguished.
- **Custom MarshalJSON shapes extended (not removed)** for Flow/Step/ForEachParallel. The Phase 1 design (cross-machine stability via Pos exclusion) still applies, so the cleanest delta is mirroring new omitempty fields into both the struct and the shape struct.
- **WorkflowInput's custom MarshalJSON deleted entirely.** Phase 1's reason for it (omit *starlark.Function values) is gone; the new three-field shape is JSON-natural.
- **MaxConcurrency=0 is valid** (means "interpreter default"); only negatives are rejected. No need for a `*int` or presence flag.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical mirroring] Extended custom MarshalJSON shape structs for new fields**
- **Found during:** Task 1 (TaskQueue fields)
- **Issue:** The plan's `<action>` for Task 1 said "add a `TaskQueue string \`json:\"task_queue,omitempty\"\`` field" to `pkg/dag/flow.go` and `pkg/dag/step.go`. But Phase 1's `pkg/dag/marshal.go` defines custom `MarshalJSON` for both Flow and Step using shadow shape structs (`flowJSON`, `stepJSON`). Without mirroring `TaskQueue` into those shape structs, the field would never surface in JSON output — the `TestFlow_TaskQueueRoundTrip` and `TestStep_TaskQueueRoundTrip` round-trip assertions would fail despite the struct field existing.
- **Fix:** Added `TaskQueue string \`json:"task_queue,omitempty"\`` to `flowJSON` + `stepJSON`, and threaded `f.TaskQueue` / `s.TaskQueue` into the `json.Marshal` payloads. Same fix applied for `MaxConcurrency` on `forEachParallelJSON` in Task 4.
- **Files modified:** `pkg/dag/marshal.go`
- **Verification:** Round-trip tests pass; Phase 1 + 2 marshal tests still green; `go test ./pkg/dag/... -count=1` exits 0.
- **Committed in:** `8210dc6` (Task 1 commit) for Flow/Step; `fc3823d` (Task 4 commit) for ForEachParallel
- **Why automatic:** Plan invariant — JSON round-trip must preserve `task_queue` / `max_concurrency` keys. Without the marshal-shape mirror, the round-trip tests would fail. This is a Rule 2 (missing critical functionality) auto-fix; the mirror is required for correctness.

**2. [Rule 3 - Blocking] Created empty `08-task-queue-overrides.golden.json` stub before running `UPDATE_GOLDEN=1`**
- **Found during:** Task 2 (fixture creation)
- **Issue:** `pkg/parser/fixtures_test.go` line 161 has `if _, statErr := os.Stat(goldenPath); statErr != nil { return }` — when the golden file doesn't exist, the test silently skips comparison and the `UPDATE_GOLDEN` env var also doesn't trigger a write because the path-existence check happens first. Without an existing stub, no golden was generated.
- **Fix:** Wrote a placeholder `{}` to `tests/fixtures/valid/08-task-queue-overrides.golden.json` so `os.Stat` succeeds, then re-ran `UPDATE_GOLDEN=1 go test ./pkg/parser/... -run TestValidFixtures`. The framework rewrote the file with the correct golden output.
- **Files modified:** `tests/fixtures/valid/08-task-queue-overrides.golden.json`
- **Verification:** `go test ./pkg/parser/... -run TestValidFixtures -count=1` (no UPDATE_GOLDEN) passes for all 8 valid fixtures including the new `08-task-queue-overrides.star`.
- **Committed in:** `2f5d5d2` (Task 2 commit)
- **Why automatic:** Rule 3 (blocking issue) — without this two-step (stub then update) the new fixture cannot acquire its golden file.

---

**Total deviations:** 2 auto-fixed (1 Rule 2 — missing critical marshaling mirror; 1 Rule 3 — golden-file bootstrapping workflow).
**Impact on plan:** Both auto-fixes were necessary for the plan's stated acceptance criteria to be testable. No scope creep — both stayed strictly within the plan's task boundaries (Task 1, 2, 4 respectively).

## Issues Encountered

None during execution. The TDD RED → GREEN flow worked smoothly for all four tasks: tests written first, confirmed RED (build failures on missing fields), implementation added, confirmed GREEN, race-suite confirmed clean.

## Self-Check: PASSED

All 13 expected files exist on disk; all 4 task commits are reachable in git history (`8210dc6`, `2f5d5d2`, `ae8cbbb`, `fc3823d`). Full repo `go test ./... -race -count=1` exits 0; `go vet ./...` clean; `go build ./...` clean; firewall hold confirmed (0 non-allowlist importers).

## Next Phase Readiness

**Ready for plan 03-02 (pkg/interpreter foundations):**
- The firewall now permits `pkg/interpreter` to import `go.temporal.io/sdk/...`. Until that package is created, the allowlist entry is a no-op skip (forward-compatible).
- `dag.WorkflowInput` is in its final Phase 3 shape — plan 03-02's `FlowRegistry` consumes `(FlowName, ContentHash)` directly with no further wire-format changes.
- `dag.Flow.TaskQueue` and `dag.Step.TaskQueue` are populated by the parser; plan 03-03's interpreter walkers can read them without retrofitting the parser.

**Ready for plan 03-03 (interpreter walkers):**
- `dag.ForEachParallel.MaxConcurrency` is populated; the for_each_parallel walker can read the field directly and apply the D3-13 default of 10 when zero.

**Ready for plan 03-04 (pkg/worker bootstrap):**
- The firewall permits `pkg/worker` to import the SDK.
- `WorkflowInput.{FlowName, ContentHash}` is the registry-lookup key shape that the worker's flow-registry boot-time builder will materialize.

**Blockers / concerns:** None. All Phase 1 + 2 tests (245+ across pkg/dag, pkg/extension, pkg/extension/testing, pkg/bridge, pkg/parser, pkg/activity) remain green, including the race detector. `grep -rl 'go.temporal.io/sdk' pkg/ | grep -vE '/(activity|interpreter|worker)' | wc -l` outputs `0` — firewall holds.

---
*Phase: 03-lambda-serialization-decision-interpreter-worker*
*Completed: 2026-04-30*
