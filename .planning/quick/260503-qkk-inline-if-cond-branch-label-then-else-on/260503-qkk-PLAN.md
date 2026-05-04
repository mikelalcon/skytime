---
phase: quick-260503-qkk
plan: 01
type: tdd
wave: 1
depends_on: []
files_modified:
  - pkg/cli/progress.go
  - pkg/cli/progress_static.go
  - pkg/cli/progress_live.go
  - pkg/cli/progress_test.go
  - pkg/cli/progress_live_test.go
autonomous: true
requirements:
  - CLI-06
  - CLI-07
must_haves:
  truths:
    - "if_cond's step_complete line ends with the branch suffix (` → then` or ` → else`) when the renderer received a matching branch event"
    - "the standalone `     → then` / `     → else` line no longer appears on either the static path or the live path"
    - "the `→` glyph in the inline suffix is wrapped in ansiYellow + ansiReset (color parity with q9p)"
    - "the branch-name word (`then` / `else`) is NOT color-wrapped (matches prior standalone-line treatment)"
    - "if a branch event fires but no matching step_complete arrives, no standalone branch line is rendered (defensive)"
    - "if a step_complete fires for an idx with no buffered branch, the suffix is NOT appended (existing rendering preserved verbatim)"
    - "previously-passing live-block tests (banner color, completed-row marker color, flow_failed banner, truncation, spinner cadence, no-flicker) remain green"
    - "go test ./pkg/cli/... -count=1 exits 0"
    - "go build -o skytime ./cmd/skytime exits 0 — the rebuilt binary makes the inline rendering observable end-to-end"
    - "pkg/interpreter/walk_ifcond.go is unchanged — the interpreter's `branch` slog event continues to fire (public observability surface preserved)"
  artifacts:
    - path: "pkg/cli/progress.go"
      provides: "*progressHandler.branchByIdx map[int64]string field (lazy-init); shallow-copied through WithAttrs/WithGroup"
      contains: "branchByIdx"
    - path: "pkg/cli/progress_static.go"
      provides: "renderBranch buffers (no output); renderStepComplete reads+deletes buffered branch and appends ` → <name>` suffix with colored arrow"
      contains: "branchByIdx"
    - path: "pkg/cli/progress_live.go"
      provides: "*liveRenderer.branchByIdx map[int64]string field; case \"branch\" buffers; case \"step_complete\" reads+deletes and appends colored ` → <name>` suffix"
      contains: "branchByIdx"
    - path: "pkg/cli/progress_test.go"
      provides: "Updated branch sub-cases (assert NO standalone line) + TestProgress_BranchAppendsToStepComplete + TestProgress_OrphanBranchEvent_NoStandaloneLine"
      contains: "BranchAppendsToStepComplete"
    - path: "pkg/cli/progress_live_test.go"
      provides: "Updated TestLiveBlock_BranchArrowColor (color asserted on suffix of step_complete line) + TestLiveBlock_BranchAppendsToStepComplete"
      contains: "BranchAppendsToStepComplete"
  key_links:
    - from: "pkg/cli/progress_static.go::renderBranch"
      to: "*progressHandler.branchByIdx[idx]"
      via: "lazy-init map; renderBranch becomes buffer-only (returns nil with no output)"
      pattern: "branchByIdx\\[.*idx.*\\]\\s*="
    - from: "pkg/cli/progress_static.go::renderStepComplete"
      to: "*progressHandler.branchByIdx[idx]"
      via: "read + delete + append ` <ansiYellow>→<ansiReset> <branch>` to printed line"
      pattern: "delete\\(.*branchByIdx.*\\)"
    - from: "pkg/cli/progress_live.go::applyEvent case \"branch\""
      to: "*liveRenderer.branchByIdx[ev.Idx]"
      via: "buffers ev.Branch; emits NO Fprintf"
      pattern: "branchByIdx\\[ev\\.Idx\\]"
    - from: "pkg/cli/progress_live.go::applyEvent case \"step_complete\""
      to: "*liveRenderer.branchByIdx[ev.Idx]"
      via: "lookup + delete + append ` %s→%s %s` (ansiYellow/ansiReset/branch) to the Fprintf format"
      pattern: "delete\\(.*branchByIdx.*\\)"
    - from: "pkg/interpreter/walk_ifcond.go"
      to: "logger.Info(\"skytime\", \"event\", \"branch\", ...)"
      via: "UNCHANGED — branch event continues to fire; renderer-only semantic change"
      pattern: "\"event\", \"branch\""
---

<objective>
Inline the if_cond branch label (` → then` or ` → else`) onto the same line as the if_cond's step_complete output, dropping the standalone `     → then` / `     → else` line. Apply identically to BOTH the static (Bazel-style) renderer and the live-block renderer. The interpreter's `branch` slog event keeps firing — only the renderer's display behavior changes.

Purpose: Compact the demo output. Post-q9p the standalone branch line wastes a row between the if_cond completion and the next step; users want the branch decision attached to the line that announces it. Renderer-only fix preserves the public observability surface (slog stream consumers see the same event sequence).

Output:
- pkg/cli/progress.go: *progressHandler gains `branchByIdx map[int64]string`; propagated by WithAttrs/WithGroup (shallow copy, matches lastErr pattern).
- pkg/cli/progress_static.go: renderBranch buffers and emits nothing; renderStepComplete reads+deletes the buffered branch (if any) and appends ` <ansiYellow>→<ansiReset> <branch>` to the rendered line.
- pkg/cli/progress_live.go: *liveRenderer gains `branchByIdx map[int64]string`; applyEvent case "branch" buffers and emits no output; case "step_complete" reads+deletes and appends ` %s→%s %s` (ansiYellow/ansiReset/branch) to the existing Fprintf.
- pkg/cli/progress_test.go + pkg/cli/progress_live_test.go: updated existing branch tests + 2 new tests + 1 defensive orphan test, all asserting inline behavior.
- ./skytime rebuilt so the user can re-run the demo.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@.planning/STATE.md

@pkg/cli/progress.go
@pkg/cli/progress_static.go
@pkg/cli/progress_live.go
@pkg/cli/progress_test.go
@pkg/cli/progress_live_test.go
@pkg/interpreter/walk_ifcond.go
@examples/skeleton/simple_check.star

<interfaces>
<!-- Key existing types/state the executor needs. Extracted from pkg/cli. -->
<!-- Use these directly — no codebase exploration needed. -->

From pkg/cli/progress.go (existing):

```go
type progressHandler struct {
    wrapped     slog.Handler
    out         io.Writer
    ttyKnown    bool
    tty         bool
    verbose     bool
    lastErr     *failureContext
    live        *liveRenderer
    liveOnce    sync.Once
    // NEW (this plan):
    // branchByIdx map[int64]string  // keyed by step idx; lazy-init on first store
}

type progressEvent struct {
    Kind       string  // "flow_start" | "step_dispatch" | "step_complete" | "branch" | "flow_complete" | "raw"
    FlowName   string
    StepCount  int64
    Idx        int64
    Total      int64
    KindAttr   string
    Label      string
    Status     string
    Summary    string
    DurationMs int64
    OkCount    int64
    ErrCount   int64
    TotalMs    int64
    Branch     string
    Path       string
    Raw        string
}

// Existing helper used by both renderers — tests use it to strip ANSI for content asserts:
func stripAnsiTest(s string) string  // defined in progress_testutil_test.go
```

From pkg/cli/progress_static.go (existing — current standalone-line shape, to be REPLACED with buffer-only):

```go
// CURRENT (will become buffer-only):
func (p *progressHandler) renderBranch(a attrMap) error {
    branch := a.str("branch")
    path := a.str("path")
    idx := a.int("idx")
    indent := "     "
    if isNestedPath(idx, path) {
        indent = "       "
    }
    line := fmt.Sprintf("%s%s %s", indent, p.colorArrow("→"), branch)
    return p.println(line)
}

// CURRENT renderStepComplete signature (Fprintf format string lives here):
//   line := fmt.Sprintf("%s%s %dms  %s", indent, marker, dur, summary)
// NEW: append ` <ansiYellow>→<ansiReset> <branch>` (via p.colorArrow) when branchByIdx[idx] is non-empty.
```

From pkg/cli/progress_live.go (existing case bodies — current standalone-line shape):

```go
case "branch":
    // Quick 260503-q9p: wrap → arrow in yellow, mirroring the
    // static path's colorArrow choice.
    r.clearRedrawRegion()
    fmt.Fprintf(r.out, "     %s→%s %s\n", ansiYellow, ansiReset, ev.Branch)

case "step_complete":
    r.clearRedrawRegion()
    for i, s := range r.active {
        if s.Idx == ev.Idx {
            r.active = append(r.active[:i], r.active[i+1:]...)
            break
        }
    }
    marker := ansiGreen + "✓" + ansiReset
    if ev.Status == "err" {
        marker = ansiRed + "✗" + ansiReset
    }
    fmt.Fprintf(r.out, "%s[%d/%d]%s %sstep%s  %s %dms  %s\n",
        ansiBrightCyan, ev.Idx, ev.Total, ansiReset,
        ansiBrightWhite, ansiReset,
        marker, ev.DurationMs, ev.Summary)
```

From pkg/cli/progress_static.go (existing color helpers — REUSE, no new constants):

```go
const (
    ansiReset       = "\x1b[0m"
    ansiYellow      = "\x1b[33m"
    // ... others unchanged
)

func (p *progressHandler) colorArrow(s string) string  // wraps in ansiYellow on TTY, plain on non-TTY
```

From pkg/interpreter/walk_ifcond.go (UNCHANGED — for context only):

```go
logger.Info("skytime",
    "event", "branch",
    "idx", parentIdx,         // <-- the idx the renderer keys the buffer on
    "path", parentPath,
    "branch", branchName,     // "then" | "else"
)
// Note: the branch event fires AFTER the if_cond's step_complete defer is queued
// but BEFORE the deferred step_complete actually emits — the defer runs on function
// return, AFTER walkBody completes. So in practice the branch event arrives BEFORE
// the if_cond's step_complete event. Buffer-then-consume on step_complete is correct.
```
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: RED — pin inline-suffix behavior across static + live paths</name>
  <files>pkg/cli/progress_test.go, pkg/cli/progress_live_test.go</files>
  <behavior>
    All tests below must FAIL against the current code (which still emits a standalone branch line). They must PASS after Task 2 lands. Implement EXACTLY these assertions — no extra coverage drift:

    --- pkg/cli/progress_test.go ---

    1. UPDATE the existing `branch then arrow` sub-case in `TestProgress_BazelFormat` (line ~94):
       - REMOVE this case from the table entirely. The standalone branch line is no longer emitted, so a single-event call to the renderer with `event=branch` produces no output and `Contains` assertions for "→" and "then" would fail spuriously. Replace its coverage by the new tests below.

    2. ADD `TestProgress_BranchAppendsToStepComplete(t *testing.T)`:
       - Drive a 3-event sequence via `slog.New(handler).LogAttrs(...)`:
         a. `event=step_dispatch` for the if_cond: `idx=3, total=3, kind="if_cond", label="ctx.health", path="3"`
         b. `event=branch` for the same idx: `idx=3, path="3", branch="then"`
         c. `event=step_complete` for the same idx: `idx=3, total=3, kind="if_cond", path="3", status="ok", duration_ms=1, summary=""`
       - Use `newProgressHandler(passthrough, &progressOut)` (non-TTY = bytes.Buffer; no ANSI in output).
       - Assert `progressOut.String()`:
         * MUST contain `"✓ 1ms"` (existing if_cond completion marker + duration).
         * MUST contain `"→ then"` (the inline suffix; no leading spaces required since color-helpers strip on non-TTY).
         * MUST NOT contain `"     → then"` with the 5-space indent prefix (the standalone-line shape from the old renderBranch).
         * Splitting by `"\n"`: NO line in the output equals `"     → then"` exactly (defensive).
         * Splitting by `"\n"`: the line containing `"✓ 1ms"` MUST end with `"→ then"` (after trimming trailing whitespace) — this pins "inline" rather than "on a separate adjacent line".

    3. ADD a sibling sub-table-style assertion path for `branch="else"` — minimal: extend `TestProgress_BranchAppendsToStepComplete` with a second sub-test (or repeat the helper) that drives `branch="else"` and asserts `"→ else"` on the same step_complete line. Keep it tight — one sub-test per branch name.

    4. ADD `TestProgress_OrphanBranchEvent_NoStandaloneLine(t *testing.T)`:
       - Drive a 1-event sequence: `event=branch` with `idx=99, path="99", branch="then"`.
       - Use the non-TTY handler.
       - Assert `progressOut.String()` is the empty string (no output at all). This proves renderBranch is buffer-only and never emits when no matching step_complete arrives.

    5. ADD `TestProgress_StepCompleteWithoutBufferedBranch_NoSuffix(t *testing.T)`:
       - Drive a 1-event sequence: `event=step_complete` with `idx=1, total=1, kind="step", path="1", status="ok", duration_ms=42, summary="status=200"`. (No prior branch event.)
       - Assert `progressOut.String()`:
         * MUST contain `"✓ 42ms  status=200"` (existing format preserved verbatim).
         * MUST NOT contain `"→"` (no suffix appended when buffer is empty).

    --- pkg/cli/progress_live_test.go ---

    6. UPDATE `TestLiveBlock_BranchArrowColor` (line ~318):
       - Currently sends a single `Kind: "branch"` event and asserts `ansiYellow` + stripped `"→ then"`. After this change, a lone branch event produces NO output (it just buffers). Rewrite the test to drive a 2-event sequence:
         a. `Kind: "branch", Idx: 3, Branch: "then"`
         b. `Kind: "step_complete", Idx: 3, Total: 3, Status: "ok", DurationMs: 1, Summary: ""`
       - Wait `150 * time.Millisecond` between submits and after the second submit (existing pattern).
       - Call `r.Close()`.
       - Assert on raw output:
         * `Contains(raw, ansiYellow)` — the suffix `→` must still be yellow.
         * `Contains(raw, ansiReset)` — every wrap closed.
       - Assert on stripped output:
         * `Contains(stripped, "→ then")` — suffix glyph + name survive.
         * Splitting by `"\n"`: the first non-empty rendered LINE that contains `"→ then"` must ALSO contain `"✓"` and `"1ms"` (proves inline).
         * Splitting by `"\n"`: NO line equals `"     → then"` (the old standalone shape) after stripping ANSI.

    7. ADD `TestLiveBlock_BranchAppendsToStepComplete(t *testing.T)`:
       - Same shape as #6 but with explicit step counter assertions: send branch (idx=3, "then") then step_complete (idx=3, total=3, status=ok, duration_ms=1ms, summary="").
       - Assert stripped output contains `"[3/3]"` AND `"step  ✓ 1ms"` AND `"→ then"`, all on the same line (split by `\n`, find the line, multiple `Contains` on that line string).

    8. ADD `TestLiveBlock_OrphanBranchEvent_NoOutput(t *testing.T)`:
       - Send a single `Kind: "branch", Idx: 99, Branch: "then"` event.
       - Wait `150 * time.Millisecond`.
       - Call `r.Close()`.
       - Assert that the stripped output contains NEITHER `"→"` NOR `"then"` — defensive parity with the static-path orphan test.
       - The output may contain ANSI cursor-show / cursor-hide sequences (Close emits `\x1b[?25h`); that's fine. The CONTENT (post-strip) must not include the branch glyph or name.

    Verify-RED expectation: `go test ./pkg/cli/... -run 'BranchAppendsToStepComplete|OrphanBranchEvent|StepCompleteWithoutBufferedBranch|BranchArrowColor' -count=1` MUST exit non-zero before Task 2 lands. The existing `TestLiveBlock_BranchArrowColor` will newly fail; the new tests will fail-because-undefined-or-failing-assertions. This is the RED bar.

    Tests already present (preserve unchanged): TestLiveBlock_AnsiSequencesEmitted, TestLiveBlock_SpinnerCadence, TestLiveBlock_TruncationAtTen, TestLiveBlock_FinalizeRow, TestLiveBlock_FlushFinal, TestLiveBlock_FailedStepRendersX, TestLiveBlock_NoFlickerOnRapidEvents, TestLiveBlock_BannerHasColor, TestLiveBlock_CompletedRowMarkerColor_Ok, TestLiveBlock_CompletedRowMarkerColor_Err, TestLiveBlock_FlowFailedHasRedFailedMarker, TestLiveBlock_FlowCompleteBannerColored, all TestProgress_BazelFormat sub-cases except the removed `branch then arrow`, TestProgress_FlowFailed*, TestProgress_LastErrResetsOnFlowStart, TestProgress_PassthroughOnNonSkytimeRecord, TestProgress_NestedStepPath, TestProgress_StaticPath_*, TestProgress_LivePath*, TestProgress_LiveBlock_*. None of these should change.
  </behavior>
  <action>
    Implement the test changes per &lt;behavior&gt; above. Use the existing `safeBuffer` (from progress_testutil_test.go) for live-block tests and `bytes.Buffer` for static-path tests. Use `slog.LevelInfo` and the `slog.NewTextHandler` passthrough wiring already present in progress_test.go.

    Color constants (ansiYellow, ansiReset, ansiGreen, ansiRed, ansiBrightCyan, ansiBrightWhite) are package-private in progress_static.go — reference directly, no import.

    Run go test to confirm RED:
    ```
    go test ./pkg/cli/... -run 'BranchAppendsToStepComplete|OrphanBranchEvent|StepCompleteWithoutBufferedBranch|BranchArrowColor' -count=1
    ```
    Must exit non-zero. If it accidentally passes, the test isn't biting — review assertions.

    DO NOT modify pkg/cli/progress.go, pkg/cli/progress_static.go, pkg/cli/progress_live.go, or pkg/interpreter/walk_ifcond.go in this task. Tests-only.

    Commit with message: `test(260503-qkk): branch label inlines onto step_complete (RED)`
    Use HEREDOC with Co-Authored-By trailer per project commit convention.
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero &amp;&amp; go test ./pkg/cli/... -run 'BranchAppendsToStepComplete|OrphanBranchEvent|StepCompleteWithoutBufferedBranch|BranchArrowColor' -count=1; test $? -ne 0</automated>
  </verify>
  <done>
    - pkg/cli/progress_test.go: `branch then arrow` sub-case removed from TestProgress_BazelFormat; TestProgress_BranchAppendsToStepComplete (covering both `then` and `else`), TestProgress_OrphanBranchEvent_NoStandaloneLine, TestProgress_StepCompleteWithoutBufferedBranch_NoSuffix added.
    - pkg/cli/progress_live_test.go: TestLiveBlock_BranchArrowColor rewritten to assert inline-suffix behavior; TestLiveBlock_BranchAppendsToStepComplete and TestLiveBlock_OrphanBranchEvent_NoOutput added.
    - All other tests unmodified.
    - `go test ./pkg/cli/... -count=1` exits non-zero (specifically the new + rewritten tests fail).
    - All other test files unchanged; no source under pkg/cli/ or pkg/interpreter/ modified.
    - Commit landed with the RED message.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: GREEN — implement branch buffering + inline suffix on both renderers</name>
  <files>pkg/cli/progress.go, pkg/cli/progress_static.go, pkg/cli/progress_live.go</files>
  <behavior>
    All tests added/rewritten in Task 1 must now PASS. All previously-passing pkg/cli tests must remain green. The interpreter package is untouched.

    Specifically:

    - `TestProgress_BranchAppendsToStepComplete` (then + else): on the same step_complete output line, the suffix ` → then` (or ` → else`) appears after the existing summary. On non-TTY there are no ANSI codes; on TTY the `→` is wrapped in ansiYellow + ansiReset (via `p.colorArrow("→")`).
    - `TestProgress_OrphanBranchEvent_NoStandaloneLine`: a lone branch event produces zero bytes of output.
    - `TestProgress_StepCompleteWithoutBufferedBranch_NoSuffix`: a step_complete with no prior buffered branch emits the existing format unchanged (no trailing `→`).
    - `TestLiveBlock_BranchArrowColor` (rewritten): the inline suffix carries `ansiYellow` + `ansiReset`; the line containing `→ then` also contains `✓` and `1ms`.
    - `TestLiveBlock_BranchAppendsToStepComplete`: stripped output line contains `[3/3]`, `step`, `✓`, `1ms`, AND `→ then` — all on one rendered line.
    - `TestLiveBlock_OrphanBranchEvent_NoOutput`: stripped output contains neither `→` nor `then`.
    - The full `go test ./pkg/cli/... -count=1` suite exits 0.
  </behavior>
  <action>
    Implementation steps (apply all three files, then build):

    --- pkg/cli/progress.go ---

    1. Add a new field to *progressHandler (after `lastErr` or `live`/`liveOnce`, group-it doesn't matter):
       ```go
       // branchByIdx buffers `event=branch` records keyed by step idx. The
       // entry is read+deleted by renderStepComplete for the same idx, which
       // appends ` <colorArrow(→)> <branch>` to the rendered line. Lazy-init
       // on first store. Quick 260503-qkk: inlines if_cond branch label onto
       // the if_cond's step_complete line; standalone branch line removed.
       branchByIdx map[int64]string
       ```
    2. Propagate `branchByIdx` through `WithAttrs` and `WithGroup` shallow-copy (matches the existing `lastErr` pattern). Both helpers return a new *progressHandler — add `branchByIdx: p.branchByIdx,` to both struct literals. The map itself is shared by reference; for the v1 single-flow-per-handler usage this is safe (matches lastErr semantics).
    3. Do NOT touch buildProgressEvent, Enabled, Handle, Close, isTTY, useLiveBlock, hasAttr, collectAttrs, attrMap accessors, progressEvent, progressHandlerOptions, newProgressHandler, newProgressHandlerWithOptions.

    --- pkg/cli/progress_static.go ---

    4. Replace renderBranch with a buffer-only implementation:
       ```go
       // renderBranch buffers the branch name keyed by idx; renderStepComplete
       // for the same idx reads+deletes the buffer and appends a colored
       // ` → <branch>` suffix. Returns nil with NO output. Quick 260503-qkk.
       func (p *progressHandler) renderBranch(a attrMap) error {
           branch := a.str("branch")
           idx := a.int("idx")
           if branch == "" {
               return nil // defensive: empty-branch event is a no-op
           }
           if p.branchByIdx == nil {
               p.branchByIdx = make(map[int64]string)
           }
           p.branchByIdx[idx] = branch
           return nil
       }
       ```
    5. Update renderStepComplete: after computing the existing `line` string but BEFORE `p.println(line)`, append the suffix when a buffered branch exists:
       ```go
       if p.branchByIdx != nil {
           if branch, ok := p.branchByIdx[idx]; ok && branch != "" {
               delete(p.branchByIdx, idx)
               line = fmt.Sprintf("%s %s %s", line, p.colorArrow("→"), branch)
           } else if ok {
               // empty-string entry from a defensive earlier write — drop it
               delete(p.branchByIdx, idx)
           }
       }
       ```
       Place this AFTER the existing `line := fmt.Sprintf(...)` and BEFORE `return p.println(line)`. Do NOT change the indent logic, the marker logic, the lastErr capture, or any other branch of renderStepComplete.

    --- pkg/cli/progress_live.go ---

    6. Add a new field to *liveRenderer (group with `active`/`drawnLines`/`spinIdx`):
       ```go
       // branchByIdx buffers branch events keyed by step idx; consumed by
       // case "step_complete" which appends ` → <branch>` (yellow arrow) to
       // the rendered Fprintf line. Owned by the render goroutine. Quick
       // 260503-qkk.
       branchByIdx map[int64]string
       ```
    7. Initialize `branchByIdx: make(map[int64]string)` in `newLiveRenderer` (eager init is fine here — single-goroutine ownership, no concurrent access).
    8. Update `applyEvent` `case "branch":` to buffer and emit nothing:
       ```go
       case "branch":
           // Quick 260503-qkk: branch is a buffer-only signal; the suffix is
           // emitted on the matching step_complete event.
           if ev.Branch != "" {
               r.branchByIdx[ev.Idx] = ev.Branch
           }
       ```
       REMOVE the `r.clearRedrawRegion()` and `fmt.Fprintf` calls from this case. The render loop's tick will continue to redraw the active region; no visible change here.
    9. Update `applyEvent` `case "step_complete":` to read+delete and append suffix to the Fprintf:
       - Compute `suffix := ""` BEFORE the existing `fmt.Fprintf(r.out, ...)` call.
       - Add the lookup:
         ```go
         if branch, ok := r.branchByIdx[ev.Idx]; ok {
             delete(r.branchByIdx, ev.Idx)
             if branch != "" {
                 suffix = fmt.Sprintf(" %s→%s %s", ansiYellow, ansiReset, branch)
             }
         }
         ```
       - Change the Fprintf format string from:
         ```
         "%s[%d/%d]%s %sstep%s  %s %dms  %s\n"
         ```
         to:
         ```
         "%s[%d/%d]%s %sstep%s  %s %dms  %s%s\n"
         ```
         appending `suffix` as the final format argument. The existing arg ordering (counter+kind+marker+dur+summary) is preserved verbatim; the new `%s` consumes `suffix`.
       - Place the lookup AFTER the active-row removal loop and BEFORE the Fprintf, so an err-status step's marker logic remains correct (suffix appears on err completions too — matches the static path's behavior).

    Then run:
    ```
    go test ./pkg/cli/... -count=1                          # full pkg/cli suite green
    go build -o skytime ./cmd/skytime                       # binary rebuilds clean
    ```

    DO NOT:
    - Modify pkg/interpreter/walk_ifcond.go or any other interpreter file.
    - Add new ANSI constants — reuse ansiYellow / ansiReset.
    - Touch the live-renderer's redraw / clearRedrawRegion / spinner / truncation / flushFinal logic.
    - Change the progressEvent struct shape (Branch field already present).
    - Add concurrency primitives — the static path is serial under one Handle() call, and the live path's branchByIdx is owned exclusively by the render goroutine (matches r.active / r.drawnLines / r.spinIdx ownership).
    - Emit a branch suffix when status="err" AND ev.Summary already contains a `→` substring (no, just always append when the buffer holds a non-empty entry — the err-with-branch case is rare and harmless).

    Commit with message: `fix(260503-qkk): inline if_cond branch label onto step_complete line`
    Use HEREDOC with Co-Authored-By trailer per project commit convention.
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero &amp;&amp; go test ./pkg/cli/... -count=1 &amp;&amp; go build -o skytime ./cmd/skytime</automated>
  </verify>
  <done>
    - pkg/cli/progress.go: *progressHandler.branchByIdx field added; propagated via WithAttrs and WithGroup.
    - pkg/cli/progress_static.go: renderBranch is buffer-only (no output); renderStepComplete reads+deletes buffered branch and appends ` <colorArrow(→)> <branch>` suffix when present.
    - pkg/cli/progress_live.go: *liveRenderer.branchByIdx field added (eager-init in newLiveRenderer); applyEvent case "branch" buffers without Fprintf; case "step_complete" Fprintf appends ` <ansiYellow>→<ansiReset> <branch>` suffix when buffer holds an entry for ev.Idx.
    - pkg/interpreter/walk_ifcond.go UNCHANGED — `git diff` shows zero bytes touched in pkg/interpreter/.
    - `go test ./pkg/cli/... -count=1` exits 0.
    - `go build -o skytime ./cmd/skytime` exits 0; ./skytime binary present + executable.
    - Commit landed with the GREEN message.
    - Manual demo (post-rebuild): running `./skytime run examples/skeleton/simple_check.star --flow simple_check --input '{"repo":"octocat/Hello-World"}'` shows the if_cond's completion line ending with ` → then` (or ` → else`) and NO standalone arrow line between the if_cond and its child step.
  </done>
</task>

</tasks>

<verification>
After both tasks land:

1. `cd /Users/mikel/dev/ai/temporero && go test ./pkg/cli/... -count=1` → exit 0, all tests green (existing + 4 new + 1 rewritten).
2. `cd /Users/mikel/dev/ai/temporero && go test ./... -count=1` → exit 0, no regression elsewhere.
3. `cd /Users/mikel/dev/ai/temporero && go build -o skytime ./cmd/skytime` → exit 0, binary rebuilt.
4. `git log --oneline -3` → shows two new commits, RED before GREEN, both with `260503-qkk` in subject.
5. `git diff HEAD~2 HEAD -- pkg/interpreter/walk_ifcond.go` → empty (interpreter untouched).
6. `git diff HEAD~2 HEAD -- pkg/interpreter/` → empty (no interpreter file touched).
7. Visual: rerun `./skytime run examples/skeleton/simple_check.star --flow simple_check --input '{"repo":"octocat/Hello-World"}'`. Output shows the if_cond row's `✓` line ending with ` → then` (yellow arrow on TTY) and the previously-standalone `     → then` line is gone. Confirms must_have truth #1 + #2 + #3 end-to-end.

Edge-case spot checks (already covered by tests, listed for traceability):
- Orphan branch event (no matching step_complete): no output. (TestProgress_OrphanBranchEvent_NoStandaloneLine + TestLiveBlock_OrphanBranchEvent_NoOutput)
- step_complete without prior branch: format unchanged, no suffix. (TestProgress_StepCompleteWithoutBufferedBranch_NoSuffix)
- branch="else": suffix renders as `→ else`. (TestProgress_BranchAppendsToStepComplete sub-test)
- Color parity: live + static both wrap `→` in ansiYellow + ansiReset. (TestLiveBlock_BranchArrowColor + non-TTY assertions in static tests)
- Multi-flow handler reuse: lastErr already resets on flow_start (existing test); branchByIdx auto-clears on consume so leakage across flows is bounded by an unconsumed orphan only — acceptable for v1, no test required (the orphan test pins zero output for unconsumed entries).
</verification>

<success_criteria>
- [ ] RED commit: tests pin the inline-suffix expectation; new + rewritten tests fail before any source change.
- [ ] GREEN commit: source change makes RED tests pass without breaking any previously-green test in pkg/cli or anywhere else in the repo.
- [ ] pkg/interpreter/walk_ifcond.go and pkg/interpreter/walk_ifcond_test.go unchanged (interpreter contract preserved).
- [ ] No new files; no new dependencies; no new ANSI constants.
- [ ] Both renderers (static + live) inline the branch suffix; standalone branch line eliminated on both paths.
- [ ] `→` glyph on the suffix wrapped in ansiYellow + ansiReset (color parity with q9p).
- [ ] Branch name (`then` / `else`) is plain text — not color-wrapped.
- [ ] go test ./pkg/cli/... -count=1 exits 0 after Task 2.
- [ ] go build -o skytime ./cmd/skytime exits 0 after Task 2; ./skytime binary present.
- [ ] Manual demo confirms inline rendering on the live block when running against simple_check.star with valid `--input`.
</success_criteria>

<output>
After completion, create `.planning/quick/260503-qkk-inline-if-cond-branch-label-then-else-on/260503-qkk-SUMMARY.md` capturing:

- The two atomic commits (RED + GREEN) with hashes.
- Files modified: pkg/cli/progress.go, pkg/cli/progress_static.go, pkg/cli/progress_live.go, pkg/cli/progress_test.go, pkg/cli/progress_live_test.go.
- Files deliberately NOT modified: pkg/interpreter/walk_ifcond.go, pkg/interpreter/walk_ifcond_test.go (interpreter contract preserved).
- Verification commands run + their exit codes.
- Demo command + a snippet of the resulting output showing the inline `→ then` on the if_cond completion line.
- Any deviations from this plan (expected: none).
</output>
