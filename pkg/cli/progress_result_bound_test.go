package cli

// Phase 04.2-05 Task 2 — renderer tests for the new D4.2 progress
// events. Two suites:
//
//   - TestProgressHandler_ResultBound (6 sub-cases): verifies the
//     `event=result_bound` row format on both the static path
//     (non-TTY, --verbose) and the live path (TTY+non-verbose),
//     including the path-depth indent rule and the empty-keys edge.
//
//   - TestProgressHandler_FailLeaf (3 sub-cases): verifies that the
//     existing red-✗ marker pattern from quick 260502-onc renders
//     top-level fail nodes correctly via the unchanged step_complete
//     {kind=fail, status=err} path — NO new fail-specific renderer
//     code is needed (the renderer doesn't know about *dag.Fail; it
//     knows about step_complete{status=err}).

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestProgressHandler_ResultBound — static + live + indent + verbose + empty
// ---------------------------------------------------------------------------

// TestProgressHandler_ResultBound exercises the static-path renderer's
// result_bound row in non-verbose mode: the buffer must contain
// `→ ctx.<alias>` and the keys=[...] substring must NOT be present.
func TestProgressHandler_ResultBound(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  false,
		ForceTTY: boolPtr(false),
	})
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "result_bound"),
		slog.String("alias", "result_dict"),
		slog.Any("keys", []string{"sign", "magnitude"}),
		slog.String("path", "0a"),
	)

	got := progressOut.String()
	require.Contains(t, got, "→ ctx.result_dict",
		"non-verbose static-path result_bound row must contain `→ ctx.<alias>`. Output: %q", got)
	require.NotContains(t, got, "keys=",
		"non-verbose mode must NOT show the keys list (terminal-noise reduction). Output: %q", got)
	// Sanity: no ANSI escapes on a non-TTY buffer.
	require.NotContains(t, got, "\x1b[",
		"non-TTY output must contain NO ANSI escape sequences. Output: %q", got)
	// Path "0a" → depth 1 → 4-space indent.
	require.True(t, strings.HasPrefix(got, "    "),
		"path 0a (depth 1) must start with 4 spaces of indent. Output: %q", got)
}

// TestProgressHandler_ResultBound_VerboseShowsKeys: --verbose mode
// triggers the keys list display. Buffer contains both `→ ctx.<alias>`
// and a keys=[...] substring carrying the bound dict's keys.
func TestProgressHandler_ResultBound_VerboseShowsKeys(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  true,
		ForceTTY: boolPtr(false),
	})
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "result_bound"),
		slog.String("alias", "result_dict"),
		slog.Any("keys", []string{"sign", "magnitude"}),
		slog.String("path", "0a"),
	)

	got := progressOut.String()
	require.Contains(t, got, "→ ctx.result_dict", "alias arrow row must appear")
	require.Contains(t, got, "keys=", "verbose mode must show the keys=... substring")
	require.Contains(t, got, "sign", "verbose-mode keys list must include each bound key")
	require.Contains(t, got, "magnitude", "verbose-mode keys list must include each bound key")
}

// TestProgressHandler_ResultBound_TTYWrapsCheckmark: when ForceTTY=true
// AND Verbose=true (verbose forces static-path even on TTY per
// D4.1-20), the static renderer wraps the ✓ marker in ANSI green +
// reset. Verbose=true is the cleanest way to drive the static path on
// a TTY so we can assert color escapes against the renderer output
// directly.
func TestProgressHandler_ResultBound_TTYWrapsCheckmark(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  true, // verbose=true keeps us on the static path even on TTY
		ForceTTY: boolPtr(true),
	})
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "result_bound"),
		slog.String("alias", "X"),
		slog.Any("keys", []string{"k"}),
		slog.String("path", "0a"),
	)

	got := progressOut.String()
	require.Contains(t, got, ansiGreen+"✓"+ansiReset,
		"TTY static-path result_bound row must wrap ✓ in ansiGreen + ansiReset. Output: %q", got)
	require.Contains(t, got, "→ ctx.X",
		"alias arrow row must follow the marker. Output: %q", got)
}

// TestProgressHandler_ResultBound_LiveBlockEmitsStaticLine: the live
// path's case "result_bound" emits the row above the redraw region
// (mirrors case "branch"). The buffer must contain the same `→ ctx.<alias>`
// row, emitted as a complete line (no in-place redraw of in-flight rows).
func TestProgressHandler_ResultBound_LiveBlockEmitsStaticLine(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)
	defer r.Close()

	r.submit(progressEvent{
		Kind:  "result_bound",
		Alias: "result_dict",
		Keys:  []string{"sign", "magnitude"},
		Path:  "0a",
	})

	// Wait for one tick so the renderer drains the event.
	time.Sleep(150 * time.Millisecond)

	stripped := stripAnsiTest(out.String())
	require.Contains(t, stripped, "→ ctx.result_dict",
		"live path must emit the same `→ ctx.<alias>` row above the redraw region. Output: %q", stripped)
	// Live path indent: path "0a" → depth 1 → 4-space indent.
	found := false
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "→ ctx.result_dict") {
			require.True(t, strings.HasPrefix(line, "    "),
				"live-path result_bound row must indent 4 spaces for path 0a. Line: %q", line)
			found = true
		}
	}
	require.True(t, found, "expected one result_bound line in live output. Output: %q", stripped)
}

// TestProgressHandler_ResultBound_PathDepthIndent: a deep-nested path
// "3a.0" yields depth 2 → 8-space indent. Pins the indent calculation
// shared between static and live paths.
func TestProgressHandler_ResultBound_PathDepthIndent(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  false,
		ForceTTY: boolPtr(false),
	})
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "result_bound"),
		slog.String("alias", "deep"),
		slog.Any("keys", []string{"k"}),
		slog.String("path", "3a.0"),
	)

	got := progressOut.String()
	require.True(t, strings.HasPrefix(got, "        "),
		"path 3a.0 (depth 2) must indent 8 spaces. Output: %q", got)
	require.False(t, strings.HasPrefix(got, "            "),
		"path 3a.0 (depth 2) must NOT indent 12 spaces. Output: %q", got)
}

// TestProgressHandler_ResultBound_EmptyKeysNoCrash: a result_bound event
// with Keys=nil renders the alias-only row and does NOT panic. Defense
// in depth — the parser-side invariant guarantees Keys is non-nil for
// well-formed result() calls, but a malformed event must not crash the
// renderer.
func TestProgressHandler_ResultBound_EmptyKeysNoCrash(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  true, // verbose=true tries to render keys; nil must not panic
		ForceTTY: boolPtr(false),
	})
	logger := slog.New(handler)

	require.NotPanics(t, func() {
		logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
			slog.String("event", "result_bound"),
			slog.String("alias", "X"),
			// no keys attr at all — defensive
			slog.String("path", "0a"),
		)
	})

	got := progressOut.String()
	require.Contains(t, got, "→ ctx.X",
		"alias-only row must still render when keys is empty. Output: %q", got)
	require.NotContains(t, got, "keys=",
		"empty-keys must suppress the keys=... substring entirely. Output: %q", got)
}

// ---------------------------------------------------------------------------
// TestProgressHandler_FailLeaf — fail nodes flow through unchanged
// renderStepComplete (status=err → red ✗), no new code path needed
// ---------------------------------------------------------------------------

// TestProgressHandler_FailLeaf: a step_complete event with kind=fail +
// status=err renders via the EXISTING renderStepComplete path with the
// red-✗ marker. Mirrors the quick 260502-onc red-marker pattern. NO
// new code path needed for fail leaves — the renderer doesn't know
// about *dag.Fail; it knows about step_complete{status=err}.
func TestProgressHandler_FailLeaf(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  false,
		ForceTTY: boolPtr(false),
	})
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_complete"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("kind", "fail"),
		slog.String("label", "fail(\"missing repo\")"),
		slog.String("path", "1"),
		slog.String("status", "err"),
		slog.Int64("duration_ms", 5),
		slog.String("summary", "fail t.star:5:13: missing repo"),
	)

	got := progressOut.String()
	require.Contains(t, got, "✗",
		"fail leaf must render with the red ✗ marker (status=err). Output: %q", got)
	require.Contains(t, got, "missing repo",
		"failure reason must appear in the rendered row. Output: %q", got)
	require.Contains(t, got, "fail",
		"kind=fail label must appear in the rendered row. Output: %q", got)
}

// TestProgressHandler_FailLeaf_LiveBlockRedMarker: the live path's
// case "step_complete" already handles status=err with the red ✗
// marker (existing pattern verified in progress_live_test.go). A
// fail-kind step_complete event flows through that case unchanged.
func TestProgressHandler_FailLeaf_LiveBlockRedMarker(t *testing.T) {
	out := &safeBuffer{}
	r := newLiveRenderer(out)
	defer r.Close()

	// First: step_dispatch so the live renderer has an active row to
	// finalize on step_complete (mirrors the leaf-kind shape).
	r.submit(progressEvent{
		Kind: "step_dispatch",
		Idx:  1, Total: 1,
		KindAttr: "fail",
		Label:    "fail(\"missing repo\")",
		Path:     "1",
	})
	time.Sleep(50 * time.Millisecond)

	r.submit(progressEvent{
		Kind: "step_complete",
		Idx:  1, Total: 1,
		KindAttr:   "fail",
		Label:      "fail(\"missing repo\")",
		Path:       "1",
		Status:     "err",
		DurationMs: 5,
		Summary:    "fail t.star:5:13: missing repo",
	})
	time.Sleep(150 * time.Millisecond)

	raw := out.String()
	require.Contains(t, raw, ansiRed+"✗"+ansiReset,
		"live-path fail leaf must wrap ✗ in ansiRed + ansiReset. Output: %q", raw)
	stripped := stripAnsiTest(raw)
	require.Contains(t, stripped, "missing repo",
		"failure reason must appear in the rendered live-path row. Output: %q", stripped)
}

// TestProgressHandler_FailLeaf_FlowFailedLastErrCaptured: the
// renderStepComplete path captures lastErr for renderFlowComplete to
// attribute the failure on the err_count > 0 branch (quick 260502-onc
// Fix C). A step_complete{kind=fail, status=err} event must populate
// lastErr just like any other status=err event so the subsequent
// flow_complete renders the [skytime] flow failed line citing the
// fail's summary.
func TestProgressHandler_FailLeaf_FlowFailedLastErrCaptured(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		{"skytime", []slog.Attr{
			slog.String("event", "flow_start"),
			slog.String("flow_name", "fail_demo"),
			slog.Int("step_count", 1),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 1), slog.Int("total", 1),
			slog.String("kind", "fail"),
			slog.String("label", "fail(\"nope\")"),
			slog.String("path", "1"),
			slog.String("status", "err"),
			slog.Int64("duration_ms", 5),
			slog.String("summary", "fail t.star:5:13: nope"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "flow_complete"),
			slog.Int("ok_count", 0),
			slog.Int("err_count", 1),
			slog.Int64("total_ms", 7),
		}},
	})

	require.Contains(t, out, "flow",
		"flow_complete with err_count>0 must render the failed line. Output: %q", out)
	require.Contains(t, out, "failed",
		"flow_complete with err_count>0 must contain `failed`. Output: %q", out)
	require.Contains(t, out, "nope",
		"flow_failed line must include the captured fail summary (Quick 260502-onc Fix C). Output: %q", out)
}
