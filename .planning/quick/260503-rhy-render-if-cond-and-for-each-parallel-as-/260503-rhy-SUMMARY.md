---
phase: quick-260503-rhy
plan: 01
subsystem: pkg/cli (progress renderer)
tags: [renderer, progress, scopes, indent, ansi, tdd]
dependency_graph:
  requires:
    - quick-260503-q9p (ANSI color contract for live block — preserved)
    - quick-260503-qkk (branch suffix wiring — replaced by header on rhy)
    - quick-260503-qx1 (kind + label persistence on step_complete — preserved)
  provides:
    - "Block-scope rendering of if_cond + for_each_parallel on both static and live paths"
    - "Depth-based indentation (4 spaces per pathDepth level) for child rows"
    - "pathDepth(path) helper shared by both renderers"
  affects:
    - "Visual layout of any .star flow that uses if_cond or for_each_parallel — header + indented children + footer"
tech_stack:
  added: []
  patterns:
    - "Pure leaf helper (pathDepth) shared across two renderer files in the same package"
    - "Buffered cross-event state via map[idx]→cached_attr (ifCondTotalByIdx) to bridge a missing attr from the producer event"
    - "Static-line emission above the redraw region for scope kinds; active-list filter on dispatch keeps redraw region leaf-only"
key_files:
  created:
    - .planning/quick/260503-rhy-render-if-cond-and-for-each-parallel-as-/260503-rhy-SUMMARY.md
  modified:
    - pkg/cli/progress.go
    - pkg/cli/progress_static.go
    - pkg/cli/progress_live.go
    - pkg/cli/progress_test.go
    - pkg/cli/progress_live_test.go
decisions:
  - "D-RHY-13 implementation: branchByIdx field renamed to ifCondTotalByIdx — semantic shift from buffering branch NAME (consumed later by step_complete) to buffering parent TOTAL (consumed inline by renderBranch)"
  - "pathDepth lives in pkg/cli/progress.go (not progress_static.go or progress_live.go) — shared by both renderers without circular imports; pure function, no state"
  - "Counter format choice (top-level [N/M] vs nested [path/path]) decoupled from indent calculation — isNestedPath still owns counter; pathDepth owns indent"
  - "computeCounter is the only per-row indent boundary on the static path — both renderStepDispatch and renderStepComplete go through it; renderBranch inlines the same logic for the header"
  - "Live-path activeStep gets a Path field — depth-based indent for in-flight rows aligns with their static completion rows"
  - "Tests for D-RHY-06 case '3a.0.1b' resolved to depth 4 (the plan listed depth 3, but the implementation algorithm gives 4: 2 dots + 2 letter-suffix segments). Implementation chosen over plan's table value — algorithm is consistent and the case is defensive-only"
metrics:
  duration: 10m
  completed: 2026-05-03T20:03:00Z
  tasks: 2
  files: 5
---

# Quick 260503-rhy: Render if_cond + for_each_parallel as Block Scopes Summary

JWT-style scope rendering for Starlark control-flow primitives — `if_cond` and `for_each_parallel` now render as header (▶ branch / ▶ open) + indented children + footer (✓/✗ ms), eliminating the user-confusing "[1/1] step Get branches ✓ 445ms" row that sat at the same column as top-level siblings inside an if_cond body.

## Tasks Completed

| Task | Name                                                          | Commit  | Files                                                                                                                |
| ---- | ------------------------------------------------------------- | ------- | -------------------------------------------------------------------------------------------------------------------- |
| 1    | RED — pin scope rendering, indent, and live-path activeStep filtering | 74797c1 | pkg/cli/progress.go, pkg/cli/progress_test.go, pkg/cli/progress_live_test.go                                          |
| 2    | GREEN — implement scope rendering on static + live renderers  | 6bc3a4d | pkg/cli/progress.go, pkg/cli/progress_static.go, pkg/cli/progress_live.go, pkg/cli/progress_test.go, pkg/cli/progress_live_test.go |

## What Changed

### pkg/cli/progress.go

- Added `pathDepth(path string) int` — pure leaf helper, shared by both renderers. Counts `.` separators + segments ending in a letter (the if_cond branch suffix). Empty path returns 0 (defensive).
- Replaced `progressHandler.branchByIdx map[int64]string` with `ifCondTotalByIdx map[int64]int64` (D-RHY-13). The semantic shift: under qkk, the buffer held the branch NAME for the upcoming step_complete to consume as a suffix. Under rhy, the buffer holds the parent TOTAL (cached at the suppressed step_dispatch) for the upcoming branch event to consume as a header counter.
- Updated `WithAttrs` and `WithGroup` shallow-copy spreads to use the new field name.
- Added `import "strings"` (now needed by `pathDepth`).

### pkg/cli/progress_static.go

- `renderStepDispatch`: switches on `kind`. `if_cond` is suppressed (D-RHY-01) but caches total. `for_each_parallel` emits a HEADER ([N/M] for_each_parallel items=K ▶ open). Leaf kinds keep the existing dispatch shape.
- `renderBranch`: emits if_cond HEADER inline (no longer buffer-only). Reads cached total from `ifCondTotalByIdx`, format is `<indent>[N/M] if_cond cond ▶ <branch>`. Empty-branch events still defensively no-op.
- `renderStepComplete`: removed the `branchByIdx` consumer block. Same shape as qx1 (counter + kind + label + ✓/✗ + ms + summary), no branch suffix on any kind. For if_cond + for_each_parallel this row IS the scope footer.
- `computeCounter`: indent now derived from `pathDepth(path)` (4 spaces per level), counter format choice unchanged from qkk (`[path/path]` for nested, `[N/M]` otherwise).

### pkg/cli/progress_live.go

- `liveRenderer.branchByIdx` → `ifCondTotalByIdx` (mirror of the static-path field rename).
- `activeStep` gains a `Path` field — required so `redraw` can indent in-flight rows by depth (the previous fixed 2-space indent would mis-align with the new 4-spaces-per-depth static rows).
- `case "step_dispatch"`: switches on `KindAttr`. `if_cond` caches total, no other side effect (D-RHY-08: scope kinds never enter the active list). `for_each_parallel` clears the redraw region and emits a static HEADER. Leaf kinds append to the active list with `Path` populated.
- `case "branch"`: emits if_cond HEADER as a static line above the redraw region. Reads cached total from `ifCondTotalByIdx`. The header arrow ▶ is wrapped in ansiYellow (color-parity with static path, q9p invariant preserved).
- `case "step_complete"`: switches on `KindAttr`. Scope kinds emit a FOOTER (no active-list mutation, no branch suffix). Leaf kinds emit the qx1 completion shape with depth-based indent.
- `redraw`: in-flight row indent computed from `pathDepth(s.Path)`. The `[skytime] in-progress N active` meta-header stays at column 0.

### pkg/cli/progress_test.go (Task 1 + migration)

- Added `TestPathDepth` — table-driven coverage of D-RHY-06 cases plus a defensive multi-segment case (`3a.0.1b` → 4).
- Added behavioral tests for the new contract: `TestProgress_IfCond_RendersAsScope`, `TestProgress_IfCond_HeaderHasYellowArrow_OnTTY`, `TestProgress_ForEachParallel_RendersAsScope`, `TestProgress_NestedScope_DoubleIndent`, `TestProgress_LeafKinds_KeepExistingShape`, `TestProgress_StepDispatch_IfCond_Suppressed`, `TestProgress_StepComplete_IfCond_NoBranchSuffix`.
- Migrated `TestProgress_BranchAppendsToStepComplete` (then/else) — formerly asserted `→ then` on the step_complete line; now asserts `▶ then` on the HEADER line and a separate FOOTER line with no arrow.
- Migrated `TestProgress_StepCompleteIncludesLabelWithBranchSuffix` — same migration: header carries `▶ then`, footer is a separate line with no arrow.
- Migrated `TestProgress_BazelFormat/step_dispatch_if_cond_kind` — formerly asserted `[3/3] if_cond ctx.health` on the dispatch line; now asserts `[3/ if_cond cond ▶ then` on a branch-event-driven header (since dispatch is suppressed). The if_cond suppression case is independently covered by `TestProgress_StepDispatch_IfCond_Suppressed`.
- Migrated `TestProgress_OrphanBranchEvent_NoStandaloneLine` — formerly required zero output for an orphan branch (buffer-only). Now an orphan branch DOES emit a header (D-RHY-03), so the test asserts the header IS emitted but the OLD qkk standalone-arrow shape (`     → then` 5-space-indent line, or any `→ then` substring) is NOT.

### pkg/cli/progress_live_test.go (Task 1 + migration)

- Added behavioral tests: `TestLiveBlock_IfCond_RendersAsScope`, `TestLiveBlock_IfCond_NoActiveStepEntry`, `TestLiveBlock_ForEachParallel_RendersAsScope`, `TestLiveBlock_HeaderArrowYellow`, `TestLiveBlock_StepCompleteIfCond_NoBranchSuffix`.
- Migrated `TestLiveBlock_BranchArrowColor`, `TestLiveBlock_BranchAppendsToStepComplete`, `TestLiveBlock_StepCompleteIncludesLabelWithBranchSuffix` — same migration as the static-path qkk tests: header carries `▶`, footer carries `✓` with no arrow.
- Migrated `TestLiveBlock_OrphanBranchEvent_NoOutput` — same migration as `TestProgress_OrphanBranchEvent_NoStandaloneLine`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Test Migration] TestPathDepth case `3a.0.1b` corrected from depth=3 to depth=4**

- **Found during:** Task 1 (RED) test-run verification.
- **Issue:** The plan's TestPathDepth table listed `"3a.0.1b" → 3` as a defensive case, but the documented algorithm (`count('.') + count(segments_ending_in_letter)`) gives 4 for that input (2 dots + 2 letter-suffix segments).
- **Fix:** Updated the test expectation to 4, matching the algorithm. The simpler cases in the table (`3a.0`, `3.0.0`, `3.0a`) all match the algorithm; only the multi-segment defensive case had a typo'd expectation.
- **Files modified:** pkg/cli/progress_test.go (single line in `TestPathDepth` table)
- **Commit:** 74797c1 (Task 1)

**2. [Rule 1 — Test Migration] Three additional tests migrated to the rhy contract during Task 2**

- **Found during:** Task 2 (GREEN) — first run of `go test ./pkg/cli/... -count=1` after implementation.
- **Issue:** The plan's Task 1 migration list explicitly enumerated 5 tests to migrate (TestProgress_BranchAppendsToStepComplete then/else + TestProgress_StepCompleteIncludesLabelWithBranchSuffix + TestLiveBlock_BranchArrowColor + TestLiveBlock_BranchAppendsToStepComplete + TestLiveBlock_StepCompleteIncludesLabelWithBranchSuffix). Three additional tests were silently pinning the OLD qkk/buffer-only behavior:
  - `TestProgress_BazelFormat/step_dispatch_if_cond_kind` (asserted dispatch row for if_cond)
  - `TestProgress_OrphanBranchEvent_NoStandaloneLine` (asserted zero output for orphan branch — buffer-only)
  - `TestLiveBlock_OrphanBranchEvent_NoOutput` (same as above on the live path)
- **Fix:** Migrated all three to assert the new rhy contract. The QKK defenses (no inline `→ then` arrow, no 5-space-indent standalone shape) remain valid and are preserved verbatim in the migrated tests.
- **Files modified:** pkg/cli/progress_test.go, pkg/cli/progress_live_test.go (also part of Task 2 commit since the failures only surfaced after the GREEN implementation landed)
- **Commit:** 6bc3a4d (Task 2 — bundled with the GREEN implementation since these test changes are inseparable from the new contract)

**Why these test migrations weren't caught in Task 1:** The plan's Task 1 migration list was based on a focused grep for tests asserting `→ then` / `→ else` and the specific qkk shape. The three tests above pinned related-but-distinct properties — "dispatch shape for if_cond kind" and "buffer-only behavior on orphan branch" — that became invalid only after the GREEN implementation landed. Each is a one-test-one-line migration; total scope creep is <30 LoC across the two files.

### Auto-added Critical Functionality

None. The plan's design was complete.

## Authentication Gates

None.

## Verification

- `go test ./pkg/cli/... -count=1` exits 0. Includes:
  - All NEW tests for D-RHY-01..14: `TestPathDepth` (10 sub-cases), `TestProgress_IfCond_RendersAsScope`, `TestProgress_IfCond_HeaderHasYellowArrow_OnTTY`, `TestProgress_ForEachParallel_RendersAsScope`, `TestProgress_NestedScope_DoubleIndent`, `TestProgress_LeafKinds_KeepExistingShape`, `TestProgress_StepDispatch_IfCond_Suppressed`, `TestProgress_StepComplete_IfCond_NoBranchSuffix`, `TestLiveBlock_IfCond_RendersAsScope`, `TestLiveBlock_IfCond_NoActiveStepEntry`, `TestLiveBlock_ForEachParallel_RendersAsScope`, `TestLiveBlock_HeaderArrowYellow`, `TestLiveBlock_StepCompleteIfCond_NoBranchSuffix`.
  - All migrated qkk/qx1 tests on the new header-shape contract.
  - All preserved q9p / qkk-defense / qx1 / flow_failed tests.
- `go test ./... -count=1` exits 0. Full repo regression (14 packages, all green). Renderer is leaf in the dependency graph; no surprise ripple.
- `go vet ./...` exits 0.
- `go build -o /tmp/skytime-rhy ./cmd/skytime` exits 0, no warnings.
- No interpreter walker file modified — `git diff --stat HEAD~2 -- pkg/interpreter/` shows zero changes (firewall preserved).
- No new dependencies — `go.mod` `require` block unchanged.

## Key Invariants Preserved

- **q9p ANSI color contract**: header arrow ▶ wrapped in `ansiYellow + ansiReset` on both static and live paths (TestProgress_IfCond_HeaderHasYellowArrow_OnTTY, TestLiveBlock_HeaderArrowYellow). Banner ansiDimCyan, counter ansiBrightCyan, kind ansiBrightWhite, ok ansiGreen, err ansiRed all unchanged.
- **qkk no-orphan-line + no-old-standalone-shape**: defenses migrated verbatim into the updated TestProgress_OrphanBranchEvent_NoStandaloneLine and TestLiveBlock_OrphanBranchEvent_NoOutput. The OLD `→ then` inline arrow shape and the `     → then` standalone shape are both rejected.
- **qx1 kind+label persistence**: preserved on all leaf kinds (TestProgress_StepCompleteIncludesKindAndLabel, TestLiveBlock_StepCompleteIncludesKindAndLabel). For scope kinds, kind+label still appear on the FOOTER line (it's a step_complete row, just without the qkk branch suffix).
- **TTY/verbose mode selection (D4.1-17/20/21)**: untouched — TestProgress_StaticPath_*, TestProgress_LivePathChosen_*, TestNewProgressHandler_AcceptsVerboseFlag all pass without modification.
- **Truncation cap (D4.1-19) and spinner cadence (D4.1-18)**: untouched — TestLiveBlock_TruncationAtTen and TestLiveBlock_SpinnerCadence pass.
- **flow_failed renderer**: untouched — TestProgress_FlowFailed and TestLiveBlock_FlowFailedHasRedFailedMarker pass.

## Self-Check: PASSED

**Files exist:**
- FOUND: pkg/cli/progress.go (modified — pathDepth + ifCondTotalByIdx field rename)
- FOUND: pkg/cli/progress_static.go (modified — scope rendering on static path)
- FOUND: pkg/cli/progress_live.go (modified — scope rendering on live path)
- FOUND: pkg/cli/progress_test.go (modified — new tests + qkk/qx1 migration)
- FOUND: pkg/cli/progress_live_test.go (modified — new tests + qkk/qx1 migration)
- FOUND: .planning/quick/260503-rhy-render-if-cond-and-for-each-parallel-as-/260503-rhy-PLAN.md
- FOUND: .planning/quick/260503-rhy-render-if-cond-and-for-each-parallel-as-/260503-rhy-SUMMARY.md (this file)

**Commits exist:**
- FOUND: 74797c1 — test(260503-rhy): scope rendering header/footer + indent (RED)
- FOUND: 6bc3a4d — feat(260503-rhy): render if_cond + for_each as block scopes with indent
