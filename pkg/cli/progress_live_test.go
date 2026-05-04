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
		KindAttr:   "step",
		Label:      "gh.get(/x)",
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
	require.Contains(t, stripped, "gh.get(/x)",
		"finalized row must include the user-defined label (qx1). Stripped: %q", stripped)
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
		Kind: "step_complete", Idx: 1, Total: 1, KindAttr: "step", Label: "gh.get(/missing)",
		Status: "err", DurationMs: 80, Summary: "HTTP 404",
	})
	time.Sleep(120 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "✗", "failed step must render ✗ marker")
	require.Contains(t, stripped, "HTTP 404", "failed step summary must appear")
	require.Contains(t, stripped, "gh.get(/missing)",
		"failed step row must include the user-defined label (qx1). Stripped: %q", stripped)
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
		Kind: "step_complete", Idx: 1, Total: 1, KindAttr: "step", Label: "x",
		Status: "ok", DurationMs: 50, Summary: "status=200",
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
		Kind: "step_complete", Idx: 1, Total: 1, KindAttr: "step", Label: "x",
		Status: "err", DurationMs: 50, Summary: "HTTP 404",
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

// TestLiveBlock_BranchArrowColor: quick 260503-rhy migration — branch
// event now emits a HEADER line carrying ▶ wrapped in ansiYellow. The
// step_complete line is now a footer (✓ 1ms, no arrow).
func TestLiveBlock_BranchArrowColor(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	// step_dispatch for if_cond — captures total for branch-header rendering.
	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 3, Total: 3, KindAttr: "if_cond", Label: "cond", Path: "3",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{Kind: "branch", Idx: 3, Path: "3", Branch: "then"})
	time.Sleep(150 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 3, Total: 3, KindAttr: "if_cond", Label: "cond", Path: "3",
		Status: "ok", DurationMs: 1, Summary: "",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	raw := out.String()
	require.Contains(t, raw, ansiYellow, "header arrow ▶ must be wrapped in ansiYellow")
	require.Contains(t, raw, ansiReset, "every color wrap must be closed by ansiReset")
	stripped := stripAnsiTest(raw)
	require.Contains(t, stripped, "▶ then", "header arrow + branch name must survive color wrapping")

	// Header line: contains ▶ then AND if_cond AND counter [3/3].
	var headerFound bool
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "▶ then") {
			require.Contains(t, line, "if_cond", "header line must carry kind. Line: %q", line)
			require.Contains(t, line, "[3/3]", "header line must carry counter. Line: %q", line)
			headerFound = true
			break
		}
	}
	require.True(t, headerFound, "expected a line containing '▶ then'. Stripped: %q", stripped)

	// Footer line: contains ✓ 1ms but NO arrow.
	var footerFound bool
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "✓") && strings.Contains(line, "1ms") {
			require.NotContains(t, line, "▶",
				"footer line must NOT contain '▶'. Line: %q", line)
			require.NotContains(t, line, "→",
				"footer line must NOT contain '→'. Line: %q", line)
			footerFound = true
			break
		}
	}
	require.True(t, footerFound, "expected footer line ✓ 1ms (no arrow). Stripped: %q", stripped)

	// Defense: NO line equals the old standalone shape.
	for _, line := range strings.Split(stripped, "\n") {
		require.NotEqual(t, "     → then", line,
			"no stripped line may match the old standalone shape. Stripped: %q", stripped)
	}
}

// TestLiveBlock_BranchAppendsToStepComplete: quick 260503-rhy migration —
// branch event emits a HEADER line ([3/3] + if_cond + cond + ▶ then),
// step_complete emits a separate FOOTER line ([3/3] + if_cond + cond +
// ✓ + 1ms, no arrow). Both lines are present in the stripped output.
func TestLiveBlock_BranchAppendsToStepComplete(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 3, Total: 3, KindAttr: "if_cond", Label: "cond", Path: "3",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{Kind: "branch", Idx: 3, Path: "3", Branch: "then"})
	time.Sleep(150 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 3, Total: 3, KindAttr: "if_cond", Label: "cond", Path: "3",
		Status: "ok", DurationMs: 1, Summary: "",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "[3/3]", "counter must be present")
	require.Contains(t, stripped, "if_cond",
		"header+footer rows must carry the if_cond kind. Stripped: %q", stripped)
	require.Contains(t, stripped, "cond",
		"header+footer rows must carry the cond label. Stripped: %q", stripped)
	require.Contains(t, stripped, "✓ 1ms",
		"footer row must include marker + duration. Stripped: %q", stripped)
	require.Contains(t, stripped, "▶ then", "header line must carry ▶ branch suffix")

	// Header line.
	var foundHeader, foundFooter bool
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "[3/3]") &&
			strings.Contains(line, "if_cond") &&
			strings.Contains(line, "cond") &&
			strings.Contains(line, "▶ then") {
			foundHeader = true
		}
		if strings.Contains(line, "[3/3]") &&
			strings.Contains(line, "if_cond") &&
			strings.Contains(line, "cond") &&
			strings.Contains(line, "✓") &&
			strings.Contains(line, "1ms") &&
			!strings.Contains(line, "▶") &&
			!strings.Contains(line, "→") {
			foundFooter = true
		}
	}
	require.True(t, foundHeader,
		"expected header line with [3/3] + if_cond + cond + ▶ then. Stripped: %q", stripped)
	require.True(t, foundFooter,
		"expected footer line with [3/3] + if_cond + cond + ✓ + 1ms (no arrow). Stripped: %q", stripped)
}

// TestLiveBlock_OrphanBranchEvent_NoOutput: quick 260503-rhy migration —
// orphan branch (no preceding step_dispatch caching total) DOES emit
// an if_cond header now (D-RHY-03). The header carries [N/0] + if_cond +
// cond + ▶ branch. The QKK defense (no inline "→ branch" arrow) holds.
func TestLiveBlock_OrphanBranchEvent_NoOutput(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{Kind: "branch", Idx: 99, Path: "99", Branch: "then"})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "if_cond",
		"orphan branch must emit if_cond header (D-RHY-03). Stripped: %q", stripped)
	require.Contains(t, stripped, "▶ then",
		"orphan branch header must carry ▶ then. Stripped: %q", stripped)
	// QKK defense preserved: the OLD inline-arrow shape "→ then" must
	// not appear (the new header uses ▶, not →).
	require.NotContains(t, stripped, "→ then",
		"orphan branch must NOT emit the old qkk inline-arrow shape. Stripped: %q", stripped)
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

// ---------------------------------------------------------------------------
// Quick 260503-qx1: kind + label persist on live-path step_complete line
// ---------------------------------------------------------------------------
//
// Mirrors the static-path qx1 tests: the live renderer's case "step_complete"
// MUST consume ev.KindAttr (not hardcoded "step") and insert ev.Label between
// the kind column and the marker. Branch suffix from qkk continues to append
// after the summary.

// TestLiveBlock_StepCompleteIncludesKindAndLabel: a step_dispatch +
// step_complete pair carrying KindAttr="step" + Label="Get repo octocat/
// Hello-World" produces a finalized line containing all of [1/3], "step",
// the label, ✓, 234ms, and status=200.
func TestLiveBlock_StepCompleteIncludesKindAndLabel(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 1, Total: 3,
		KindAttr: "step", Label: "Get repo octocat/Hello-World", Path: "1",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 1, Total: 3,
		KindAttr: "step", Label: "Get repo octocat/Hello-World",
		Status: "ok", DurationMs: 234, Summary: "status=200",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "[1/3]", "live completion line must carry counter")
	require.Contains(t, stripped, "step", "live completion line must carry kind word")
	require.Contains(t, stripped, "Get repo octocat/Hello-World",
		"live completion line must carry user-defined label. Stripped: %q", stripped)
	require.Contains(t, stripped, "✓", "marker must be present")
	require.Contains(t, stripped, "234ms", "duration must be present")
	require.Contains(t, stripped, "status=200", "summary must be present")

	// All six properties on the same line.
	var foundAll bool
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "[1/3]") &&
			strings.Contains(line, "step") &&
			strings.Contains(line, "Get repo octocat/Hello-World") &&
			strings.Contains(line, "✓") &&
			strings.Contains(line, "234ms") &&
			strings.Contains(line, "status=200") {
			foundAll = true
			break
		}
	}
	require.True(t, foundAll,
		"expected single line with [1/3] + step + label + ✓ + 234ms + status=200. Stripped: %q", stripped)
}

// TestLiveBlock_StepCompleteIncludesKindAndLabel_Err: same shape with
// status=err, kind="if_cond", label="ctx.health". Persists kind+label on
// the failure line.
func TestLiveBlock_StepCompleteIncludesKindAndLabel_Err(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 2, Total: 3,
		KindAttr: "if_cond", Label: "ctx.health", Path: "2",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 2, Total: 3,
		KindAttr: "if_cond", Label: "ctx.health",
		Status: "err", DurationMs: 80, Summary: "boom",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "[2/3]", "live err line must carry counter")
	require.Contains(t, stripped, "if_cond",
		"live err line must carry the if_cond kind word (NOT hardcoded 'step'). Stripped: %q", stripped)
	require.Contains(t, stripped, "ctx.health",
		"live err line must carry the cond label. Stripped: %q", stripped)
	require.Contains(t, stripped, "✗", "err marker must be present")
	require.Contains(t, stripped, "80ms", "duration must be present")
	require.Contains(t, stripped, "boom", "summary must be present")
}

// TestLiveBlock_StepCompleteIncludesLabelWithBranchSuffix: quick 260503-rhy
// migration — branch event emits a HEADER line ([3/3] + if_cond + cond +
// ▶ then), step_complete emits a FOOTER line ([3/3] + if_cond + cond + ✓ +
// 1ms with NO arrow). Pins header/footer separation on the live path.
func TestLiveBlock_StepCompleteIncludesLabelWithBranchSuffix(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 3, Total: 3,
		KindAttr: "if_cond", Label: "cond", Path: "3",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{Kind: "branch", Idx: 3, Path: "3", Branch: "then"})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 3, Total: 3,
		KindAttr: "if_cond", Label: "cond", Path: "3",
		Status: "ok", DurationMs: 1, Summary: "",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())
	var foundHeader, foundFooter bool
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "[3/3]") &&
			strings.Contains(line, "if_cond") &&
			strings.Contains(line, "cond") &&
			strings.Contains(line, "▶ then") {
			foundHeader = true
		}
		if strings.Contains(line, "[3/3]") &&
			strings.Contains(line, "if_cond") &&
			strings.Contains(line, "cond") &&
			strings.Contains(line, "✓") &&
			strings.Contains(line, "1ms") &&
			!strings.Contains(line, "▶") &&
			!strings.Contains(line, "→") {
			foundFooter = true
		}
	}
	require.True(t, foundHeader,
		"expected header line with [3/3] + if_cond + cond + ▶ then. Stripped: %q", stripped)
	require.True(t, foundFooter,
		"expected footer line with [3/3] + if_cond + cond + ✓ + 1ms (no arrow). Stripped: %q", stripped)
}

// TestLiveBlock_StepCompleteEmptyLabel_NoCrash: KindAttr="step" + Label="".
// Live-path completion line still contains kind + ✓ + 50ms; no panic.
func TestLiveBlock_StepCompleteEmptyLabel_NoCrash(t *testing.T) {
	require.NotPanics(t, func() {
		out := &safeBuffer{}
		r := newLiveRenderer(out)

		r.submit(progressEvent{
			Kind: "step_complete", Idx: 1, Total: 1,
			KindAttr: "step", Label: "",
			Status: "ok", DurationMs: 50, Summary: "status=200",
		})
		time.Sleep(150 * time.Millisecond)
		r.Close()

		stripped := stripAnsiTest(out.String())
		require.Contains(t, stripped, "step",
			"empty-label live completion must still render kind. Stripped: %q", stripped)
		require.Contains(t, stripped, "✓", "marker must still render")
		require.Contains(t, stripped, "50ms", "duration must still render")
	})
}

// ---------------------------------------------------------------------------
// Quick 260503-rhy: live-path scope rendering (header + indented children + footer)
// ---------------------------------------------------------------------------

// TestLiveBlock_IfCond_RendersAsScope: parallel of static path —
// step_dispatch suppressed for if_cond, branch emits header (▶ then),
// step_complete emits footer (✓ ms, no arrow); child indented 4 spaces.
func TestLiveBlock_IfCond_RendersAsScope(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 3, Total: 3, KindAttr: "if_cond", Label: "cond", Path: "3",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{Kind: "branch", Idx: 3, Path: "3", Branch: "then"})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 1, Total: 1, KindAttr: "step", Label: "Get branches", Path: "3a",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 1, Total: 1, KindAttr: "step", Label: "Get branches", Path: "3a",
		Status: "ok", DurationMs: 445, Summary: "status=200",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 3, Total: 3, KindAttr: "if_cond", Label: "cond", Path: "3",
		Status: "ok", DurationMs: 446, Summary: "",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())

	// Header: [3/3] + if_cond + cond + ▶ then; not indented.
	headerFound := false
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "[3/3]") &&
			strings.Contains(line, "if_cond") &&
			strings.Contains(line, "cond") &&
			strings.Contains(line, "▶ then") {
			require.False(t, strings.HasPrefix(line, " "),
				"header must not be indented (path 3 → depth 0). Line: %q", line)
			headerFound = true
		}
	}
	require.True(t, headerFound, "expected if_cond header line. Stripped: %q", stripped)

	// Footer: ✓ 446ms; no arrow; not indented.
	footerFound := false
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "[3/3]") &&
			strings.Contains(line, "if_cond") &&
			strings.Contains(line, "cond") &&
			strings.Contains(line, "✓ 446ms") {
			require.NotContains(t, line, "▶", "footer must NOT contain '▶'. Line: %q", line)
			require.NotContains(t, line, "→", "footer must NOT contain '→'. Line: %q", line)
			require.False(t, strings.HasPrefix(line, " "),
				"footer must not be indented. Line: %q", line)
			footerFound = true
		}
	}
	require.True(t, footerFound, "expected if_cond footer line. Stripped: %q", stripped)

	// Child step row: indented 4 spaces, contains "Get branches" + ✓ 445ms.
	childFound := false
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "Get branches") &&
			strings.Contains(line, "✓ 445ms") {
			require.True(t, strings.HasPrefix(line, "    "),
				"child step (path 3a → depth 1) must be indented 4 spaces. Line: %q", line)
			childFound = true
		}
	}
	require.True(t, childFound, "expected child step completion line. Stripped: %q", stripped)
}

// TestLiveBlock_IfCond_NoActiveStepEntry: an if_cond step_dispatch must
// NOT count toward the "[skytime] in-progress N active" header. Only the
// leaf-kind dispatch counts (D-RHY-08).
func TestLiveBlock_IfCond_NoActiveStepEntry(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)
	defer r.Close()

	// Submit if_cond dispatch first (must NOT appear in active list).
	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 3, Total: 3, KindAttr: "if_cond", Label: "cond", Path: "3",
	})
	// Then a leaf step inside the branch.
	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 1, Total: 1, KindAttr: "step", Label: "Inner", Path: "3a",
	})
	time.Sleep(200 * time.Millisecond)

	stripped := stripAnsiTest(out.String())
	// Find the most recent "in-progress N active" line; N MUST be 1, not 2.
	require.Contains(t, stripped, "in-progress  1 active",
		"only leaf-kind dispatches count toward the active list. Stripped: %q", stripped)
	require.NotContains(t, stripped, "in-progress  2 active",
		"if_cond must NOT count as an active row (D-RHY-08). Stripped: %q", stripped)
}

// TestLiveBlock_ForEachParallel_RendersAsScope: parallel of static
// for_each test; same header + indented children + footer assertions.
func TestLiveBlock_ForEachParallel_RendersAsScope(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	// HEADER: step_dispatch idx=2 kind=for_each_parallel.
	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 2, Total: 3,
		KindAttr: "for_each_parallel", Label: "items=3", Path: "2",
	})
	time.Sleep(120 * time.Millisecond)

	// Three children at depth 1 (paths 2.0, 2.1, 2.2).
	for i := int64(0); i < 3; i++ {
		label := []string{"Read x", "Read y", "Read z"}[i]
		path := []string{"2.0", "2.1", "2.2"}[i]
		dur := []int64{234, 250, 246}[i]

		r.submit(progressEvent{
			Kind: "step_dispatch", Idx: i, Total: 3, KindAttr: "step", Label: label, Path: path,
		})
		time.Sleep(60 * time.Millisecond)
		r.submit(progressEvent{
			Kind: "step_complete", Idx: i, Total: 3, KindAttr: "step", Label: label, Path: path,
			Status: "ok", DurationMs: dur, Summary: "status=200",
		})
		time.Sleep(60 * time.Millisecond)
	}

	// FOOTER.
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 2, Total: 3,
		KindAttr: "for_each_parallel", Label: "items=3", Path: "2",
		Status: "ok", DurationMs: 730, Summary: "",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())

	// Header: [2/ + for_each_parallel + items=3 + ▶ open.
	headerFound := false
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "[2/") &&
			strings.Contains(line, "for_each_parallel") &&
			strings.Contains(line, "items=3") &&
			strings.Contains(line, "▶ open") {
			require.False(t, strings.HasPrefix(line, " "),
				"top-level for_each header must NOT be indented. Line: %q", line)
			headerFound = true
		}
	}
	require.True(t, headerFound, "expected for_each header. Stripped: %q", stripped)

	// Footer: ✓ 730ms; no arrow; not indented.
	footerFound := false
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "for_each_parallel") &&
			strings.Contains(line, "items=3") &&
			strings.Contains(line, "✓ 730ms") {
			require.NotContains(t, line, "▶", "footer must NOT contain '▶'. Line: %q", line)
			require.NotContains(t, line, "→", "footer must NOT contain '→'. Line: %q", line)
			require.False(t, strings.HasPrefix(line, " "),
				"top-level for_each footer must NOT be indented. Line: %q", line)
			footerFound = true
		}
	}
	require.True(t, footerFound, "expected for_each footer. Stripped: %q", stripped)

	// 3 indented child step rows (4-space indent each).
	childCount := 0
	for _, lbl := range []string{"Read x", "Read y", "Read z"} {
		for _, line := range strings.Split(stripped, "\n") {
			if strings.Contains(line, lbl) &&
				strings.Contains(line, "✓") &&
				strings.HasPrefix(line, "    ") &&
				!strings.HasPrefix(line, "        ") {
				childCount++
				break
			}
		}
	}
	require.Equal(t, 3, childCount,
		"expected 3 child step completion lines indented 4 spaces. Stripped: %q", stripped)
}

// TestLiveBlock_HeaderArrowYellow: branch event header carries
// ansiYellow + ▶ + ansiReset on the raw output.
func TestLiveBlock_HeaderArrowYellow(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 3, Total: 3, KindAttr: "if_cond", Label: "cond", Path: "3",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{Kind: "branch", Idx: 3, Path: "3", Branch: "then"})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	raw := out.String()
	require.Contains(t, raw, ansiYellow+"▶"+ansiReset,
		"header arrow ▶ must be wrapped in ansiYellow + ansiReset. Raw: %q", raw)
}

// TestLiveBlock_StepCompleteIfCond_NoBranchSuffix: step_complete kind=if_cond
// FOOTER must NOT contain "→ then" (it's on the header now). The HEADER line
// emitted by case "branch" DOES contain "▶ then".
func TestLiveBlock_StepCompleteIfCond_NoBranchSuffix(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)

	r.submit(progressEvent{
		Kind: "step_dispatch", Idx: 3, Total: 3, KindAttr: "if_cond", Label: "cond", Path: "3",
	})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{Kind: "branch", Idx: 3, Path: "3", Branch: "then"})
	time.Sleep(120 * time.Millisecond)
	r.submit(progressEvent{
		Kind: "step_complete", Idx: 3, Total: 3, KindAttr: "if_cond", Label: "cond", Path: "3",
		Status: "ok", DurationMs: 1, Summary: "",
	})
	time.Sleep(150 * time.Millisecond)
	r.Close()

	stripped := stripAnsiTest(out.String())

	// Footer line ✓ 1ms must not contain "→ then" or "▶".
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "✓ 1ms") {
			require.NotContains(t, line, "→ then",
				"footer must NOT contain branch suffix. Line: %q", line)
			require.NotContains(t, line, "▶",
				"footer must NOT contain '▶'. Line: %q", line)
		}
	}

	// Header line ▶ then must be present.
	headerFound := false
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "▶ then") {
			headerFound = true
			break
		}
	}
	require.True(t, headerFound,
		"header line containing '▶ then' must be present. Stripped: %q", stripped)
}
