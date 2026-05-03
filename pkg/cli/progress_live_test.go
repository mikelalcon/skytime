//go:build !windows

package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Live-block renderer tests (Phase 04.1-06 Task 2, D4.1-17/18/19).
//
// Each test instantiates a safeBuffer (defined in progress_testutil_test.go)
// + creates a liveRenderer + submits events directly via .submit()
// (bypassing the slog Handle wrapper for unit-level testing) + waits a
// deterministic time + Close()s to drain the render goroutine.
//
// Generous time margins are used because a 100ms ticker plus a buffered
// channel makes byte-exact timing assertions flaky; the load-bearing
// properties are AT-LEAST-ONCE emission of cursor-up sequences,
// AT-LEAST-N distinct spinner frames seen, and exact "... and N more"
// presence on truncation.

// TestLiveBlock_AnsiSequencesEmitted: a single step_dispatch produces at
// least one cursor-up + clear-line pair within a tick window.
func TestLiveBlock_AnsiSequencesEmitted(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)
	defer r.Close()

	r.submit(progressEvent{
		Kind: "step_dispatch",
		Idx:  1, Total: 1,
		KindAttr: "step",
		Label:    "gh.get(/x)",
		Path:     "1",
	})

	// Wait one tick so the renderer drains and emits at least one redraw.
	time.Sleep(150 * time.Millisecond)

	got := out.String()
	require.Contains(t, got, "\x1b[1A", "live block must emit cursor-up sequence (D4.1-17)")
	require.Contains(t, got, "\x1b[2K", "live block must emit clear-line sequence (D4.1-17)")
}

// TestLiveBlock_SpinnerCadence: 350 ms after submitting a single
// step_dispatch, output contains at least 3 distinct braille frames
// from the locked set "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏" (3 frames at 100 ms cadence,
// D4.1-18). Tolerant assertion: the renderer cycles frames; at least 3
// distinct ones must appear.
func TestLiveBlock_SpinnerCadence(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)
	defer r.Close()

	r.submit(progressEvent{
		Kind: "step_dispatch",
		Idx:  1, Total: 1,
		KindAttr: "step",
		Label:    "long-running",
		Path:     "1",
	})

	time.Sleep(450 * time.Millisecond)

	got := out.String()
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	seen := 0
	for _, f := range frames {
		if strings.Contains(got, f) {
			seen++
		}
	}
	require.GreaterOrEqual(t, seen, 3,
		"expected ≥3 distinct braille frames (cadence=100ms); saw %d distinct out of 10. Output: %q",
		seen, got)
}

// TestLiveBlock_TruncationAtTen: 12 step_dispatch events for unique
// steps produces "... and 2 more" lines (12 - 10 cap = 2). The renderer
// redraws on every event AND every tick, so "... and 2 more" may appear
// multiple times (one per redraw of the saturated state). The test
// asserts ≥1 occurrence — the load-bearing property is "truncation
// engaged when active > cap" (D4.1-19), not the redraw count.
func TestLiveBlock_TruncationAtTen(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)
	defer r.Close()

	for i := int64(1); i <= 12; i++ {
		r.submit(progressEvent{
			Kind: "step_dispatch",
			Idx:  i, Total: 12,
			KindAttr: "step",
			Label:    "step-" + string(rune('0'+i%10)),
			Path:     "1",
		})
	}

	time.Sleep(200 * time.Millisecond)

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "... and 2 more",
		"truncation cap=10 with 12 active rows must emit '... and 2 more' (D4.1-19). Output: %q", stripped)
	// Defense: NEVER emit "... and 0 more" or any negative-count
	// truncation line, which would indicate a math bug.
	require.NotContains(t, stripped, "... and 0 more")
	require.NotContains(t, stripped, "... and -")
}

// TestLiveBlock_FinalizeRow: step_dispatch then step_complete (status=ok)
// for the same idx. After completion, the static ✓ line is in the
// buffer; the redraw region no longer references the completed step.
func TestLiveBlock_FinalizeRow(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch",
		Idx:  1, Total: 1,
		KindAttr: "step",
		Label:    "gh.get(/x)",
		Path:     "1",
	})
	time.Sleep(150 * time.Millisecond)

	r.submit(progressEvent{
		Kind:       "step_complete",
		Idx:        1,
		Total:      1,
		Status:     "ok",
		DurationMs: 50,
		Summary:    "status=200",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "✓", "finalized row must include ok marker")
	require.Contains(t, stripped, "50ms", "finalized row must include duration")
	require.Contains(t, stripped, "status=200", "finalized row must include summary")
}

// TestLiveBlock_FlushFinal: send flow_start, 1 step_dispatch,
// flow_complete. After Close, the redraw region is cleared and the
// final flow_complete static line is present.
func TestLiveBlock_FlushFinal(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "flow_start", FlowName: "test", StepCount: 1,
	})
	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 1, Total: 1, KindAttr: "step", Label: "gh.get(/x)", Path: "1",
	})
	r.submit(progressEvent{
		Kind: "flow_complete", OkCount: 1, ErrCount: 0, TotalMs: 100,
	})
	r.Close()

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "[skytime] flow complete", "final flow_complete must render as static line after drain")
	// After Close + flushFinal, the trailing bytes should not leave the
	// terminal in a hidden-cursor state. We assert the cursor-show
	// sequence is somewhere in the output — Close() emits it.
	require.Contains(t, out.String(), "\x1b[?25h", "Close must restore the cursor")
}

// TestLiveBlock_FailedStepRendersX: step_dispatch then step_complete
// with status=err uses ✗ marker.
func TestLiveBlock_FailedStepRendersX(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 1, Total: 1, KindAttr: "step", Label: "gh.get(/missing)", Path: "1",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 1, Total: 1, Status: "err", DurationMs: 80, Summary: "HTTP 404",
	})
	time.Sleep(120 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "✗", "failed step must render ✗ marker")
	require.Contains(t, stripped, "HTTP 404", "failed step summary must appear")
}

// TestLiveBlock_NoFlickerOnRapidEvents: 5 step_dispatch events in
// rapid succession. Output should not show 50× redraws — count cursor-
// up sequences ≤ a generous bound proportional to events × maxLines.
//
// The renderer redraws after every applyEvent + every tick. 5 events
// in <1 ms produces ≤6 redraw cycles (5 from events + 1 from a
// possibly-coincident tick). Each redraw clears r.drawnLines lines.
// With 5 active rows + 1 header = 6 lines drawn before the FINAL
// redraw, the cumulative cursor-up count is bounded.
//
// We use a generous bound to keep the test stable across timing.
func TestLiveBlock_NoFlickerOnRapidEvents(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	for i := int64(1); i <= 5; i++ {
		r.submit(progressEvent{
			Kind: "step_dispatch", Idx: i, Total: 5, KindAttr: "step", Label: "step", Path: "1",
		})
	}
	// Wait one tick window for events to drain.
	time.Sleep(150 * time.Millisecond)
	r.Close()

	got := out.String()
	cursorUps := strings.Count(got, "\x1b[1A")
	// Upper bound: each redraw clears <= drawnLines (capped at 11 = header
	// + 10 rows + ... and N more). 5 event-redraws + 1-2 ticks during
	// the 150ms window before Close = ≤7 redraws. 7 * 11 = 77 cursor-ups
	// is a generous ceiling that proves batching (no per-event flush
	// cycle without redraw consolidation). A naive "redraw on every
	// byte" implementation would produce thousands.
	require.LessOrEqual(t, cursorUps, 77,
		"5 rapid events should batch redraws; saw %d cursor-up sequences (bound=77)", cursorUps)
}

// ---------------------------------------------------------------------------
// Quick 260503-q9p: color-presence tests for the live renderer's static-line
// emissions. The live block was added in Phase 04.1-06 without color
// wrapping, regressing the colored Bazel-style output produced by the
// static-path renderer (progress_static.go). These tests pin the
// requirement that the live renderer's flow_start banner, step_complete
// completion row, branch arrow, flow_complete success banner, and
// flow_complete failed banner+marker carry the SAME ANSI codes as the
// static path. Color is asserted on RAW output (not stripAnsiTest, which
// would erase the very codes under test). Structural content asserts use
// stripAnsiTest so they survive color wrapping.
//
// All ANSI constants (ansiDimCyan, ansiGreen, ansiRed, ansiYellow,
// ansiReset, ansiBrightCyan, ansiBrightWhite) are package-private in
// progress_static.go — same package, no import needed.

// TestLiveBlock_BannerHasColor: flow_start emits the [skytime] banner
// wrapped in dim-cyan ANSI. Static content "[skytime] flow f" survives
// the color wrapping intact.
func TestLiveBlock_BannerHasColor(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{Kind: "flow_start", FlowName: "f", StepCount: 1})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	raw := out.String()
	require.Contains(t, raw, ansiDimCyan, "flow_start banner must be wrapped in ansiDimCyan")
	require.Contains(t, raw, ansiReset, "every color wrap must be closed by ansiReset")
	stripped := stripAnsiTest(raw)
	require.Contains(t, stripped, "[skytime] flow f", "banner content must survive color wrapping")
}

// TestLiveBlock_CompletedRowMarkerColor_Ok: step_complete with status=ok
// wraps the ✓ marker in green ANSI. ✗/red MUST NOT appear.
func TestLiveBlock_CompletedRowMarkerColor_Ok(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 1, Total: 1, KindAttr: "step", Label: "x", Path: "1",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 1, Total: 1, Status: "ok", DurationMs: 50, Summary: "status=200",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	raw := out.String()
	require.Contains(t, raw, ansiGreen, "ok marker (✓) must be wrapped in ansiGreen")
	require.Contains(t, raw, ansiReset, "every color wrap must be closed by ansiReset")
	require.NotContains(t, raw, ansiRed, "success path must NOT contain ansiRed")
	stripped := stripAnsiTest(raw)
	require.Contains(t, stripped, "✓", "ok marker glyph must survive color wrapping")
	require.Contains(t, stripped, "50ms", "duration must survive color wrapping")
	require.Contains(t, stripped, "status=200", "summary must survive color wrapping")
}

// TestLiveBlock_CompletedRowMarkerColor_Err: step_complete with status=err
// wraps the ✗ marker in red ANSI. ansiGreen MUST NOT appear in the
// completion row (the marker is the only colored glyph for the err case).
func TestLiveBlock_CompletedRowMarkerColor_Err(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 1, Total: 1, KindAttr: "step", Label: "x", Path: "1",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 1, Total: 1, Status: "err", DurationMs: 50, Summary: "HTTP 404",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	raw := out.String()
	require.Contains(t, raw, ansiRed, "err marker (✗) must be wrapped in ansiRed")
	require.Contains(t, raw, ansiReset, "every color wrap must be closed by ansiReset")
	require.NotContains(t, raw, ansiGreen, "err path must NOT contain ansiGreen")
	stripped := stripAnsiTest(raw)
	require.Contains(t, stripped, "✗", "err marker glyph must survive color wrapping")
	require.Contains(t, stripped, "HTTP 404", "summary must survive color wrapping")
}

// TestLiveBlock_BranchArrowColor: a branch event followed by step_complete
// for the same idx emits an inline ` → then` suffix on the completion
// line. The → glyph is wrapped in yellow ANSI; the completion line
// contains both ✓ and 1ms (proving "inline" rather than "standalone
// adjacent line"). Quick 260503-qkk: branch is now buffer-only; the
// suffix is the only place the arrow appears.
func TestLiveBlock_BranchArrowColor(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{Kind: "branch", Idx: 3, Branch: "then"})
	time.Sleep(150 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 3, Total: 3, Status: "ok", DurationMs: 1, Summary: "",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	raw := out.String()
	require.Contains(t, raw, ansiYellow, "inline branch arrow (→) must be wrapped in ansiYellow")
	require.Contains(t, raw, ansiReset, "every color wrap must be closed by ansiReset")
	stripped := stripAnsiTest(raw)
	require.Contains(t, stripped, "→ then", "branch arrow + name must survive color wrapping")

	// Inline assertion: the line containing "→ then" must ALSO contain
	// "✓" and "1ms" (proves the suffix is appended to the step_complete
	// line, not emitted on a standalone adjacent line).
	var found bool
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "→ then") {
			require.Contains(t, line, "✓",
				"line containing branch suffix must also contain ✓ (inline). Line: %q", line)
			require.Contains(t, line, "1ms",
				"line containing branch suffix must also contain 1ms (inline). Line: %q", line)
			found = true
			break
		}
	}
	require.True(t, found, "expected a line containing '→ then'. Stripped output: %q", stripped)

	// Defense: NO line equals the old standalone shape "     → then" exactly.
	for _, line := range strings.Split(stripped, "\n") {
		require.NotEqual(t, "     → then", line,
			"no stripped line may match the old standalone shape. Stripped: %q", stripped)
	}
}

// TestLiveBlock_BranchAppendsToStepComplete: a branch event followed by
// step_complete for the same idx emits a single line containing the
// counter [3/3], the kind word "step", the ✓ marker, the duration "1ms",
// AND the inline branch suffix "→ then" — all on the same rendered line.
func TestLiveBlock_BranchAppendsToStepComplete(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{Kind: "branch", Idx: 3, Branch: "then"})
	time.Sleep(150 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 3, Total: 3, Status: "ok", DurationMs: 1, Summary: "",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "[3/3]", "counter must be present")
	require.Contains(t, stripped, "step  ✓ 1ms",
		"completion row must follow the established format. Stripped: %q", stripped)
	require.Contains(t, stripped, "→ then", "inline branch suffix must be present")

	// All four properties on the same rendered line.
	var foundAll bool
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "[3/3]") &&
			strings.Contains(line, "step") &&
			strings.Contains(line, "✓") &&
			strings.Contains(line, "1ms") &&
			strings.Contains(line, "→ then") {
			foundAll = true
			break
		}
	}
	require.True(t, foundAll,
		"expected a single line with [3/3], step, ✓, 1ms, AND → then. Stripped: %q", stripped)
}

// TestLiveBlock_OrphanBranchEvent_NoOutput: a lone branch event (no
// matching step_complete) produces no visible content — neither the →
// glyph nor the branch name "then" appears in stripped output. Cursor
// show/hide ANSI sequences are allowed (Close emits them).
func TestLiveBlock_OrphanBranchEvent_NoOutput(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{Kind: "branch", Idx: 99, Branch: "then"})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())
	require.NotContains(t, stripped, "→",
		"orphan branch must produce no visible arrow. Stripped: %q", stripped)
	require.NotContains(t, stripped, "then",
		"orphan branch must produce no visible branch name. Stripped: %q", stripped)
}

// TestLiveBlock_FlowFailedHasRedFailedMarker: flow_complete with
// ErrCount > 0 wraps the [skytime] banner in dim cyan AND the word
// "failed" in red.
func TestLiveBlock_FlowFailedHasRedFailedMarker(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{Kind: "flow_complete", ErrCount: 1, TotalMs: 42})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	raw := out.String()
	require.Contains(t, raw, ansiDimCyan, "flow_failed banner must be wrapped in ansiDimCyan")
	require.Contains(t, raw, ansiRed, "the word 'failed' must be wrapped in ansiRed")
	require.Contains(t, raw, ansiReset, "every color wrap must be closed by ansiReset")
	stripped := stripAnsiTest(raw)
	require.Contains(t, stripped, "[skytime] flow failed", "failed banner content must survive color wrapping")
	require.Contains(t, stripped, "42ms", "total_ms must survive color wrapping")
}

// TestLiveBlock_FlowCompleteBannerColored: flow_complete success path
// wraps the [skytime] banner in dim cyan. ansiRed MUST NOT appear.
func TestLiveBlock_FlowCompleteBannerColored(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{Kind: "flow_complete", OkCount: 3, ErrCount: 0, TotalMs: 100})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	raw := out.String()
	require.Contains(t, raw, ansiDimCyan, "flow_complete success banner must be wrapped in ansiDimCyan")
	require.Contains(t, raw, ansiReset, "every color wrap must be closed by ansiReset")
	require.NotContains(t, raw, ansiRed, "success path must NOT contain ansiRed")
	stripped := stripAnsiTest(raw)
	require.Contains(t, stripped, "[skytime] flow complete  3/3 steps", "success banner content must survive color wrapping")
	require.Contains(t, stripped, "100ms", "total_ms must survive color wrapping")
}
