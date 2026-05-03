---
phase: 260503-qx1-include-step-kind-label-on-step-complete
plan: 01
subsystem: pkg/interpreter + pkg/cli
tags:
  - quick-task
  - rendering
  - tdd
  - interpreter-events
  - live-block
requirements:
  - QUICK-260503-QX1
dependency_graph:
  requires:
    - "Quick 260503-qkk (branch suffix on step_complete)"
    - "Quick 260503-q9p (ANSI color treatment in live block)"
    - "Phase 04.1-01 (D4.1-15 step(name=\"...\") support)"
  provides:
    - "User-defined step name persists from spinner row onto finalized completion row"
    - "Live + static renderers consume ev.KindAttr (no longer hardcoded \"step\") on step_complete"
    - "Latent bug fix: if_cond/script/for_each_parallel/call_flow rows now render with their correct kind word on completion (previously all showed as \"step\")"
  affects:
    - "renderStepComplete column shape (counter | kind | label | marker | dur | summary)"
    - "All seven interpreter step_complete emit sites (walk_step ×2, walk_ifcond, walk_script, walk_foreach ×2, walk_callflow)"
tech_stack:
  added: []
  patterns:
    - "Parse-time route (a) over runtime route (b): emit the data on the event itself rather than maintaining a labelByIdx map across WithAttrs/WithGroup shallow-copies"
    - "padKind reuse: completion-line kind column shares the static-path width=19 helper, mirroring renderStepDispatch"
    - "Hoist label to a local in walk_foreach so dispatch + deferred complete share the same string (single source of truth for items=N)"
key_files:
  created: []
  modified:
    - pkg/interpreter/walk_step.go
    - pkg/interpreter/walk_ifcond.go
    - pkg/interpreter/walk_script.go
    - pkg/interpreter/walk_foreach.go
    - pkg/interpreter/walk_callflow.go
    - pkg/cli/progress_static.go
    - pkg/cli/progress_live.go
    - pkg/cli/progress_test.go
    - pkg/cli/progress_live_test.go
decisions:
  - "Route (a) — interpreter emits label attr — beats route (b) — renderer-side labelByIdx map — for stateless symmetry: every dispatch event already emits label, so the absence on step_complete is a bug, not a design choice"
  - "computeCounter on renderStepComplete (replacing the legacy 5/7-space indent) so the dispatch row above and the completion row below align column-for-column"
  - "Live-path step_complete now reads padKind(ev.KindAttr) instead of hardcoding the literal \"step\" — fixes the latent bug where if_cond/script/for_each_parallel/call_flow rows rendered with the wrong kind word on completion"
  - "Hoist label := fmt.Sprintf(\"items=%d\", n) in walk_foreach so dispatch and deferred complete share a single source of truth (avoid recomputing the format string twice)"
  - "Resolution-error path in walk_foreach uses literal \"items=?\" on both dispatch and complete (the count is unknown when the items-lambda fails before len(items) is computable)"
metrics:
  duration: "4min"
  tasks: 2
  files: 9
  completed_date: "2026-05-03"
---

# Quick 260503-qx1: Include kind + label on step_complete line — Summary

**One-liner:** Mirror the step_dispatch column shape onto step_complete so user-defined step names (D4.1-15 `step(name="Get repo ${ctx.repo}")`) persist past the dispatch banner onto the finalized ✓ row, and fix the latent bug where the live renderer's completion line hardcoded "step" for every kind.

## Context

User said `step(name="...")` and meant it — the name is the identity of the step, and identity must persist through the entire row's lifecycle (dispatch → spinning → completion). Pre-fix, the label appeared on the dispatch banner and during the live spinner redraw, then **dropped on completion**:

```
[1/3] step                Get repo octocat/Hello-World        ← dispatch (had label)
  [1/1] step               Get repo octocat/Hello-World  ⠋ 0.2s   ← spinner (had label)
[1/3] step  ✓ 234ms  status=200                                   ← completion (lost label!)
```

Plus a latent bug in the live renderer: line 214 hardcoded `"%sstep%s"` regardless of `ev.KindAttr`, so an if_cond completion rendered as `step` not `if_cond`.

## Implementation

### Route (a) chosen over route (b)

**Route (a) — interpreter emits label on step_complete:** all seven `logger.Info("skytime", "event", "step_complete", ...)` call sites in pkg/interpreter (walk_step ×2, walk_ifcond, walk_script, walk_foreach ×2, walk_callflow) gain a `"label", <expr>` attribute pair, exactly mirroring their existing step_dispatch shape. Renderer-side is then a stateless read of `attrs.str("label")` / `ev.Label` (already populated by buildProgressEvent at pkg/cli/progress.go line 280).

**Route (b) — renderer-side labelByIdx map:** rejected because it requires managing a `map[int64]string` on `*progressHandler` and `*liveRenderer` that has to survive WithAttrs/WithGroup shallow-copies, plus a parallel cleanup pass. Route (a) is one-line-per-call-site and stateless.

### Files modified

| File | Change |
|------|--------|
| `pkg/interpreter/walk_step.go` | Added `"label", label` to both step_complete emit sites (deferred + empty-batch short-circuit). `label` already in scope from line 32 `i.stepDisplayLabel(ctx, step)`. |
| `pkg/interpreter/walk_ifcond.go` | Added `"label", "cond"` (mirrors step_dispatch literal). |
| `pkg/interpreter/walk_script.go` | Added `"label", label` (uses local from line 26-29: `n.OutputAlias` or `n.ID`). |
| `pkg/interpreter/walk_foreach.go` | Hoisted `label := fmt.Sprintf("items=%d", n)` so dispatch + deferred complete share the string; added `"label", "items=?"` on the resolution-error path. |
| `pkg/interpreter/walk_callflow.go` | Added `"label", cf.Name` (mirrors step_dispatch). |
| `pkg/cli/progress_static.go` | renderStepComplete rewritten to mirror renderStepDispatch column shape: `<indent><counter> <padKind(kind)>  <label>  <marker> <ms>ms  <summary>`. computeCounter replaces the legacy 5/7-space indent so dispatch + completion column-align. |
| `pkg/cli/progress_live.go` | case "step_complete" Fprintf rewritten: `padKind(ev.KindAttr)` replaces hardcoded `"step"` (latent bug fix); `ev.Label` inserted between kind column and marker. |

### Renderer column-shape comparison

Before:
```
[1/3] step                Get repo octocat/Hello-World        ← dispatch
     ✓ 234ms  status=200                                       ← completion (legacy 5-sp indent)
```

After:
```
[1/3] step                Get repo octocat/Hello-World        ← dispatch
[1/3] step                Get repo octocat/Hello-World  ✓ 234ms  status=200   ← completion (mirrors dispatch + marker/dur/summary suffix)
```

Branch suffix from qkk continues to append after the summary unchanged:
```
[3/3] if_cond             ctx.health  ✓ 1ms   → then
```

## Tests

### Task 1 (RED) — `test(260503-qx1): step_complete includes kind + label (RED)` @ `70de069`

**4 new static tests** in pkg/cli/progress_test.go:
- `TestProgress_StepCompleteIncludesKindAndLabel` — kind+label render before marker
- `TestProgress_StepCompleteIncludesKindAndLabel_Err` — same on err path
- `TestProgress_StepCompleteIncludesLabelWithBranchSuffix` — kind+label coexist with qkk branch suffix
- `TestProgress_StepCompleteEmptyLabel_NoCrash` — empty label doesn't panic

**4 new live tests** in pkg/cli/progress_live_test.go (mirrors).

**Existing tests updated:**
- `TestProgress_BazelFormat` step_complete ok/err sub-cases: added `slog.String("label", "gh.get(/x)")` + extended want slice.
- `emitTestEvents` helper: added label attr.
- `TestLiveBlock_FinalizeRow`: added KindAttr+Label, asserts label substring.
- `TestLiveBlock_FailedStepRendersX`: added KindAttr+Label, asserts label substring.
- `TestLiveBlock_CompletedRowMarkerColor_Ok` / `_Err`: added KindAttr+Label (data consistency).
- `TestLiveBlock_BranchAppendsToStepComplete`: replaced `"step  ✓ 1ms"` substring assertion with separate `if_cond` + `ctx.health` substring assertions; updated single-line loop to look for `if_cond` + `ctx.health` instead of `step`.
- `TestLiveBlock_BranchArrowColor`: added KindAttr+Label to the step_complete progressEvent literal.

RED confirmed: 9 failing tests (4 new static + 3 new live + 2 updated TestProgress_BazelFormat sub-cases failed; live tests with new assertions also failed).

### Task 2 (GREEN) — `fix(260503-qx1): include kind + label on step_complete line` @ `9f4858b`

Both renderers consume the new attrs; all 9 RED tests pass; all pre-existing tests stay green.

## Verification

| Check | Result |
|-------|--------|
| `go test ./pkg/cli/... -count=1` | PASS (4.9s) |
| `go test ./pkg/interpreter/... -count=1` | PASS (0.5s) |
| `go test ./... -count=1` | PASS (all packages green) |
| `go build -o skytime ./cmd/skytime` | PASS (33MB binary) |
| `go vet ./...` | PASS (no warnings) |

### Regression checks (the load-bearing ones)

- **q9p colors preserved**: TestLiveBlock_BannerHasColor, TestLiveBlock_CompletedRowMarkerColor_Ok / _Err, TestLiveBlock_BranchArrowColor, TestLiveBlock_FlowFailedHasRedFailedMarker, TestLiveBlock_FlowCompleteBannerColored — all green.
- **qkk branch suffix preserved**: TestProgress_BranchAppendsToStepComplete (then/else), TestProgress_OrphanBranchEvent_NoStandaloneLine, TestLiveBlock_BranchAppendsToStepComplete, TestLiveBlock_OrphanBranchEvent_NoOutput — all green.
- **Latent bug fix verified**: TestLiveBlock_StepCompleteIncludesKindAndLabel_Err pins that an `if_cond` completion now renders the word `if_cond` (not the previously-hardcoded `step`).

## Deviations from Plan

None — plan executed exactly as written. The plan's note about "the substring 'step  ✓ 1ms' needs to change since the if_cond row now renders as if_cond not step" was the load-bearing reason to update TestLiveBlock_BranchAppendsToStepComplete; that update was made verbatim.

## Decisions Made

1. **Route (a) over route (b)**: interpreter emits the attr (one-line-per-call-site, stateless) over renderer-side labelByIdx map (would require shallow-copy survival across WithAttrs/WithGroup).
2. **computeCounter on renderStepComplete**: replaces the legacy 5/7-space indent block so the dispatch row above and the completion row below column-align — consistent with the rest of the static renderer.
3. **Hoist label in walk_foreach**: single source of truth for `items=N` shared between dispatch and deferred complete; avoids recomputing the format string twice.
4. **Live-path padKind(ev.KindAttr)**: fixes the latent bug where if_cond/script/for_each_parallel/call_flow completion rows hardcoded "step". Now matches renderStepDispatch's column treatment.
5. **Resolution-error path in walk_foreach uses literal "items=?"**: the count is unknown when the items-lambda fails before `len(items)` is computable; matches the dispatch label on the same path.

## Self-Check: PASSED

- pkg/interpreter/walk_step.go modified (verified via `grep -n '"label", label' pkg/interpreter/walk_step.go` — 2 occurrences in step_complete blocks)
- pkg/interpreter/walk_ifcond.go modified (`"label", "cond"` present)
- pkg/interpreter/walk_script.go modified (`"label", label` in step_complete block)
- pkg/interpreter/walk_foreach.go modified (label hoist + 2 emit-site updates including resolution-error path)
- pkg/interpreter/walk_callflow.go modified (`"label", cf.Name` in step_complete block)
- pkg/cli/progress_static.go modified (renderStepComplete rewritten with padKind + computeCounter)
- pkg/cli/progress_live.go modified (padKind(ev.KindAttr) replaces hardcoded "step")
- pkg/cli/progress_test.go modified (4 new tests + 2 updated sub-cases + emitTestEvents updated)
- pkg/cli/progress_live_test.go modified (4 new tests + multiple existing-test updates)
- Commit `70de069` (RED) found in git log
- Commit `9f4858b` (GREEN) found in git log
