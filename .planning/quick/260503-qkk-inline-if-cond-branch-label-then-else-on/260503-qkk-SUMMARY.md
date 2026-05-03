---
phase: quick-260503-qkk
plan: 01
subsystem: cli/progress-renderer
tags: [cli, progress, renderer, ansi, ux]
requirements:
  - CLI-06
  - CLI-07
dependency-graph:
  requires:
    - "Phase 04.1-06 — live block renderer (liveRenderer + applyEvent dispatch)"
    - "Quick 260503-q9p — ANSI color parity between live + static paths"
  provides:
    - "Compacted if_cond rendering: branch decision inlined onto step_complete row"
    - "Buffer-based renderer state pattern (branchByIdx) for both static + live paths"
  affects:
    - "Demo output (examples/skeleton/simple_check.star) one row shorter per if_cond"
tech-stack:
  added: []
  patterns:
    - "Buffer-and-consume on step_complete: renderBranch lazy-init map, renderStepComplete read+delete+append. Mirrors lastErr lifecycle (shallow-copy through WithAttrs/WithGroup; reset implicitly via consume rather than flow_start)."
    - "Live-path goroutine-owned mutable state: branchByIdx joins active/drawnLines/spinIdx as render-goroutine-only fields. No additional concurrency primitives — submit() ships immutable progressEvent values, applyEvent runs serially."
key-files:
  created: []
  modified:
    - "pkg/cli/progress.go (branchByIdx field + WithAttrs/WithGroup propagation)"
    - "pkg/cli/progress_static.go (renderBranch buffer-only; renderStepComplete suffix append)"
    - "pkg/cli/progress_live.go (branchByIdx field; case branch buffers; case step_complete appends suffix)"
    - "pkg/cli/progress_test.go (removed `branch then arrow` sub-case; added 3 new tests)"
    - "pkg/cli/progress_live_test.go (rewrote BranchArrowColor; added 2 new tests)"
decisions:
  - "branchByIdx propagation in WithAttrs/WithGroup uses shallow-copy by reference (map header) — matches the lastErr *failureContext pattern; for v1 single-flow-per-handler usage this is correct."
  - "Live-path branchByIdx eager-init in newLiveRenderer (vs lazy-init on the static path) — render goroutine owns it from t=0, no nil-check overhead in hot path."
  - "Buffer-only renderBranch: when no matching step_complete arrives, the branch entry is bounded by a single in-memory map entry per orphaned idx (no leak — fresh handler per workflow run; orphan is rare and harmless)."
  - "Suffix is always appended on step_complete — INCLUDING on status=err — which matches the static path's previous behavior of also rendering the standalone line above any err-completion. Edge case not under test in this plan; deferred unless a future use-case surfaces."
metrics:
  duration: "215s"
  completed: "2026-05-03"
  tasks: 2
  files_modified: 5
  files_created: 0
  commits: 2
---

# Quick 260503-qkk: Inline if_cond Branch Label

Compacted the `skytime run` demo output by inlining the if_cond's branch
decision (` → then` / ` → else`) onto the same line as its
step_complete row, dropping the standalone `     → then` /
`     → else` line. Applied identically to BOTH static (Bazel) and
live-block renderers via a shared buffer-and-append pattern. The
interpreter's `branch` slog event continues to fire — only the
display-layer semantics changed.

## Tasks

| # | Type | Commit | Description |
|---|------|--------|-------------|
| 1 | RED — test | b58bffc | Pin inline-suffix behavior across static + live paths (5 failing tests, 1 passing regression-pin) |
| 2 | GREEN — fix | 9d95b49 | Implement branch buffering + inline suffix on both renderers |

## Files Modified

| Path | Change |
|------|--------|
| `pkg/cli/progress.go` | Added `branchByIdx map[int64]string` field on `*progressHandler`; propagated via `WithAttrs` and `WithGroup` (shallow-copy, matches `lastErr` pattern) |
| `pkg/cli/progress_static.go` | `renderBranch` becomes buffer-only (lazy-init map; no Fprintln); `renderStepComplete` reads+deletes the buffered branch and appends ` <colorArrow(→)> <branch>` to the rendered line |
| `pkg/cli/progress_live.go` | Added `branchByIdx map[int64]string` field on `*liveRenderer` (eager-init in `newLiveRenderer`); `applyEvent` `case "branch":` now buffers without any Fprintf or clearRedrawRegion; `case "step_complete":` reads+deletes and appends ` <ansiYellow>→<ansiReset> <branch>` as a suffix to the existing Fprintf format string |
| `pkg/cli/progress_test.go` | Removed `branch then arrow` sub-case from `TestProgress_BazelFormat` (the standalone-line shape is no longer the contract). Added `TestProgress_BranchAppendsToStepComplete` (with then + else sub-tests), `TestProgress_OrphanBranchEvent_NoStandaloneLine`, `TestProgress_StepCompleteWithoutBufferedBranch_NoSuffix` |
| `pkg/cli/progress_live_test.go` | Rewrote `TestLiveBlock_BranchArrowColor` to drive a 2-event sequence (branch → step_complete) and assert the inline yellow → suffix on the ✓ + 1ms line. Added `TestLiveBlock_BranchAppendsToStepComplete` and `TestLiveBlock_OrphanBranchEvent_NoOutput` |

## Files Deliberately NOT Modified

| Path | Why |
|------|-----|
| `pkg/interpreter/walk_ifcond.go` | Interpreter contract preserved — the `branch` slog event still fires unchanged. Renderer-only fix; observability surface (slog stream) byte-identical for downstream consumers. |
| `pkg/interpreter/walk_ifcond_test.go` | Same — the `branch` event behavior is interpreter-contract; this plan only changes how the renderer interprets it. |

## Verification

| Command | Exit Code | Notes |
|---------|-----------|-------|
| `go test ./pkg/cli/... -run 'BranchAppendsToStepComplete\|OrphanBranchEvent\|StepCompleteWithoutBufferedBranch\|BranchArrowColor' -count=1` (post-RED, pre-GREEN) | non-zero | RED bar — 5 expected failures (BranchArrowColor, BranchAppendsToStepComplete×2, OrphanBranchEvent ×2). The `StepCompleteWithoutBufferedBranch_NoSuffix` regression-pin already passed. |
| `go test ./pkg/cli/... -count=1` (post-GREEN) | 0 | All pkg/cli tests green |
| `go test ./... -count=1` (post-GREEN) | 0 | Full repo regression sanity — every package green |
| `go build -o skytime ./cmd/skytime` (post-GREEN) | 0 | 33 MB Mach-O binary rebuilt |
| `git diff HEAD~2 HEAD -- pkg/interpreter/` | empty | Interpreter contract preserved |

## Demo

```
$ ./skytime run examples/skeleton/simple_check.star --flow simple_check --input '{"repo":"octocat/Hello-World"}'
[skytime] flow simple_check  3 steps  starting
[1/3] step                Get repo octocat/Hello-World
     ✓ 70ms  status=200
[2/3] script              health
     ✓ 0ms
[3/3] if_cond             cond
  [3a/3a] step                Get branches for octocat/Hello-World
       ✓ 23ms  status=200
     ✓ 23ms   → then         ← inline suffix on the if_cond's completion row
[skytime] flow complete  3/3 steps  total 93ms
```

The previously-standalone `     → then` line between the if_cond row and
the inner step is gone; the branch decision now rides on the if_cond's
own `✓ 23ms` line.

## Behavior Summary

The renderer maintains a per-flow `branchByIdx map[int64]string` keyed by
step idx. The interpreter's emit-order is **branch → step_complete**
(deferred step_complete fires after walkBody returns), so:

1. **branch event arrives:** `renderBranch` (static) / `case "branch"`
   (live) lazy-init the map and store `branch` keyed by `idx`. Emits no
   output.
2. **step_complete arrives for the same idx:** the renderer formats the
   completion line normally, then looks up `branchByIdx[idx]` — if
   present, deletes the entry and appends ` <ansiYellow>→<ansiReset>
   <branch>` as a suffix.

Edge cases pinned by tests:
- **Orphan branch** (no matching step_complete): zero output.
- **step_complete without buffered branch:** existing format verbatim, no
  suffix.
- **err-status step_complete with buffered branch:** suffix still
  appended (matches static path's prior behavior of rendering the
  standalone line above err completions).
- **Color parity:** `→` glyph wrapped in `ansiYellow` + `ansiReset` on
  both paths; branch name (`then`/`else`) is plain text.

## Deviations from Plan

None. Plan executed exactly as written:
- Both atomic commits used the verbatim commit messages from the plan.
- pkg/interpreter/ untouched.
- No new ANSI constants, no new dependencies.
- Both renderers (static + live) inline the suffix; standalone line
  eliminated on both paths.

## Self-Check: PASSED

- [x] Task 1 RED commit `b58bffc` exists in git log
- [x] Task 2 GREEN commit `9d95b49` exists in git log
- [x] `pkg/cli/progress.go` modified (branchByIdx field + WithAttrs/WithGroup propagation)
- [x] `pkg/cli/progress_static.go` modified (renderBranch buffer-only; renderStepComplete suffix append)
- [x] `pkg/cli/progress_live.go` modified (branchByIdx field; case branch buffers; case step_complete suffix)
- [x] `pkg/cli/progress_test.go` modified (sub-case removed; 3 new tests added)
- [x] `pkg/cli/progress_live_test.go` modified (BranchArrowColor rewritten; 2 new tests added)
- [x] `pkg/interpreter/walk_ifcond.go` unchanged (`git diff HEAD~2 HEAD -- pkg/interpreter/` empty)
- [x] `go test ./pkg/cli/... -count=1` exits 0
- [x] `go test ./... -count=1` exits 0
- [x] `go build -o skytime ./cmd/skytime` exits 0; binary present
- [x] Demo confirms inline `→ then` rendering on simple_check.star
