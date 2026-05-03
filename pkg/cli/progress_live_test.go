//go:build !windows

package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Live-block renderer tests (Phase 04.1-06 Task 2, D4.1-17/18/19).
//
// Each test instantiates a *bytes.Buffer + creates a liveRenderer + submits
// events directly via .submit() (bypassing the slog Handle wrapper for
// unit-level testing) + waits a deterministic time + Close()s to drain
// the render goroutine.
//
// Generous time margins are used because a 100ms ticker plus a buffered
// channel makes byte-exact timing assertions flaky; the load-bearing
// properties are AT-LEAST-ONCE emission of cursor-up sequences,
// AT-LEAST-N distinct spinner frames seen, and exact "... and N more"
// presence on truncation.

// safeBuffer wraps bytes.Buffer with a mutex so the render goroutine
// can write while tests read. bytes.Buffer is NOT safe for concurrent
// use; the production liveRenderer guarantees a single writer (its own
// goroutine) but the test reads the buffer between writes.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

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
