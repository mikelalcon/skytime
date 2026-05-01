---
phase: 260501-p7c-fix-worker-versioning-auto-enable-bug-ma
plan: 01
subsystem: worker
tags: [temporal, build-id, versioning, worker-options, dev-server]

# Dependency graph
requires:
  - phase: 03-lambda-serialization-decision-interpreter-worker
    provides: pkg/worker.WorkerOptions with UseBuildIDVersioning + applyDefaults; pkg/worker.NewWorker passing UseBuildIDForVersioning verbatim to sdkworker.Options
provides:
  - worker-level Build ID versioning is opt-in (default false), not auto-enabled
  - dev/CLI runs against `temporal server start-dev` no longer hang on first dispatch
  - regression test (TestWorkerOptions_VersioningOptIn) pinning the opt-in contract end-to-end
  - SDK-boundary test (TestNewWorker_OptInVersioningPropagatesToSDK) proving propagation through the worker
affects: [05-runtime-tier-tests, 06-temporal-tier-tests, deploy/production hardening]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Opt-in semantics for production-only features: defaults that work for dev must NOT auto-enable production-only contracts (Build ID compatibility set registration must precede versioned worker)."

key-files:
  created: []
  modified:
    - pkg/worker/options.go
    - pkg/worker/options_test.go
    - pkg/worker/worker_test.go
    - .planning/PROJECT.md

key-decisions:
  - "UseBuildIDVersioning default flipped from auto-true to opt-in false — production deployers must explicitly set it AFTER registering a Build ID compatibility set on the task queue."
  - "Verification target locked to 'worker scheduled the workflow + activity dispatched' (observed in run stdout), NOT to RUN_EXIT — the exit code is dominated by an unrelated pre-existing noopCredentialHandler-on-empty-id retry loop."

patterns-established:
  - "Bug-fix evidence by log inspection: when an exit code is dominated by an unrelated downstream issue, prove the fix by grepping the captured log for the specific scheduling/dispatch lines that the bug would have prevented."

requirements-completed: [WORK-01-fix-versioning-auto-enable]

# Metrics
duration: 6min
completed: 2026-05-01
---

# Quick 260501-p7c Plan 01: Fix Worker Versioning Auto-Enable Bug Summary

**Removed the `applyDefaults` auto-flip of `UseBuildIDVersioning` so a default-constructed worker is unversioned; against a fresh `temporal server start-dev` task queue (no Build ID compatibility set registered) the dev server now dispatches tasks instead of leaving the workflow unscheduled forever.**

## Performance

- **Duration:** ~6 min
- **Started:** 2026-05-01T22:12:38Z
- **Completed:** 2026-05-01T22:18:26Z
- **Tasks:** 3
- **Files modified:** 4

## Accomplishments
- Auto-enable if-block deleted from `pkg/worker/options.go::applyDefaults` (4 lines + comment).
- Doc comment on `WorkerOptions.UseBuildIDVersioning` rewritten to spell out the opt-in contract: caller must register a Build ID compatibility set on the task queue first; otherwise versioned workers receive zero dispatches and workflows hang.
- `TestWorkerOptions_Defaults` flipped from `assert.True` to `assert.False` and given an updated message string; new `TestWorkerOptions_VersioningOptIn` added with two sub-cases (`default_stays_false`, `explicit_true_preserved`).
- `TestNewWorker_PassesBuildIDToSDK` updated to assert the opt-in contract at the SDK boundary; new `TestNewWorker_OptInVersioningPropagatesToSDK` proves explicit `UseBuildIDVersioning: true` reaches `sdkworker.Options.UseBuildIDForVersioning`.
- `.planning/PROJECT.md` D3-20 line clarified — Build ID line preserved, opt-in semantics now explicit.
- End-to-end no-hang verification: `skytime dev-server` + `skytime run examples/skeleton/simple_check.star` now produces "Started Worker", "skytime workflow start", and "ExecuteActivity" log lines within 1 s — proof the worker was dispatched. (RUN_EXIT=124 from the unrelated pre-existing empty-id credential retry loop is documented in deviations below.)

## Task Commits

Each task was committed atomically:

1. **Task 1: RED — assert opt-in behavior in tests** — `ad50b1e` (test)
2. **Task 2: GREEN — remove auto-enable + update doc comment** — `f4a154f` (fix)
3. **Task 3: PROJECT.md note + end-to-end no-hang verification** — `b4f04a7` (docs)

_Plan metadata commit (this SUMMARY + STATE) follows separately._

## Files Created/Modified

- `pkg/worker/options.go` — deleted 4-line auto-enable if-block in `applyDefaults`; rewrote `UseBuildIDVersioning` doc comment to document opt-in contract + operator prerequisite.
- `pkg/worker/options_test.go` — flipped `TestWorkerOptions_Defaults` final assertion from `True`→`False`; added `TestWorkerOptions_VersioningOptIn` with `default_stays_false` and `explicit_true_preserved` sub-cases.
- `pkg/worker/worker_test.go` — flipped `TestNewWorker_PassesBuildIDToSDK` SDK-boundary assertion from `True`→`False`; added `TestNewWorker_OptInVersioningPropagatesToSDK` to cover the explicit-true → SDK propagation path. (NOT in plan's `files_modified`; see Deviation #1.)
- `.planning/PROJECT.md` — line 34 updated: Build ID line preserved, "worker-level versioning is **opt-in** via `WorkerOptions.UseBuildIDVersioning`" inserted between the build-flag clause and the WorkflowInput clause.

## Decisions Made

- **Default UX over production safety nets at the default layer.** A library whose default makes dev rigs hang is broken UX. Production deployers who actually need Build ID versioning are competent to set the flag — and now they must, having registered a compatibility set on their task queue first. The doc comment carries the operator prerequisite verbatim.
- **End-to-end verification target.** The plan's success criterion was "RUN_EXIT != 124 within 30 s". The pre-existing `noopCredentialHandler.Resolve` errors on every ID — including empty ones — so the embedded CLI workflow ends in a 30-s retry loop that times out at exit 124 even with the bug fixed. The verification target was re-anchored to "worker dispatched the activity" by inspecting captured stdout for `Started Worker` / `skytime workflow start` / `ExecuteActivity` log lines. All three appeared within 1 s, proving the bug is fixed. The credential-handler issue is pre-existing (logged in STATE under phase 04-07 as a v1.x audit item) and out-of-scope under the scope-boundary rule.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `TestNewWorker_PassesBuildIDToSDK` was asserting the auto-enable behavior at the SDK boundary**
- **Found during:** Task 2 (full `go test ./... -race -count=1` after removing the auto-enable block).
- **Issue:** The plan's `<interfaces>` block stated, "Other callers verified via grep: ... pkg/worker/options_test.go: ONE assertion (`TestWorkerOptions_Defaults`). That is the FULL set." But `pkg/worker/worker_test.go::TestNewWorker_PassesBuildIDToSDK` line 116 also relied on the auto-enable behavior — it asserted `assert.True(t, capturedOpts.UseBuildIDForVersioning, "UseBuildIDForVersioning must be true when BuildID is set")`. After Task 2's fix the SDK boundary correctly received `false`, and this stale assertion failed.
- **Fix:** Updated the failing assertion to `assert.False` with a message explaining the opt-in contract at the SDK boundary; added `TestNewWorker_OptInVersioningPropagatesToSDK` to cover the explicit-true → SDK propagation path that the original test was implicitly conflating with auto-enable. Same package, same file, same test pattern as the original — purely fixing a stale test that was encoding the bug as desired behavior.
- **Files modified:** `pkg/worker/worker_test.go`
- **Verification:** Full `go test ./... -race -count=1` exits 0 across all 13 packages.
- **Committed in:** `f4a154f` (Task 2 commit, alongside the production fix)

### Verification Adjustment

**Verification target re-anchored** — the plan's `<verify>` script for Task 3 used `RUN_EXIT != 124` as the no-hang predicate. Re-running the script against the fixed binary still produced exit 124 (`timeout` fired) because the workflow is in an exponentially-backed-off activity retry loop on the pre-existing noopCredentialHandler-on-empty-id issue (already logged in STATE phase 04-07 as a v1.x audit item for `pkg/activity/credential_cache.go`).

The substance of the constraint is "the worker scheduled the workflow," and this is the exact behavior the plan's truth-statements describe ("Default WorkerOptions ... produces a worker that the Temporal dev server dispatches tasks to"). The captured stdout was inspected for three log signatures — `Started Worker`, `skytime workflow start`, `ExecuteActivity` — which appeared within ~1 s and prove the worker received and dispatched the task. Before this fix, none of them would have appeared (the workflow would sit unscheduled because Temporal refuses to dispatch to a versioned worker on a task queue with no compatibility set). This deviation is constraint-aligned: the prompt explicitly stated, "The verification target is 'the worker scheduled the workflow,' not 'github.com returned 200.'"

---

**Total deviations:** 1 auto-fixed (Rule 1 — stale test encoding the bug as desired behavior) + 1 verification adjustment (target re-anchored from RUN_EXIT to log-evidence per the prompt's stated constraint).

**Impact on plan:** No scope creep. Both stay strictly inside the worker subsystem. The Rule 1 fix is the natural completion of Task 2 (a test suite the plan thought was empty turned out to also encode the bug). The verification adjustment matches the constraint's substance.

## Issues Encountered

- The plan author's grep of `UseBuildIDVersioning` callers missed `pkg/worker/worker_test.go` (one of three callers), causing Task 2's GREEN step to fail until that test was also updated. The fix is in the same file family as the planned changes (worker package tests) and follows an identical assert-the-opt-in-contract pattern.
- The `noopCredentialHandler` in `cmd/skytime/main.go` errors on `id=""` (anonymous endpoints), which dominates the `RUN_EXIT` of `skytime run` even after the worker bug is fixed. This is pre-existing, already documented in STATE phase 04-07 ("Pre-existing pkg/activity/credential_cache.go does not short-circuit on empty IDs (logged as v1.x audit item; out of scope per scope-boundary rule)"), and out of scope for this quick fix. Verification was re-anchored to log-evidence to work around it.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- Bug fixed; default UX (`skytime dev-server` + `skytime run`) now dispatches workflows on a fresh dev rig with no Build ID choreography.
- Production-side opt-in path (`WorkerOptions{UseBuildIDVersioning: true}`) preserved verbatim through to `sdkworker.Options.UseBuildIDForVersioning` and pinned by `TestNewWorker_OptInVersioningPropagatesToSDK`.
- Outstanding (logged in STATE, not in scope here): `pkg/activity/credential_cache.go` does not short-circuit on empty credential IDs. Affected scenario: any flow where an http endpoint omits `credential=` causes a retry loop against the default `noopCredentialHandler`. This should be revisited as a v1.x bug — either short-circuit empty IDs in the cache, or change `noopCredentialHandler.Resolve` in `cmd/skytime/main.go` to return `(nil, nil)` on `id == ""`.

---
*Quick: 260501-p7c-fix-worker-versioning-auto-enable-bug-ma*
*Completed: 2026-05-01*

## Self-Check: PASSED

- All 5 files claimed in this SUMMARY exist on disk.
- All 3 task commit hashes (`ad50b1e`, `f4a154f`, `b4f04a7`) are present in `git log --oneline --all`.
- The four worker tests touched in this plan all pass: `TestWorkerOptions_Defaults`, `TestWorkerOptions_VersioningOptIn`, `TestNewWorker_PassesBuildIDToSDK`, `TestNewWorker_OptInVersioningPropagatesToSDK`.
