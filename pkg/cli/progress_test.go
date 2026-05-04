package cli

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestProgress_BazelFormat is the table-driven coverage for every Skytime
// progress event type emitted by the interpreter walkers (quick 260502-guu
// Fix B). The renderer dispatches on the `event` attribute and emits a
// Bazel-style line; non-tty contexts (test buffers) get plain ASCII.
//
// The tests assert via Contains rather than exact-match because column
// padding can drift; the load-bearing properties are: the [skytime]
// banner appears for flow-level events, [N/M] counter appears for steps,
// kind label appears for steps, ✓/✗ markers appear for completion, and
// the duration is rendered in ms.
func TestProgress_BazelFormat(t *testing.T) {
	type call struct {
		msg   string
		attrs []slog.Attr
	}
	type expect []string

	cases := []struct {
		name string
		call call
		want expect
	}{
		{
			name: "flow_start banner",
			call: call{"skytime", []slog.Attr{
				slog.String("event", "flow_start"),
				slog.String("flow_name", "simple_check"),
				slog.Int("step_count", 3),
			}},
			want: expect{"[skytime]", "flow", "simple_check", "3", "step", "starting"},
		},
		{
			name: "step_dispatch step kind",
			call: call{"skytime", []slog.Attr{
				slog.String("event", "step_dispatch"),
				slog.Int("idx", 1), slog.Int("total", 3),
				slog.String("kind", "step"),
				slog.String("label", "gh.get(/repos/example/repo)"),
				slog.String("path", "1"),
			}},
			want: expect{"[1/3]", "step", "gh.get(/repos/example/repo)"},
		},
		{
			name: "step_complete ok",
			call: call{"skytime", []slog.Attr{
				slog.String("event", "step_complete"),
				slog.Int("idx", 1), slog.Int("total", 3),
				slog.String("kind", "step"),
				slog.String("label", "gh.get(/x)"),
				slog.String("path", "1"),
				slog.String("status", "ok"),
				slog.Int64("duration_ms", 234),
				slog.String("summary", "status=200"),
			}},
			want: expect{"✓", "234ms", "status=200", "gh.get(/x)"},
		},
		{
			name: "step_complete err",
			call: call{"skytime", []slog.Attr{
				slog.String("event", "step_complete"),
				slog.Int("idx", 2), slog.Int("total", 3),
				slog.String("kind", "step"),
				slog.String("label", "gh.get(/x)"),
				slog.String("path", "2"),
				slog.String("status", "err"),
				slog.Int64("duration_ms", 120),
				slog.String("summary", "connection refused"),
			}},
			want: expect{"✗", "120ms", "connection refused", "gh.get(/x)"},
		},
		// Quick 260503-rhy: if_cond step_dispatch is now SUPPRESSED
		// (D-RHY-01 — header is delegated to the branch event). The
		// dispatched-only case below is covered by
		// TestProgress_StepDispatch_IfCond_Suppressed (asserts empty
		// output). Drive a step_dispatch + branch sequence here so the
		// contains-style table assertions still cover the if_cond
		// header rendering path.
		{
			name: "step_dispatch if_cond + branch then renders header",
			// Note: the table runner only invokes ONE call; we exercise
			// the header by feeding the branch event directly. The
			// branch event renders the header containing if_cond + cond
			// + ▶ then. Counter is [3/0] because no preceding dispatch
			// cached the total in this single-call test (defensive
			// behavior — header still renders, just with total=0).
			call: call{"skytime", []slog.Attr{
				slog.String("event", "branch"),
				slog.Int("idx", 3),
				slog.String("path", "3"),
				slog.String("branch", "then"),
			}},
			want: expect{"[3/", "if_cond", "cond", "▶", "then"},
		},
		{
			name: "flow_complete",
			call: call{"skytime", []slog.Attr{
				slog.String("event", "flow_complete"),
				slog.Int("ok_count", 3),
				slog.Int("err_count", 0),
				slog.Int64("total_ms", 433),
			}},
			want: expect{"[skytime]", "complete", "3/3", "433ms"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var progressOut, passOut bytes.Buffer
			passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
			handler := newProgressHandler(passthrough, &progressOut)
			logger := slog.New(handler)

			logger.LogAttrs(context.Background(), slog.LevelInfo, tc.call.msg, tc.call.attrs...)

			got := progressOut.String()
			for _, want := range tc.want {
				require.Contains(t, got, want, "rendered output: %q", got)
			}
			// Skytime records MUST NOT also flow through the wrapped
			// handler — the renderer owns them exclusively.
			require.Empty(t, passOut.String(),
				"non-Skytime handler must not receive Skytime-namespaced records (got: %q)", passOut.String())
		})
	}
}

// TestProgress_NestedStepPath: a step_dispatch with a nested path "3a"
// emits [3a/3a] and the row is indented 2 spaces from column 0.
func TestProgress_NestedStepPath(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_dispatch"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("kind", "step"),
		slog.String("label", "gh.get(/x)"),
		slog.String("path", "3a"),
	)

	got := progressOut.String()
	require.Contains(t, got, "[3a/3a]", "nested path uses path-based counter, not numeric idx/total")
	require.True(t, strings.HasPrefix(got, "  "), "nested rows must be indented 2 spaces; got %q", got)
}

// ---------------------------------------------------------------------------
// Quick 260503-qkk: branch label inlines onto step_complete line
// ---------------------------------------------------------------------------
//
// renderBranch is now buffer-only; the inline ` → <branch>` suffix is
// appended to the if_cond's step_complete line. These tests pin:
//   - happy path: branch then step_complete same idx → suffix appended,
//     no standalone line.
//   - happy path (else): same as above with branch="else".
//   - orphan branch: branch with no matching step_complete → zero output.
//   - step_complete without buffered branch: format unchanged, no suffix.

// runBranchInlineSequence drives a 3-event sequence (step_dispatch → branch
// → step_complete) and returns the rendered output. Helper used by the
// then/else sub-tests below.
func runBranchInlineSequence(t *testing.T, branchName string) string {
	t.Helper()
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_dispatch"),
		slog.Int("idx", 3), slog.Int("total", 3),
		slog.String("kind", "if_cond"),
		slog.String("label", "ctx.health"),
		slog.String("path", "3"),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "branch"),
		slog.Int("idx", 3),
		slog.String("path", "3"),
		slog.String("branch", branchName),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_complete"),
		slog.Int("idx", 3), slog.Int("total", 3),
		slog.String("kind", "if_cond"),
		slog.String("path", "3"),
		slog.String("status", "ok"),
		slog.Int64("duration_ms", 1),
		slog.String("summary", ""),
	)
	return progressOut.String()
}

// TestProgress_BranchAppendsToStepComplete: quick 260503-rhy migration —
// branch suffix moves from the if_cond step_complete LINE onto the if_cond
// HEADER line emitted by renderBranch. The step_complete line is now a
// scope FOOTER (counter + kind + label + ✓ + ms) with NO arrow. The
// standalone "     → <branch>" line is still NOT emitted (qkk defense
// retained verbatim).
func TestProgress_BranchAppendsToStepComplete(t *testing.T) {
	t.Run("then", func(t *testing.T) {
		got := runBranchInlineSequence(t, "then")

		// Header line emitted by renderBranch: contains [3/3] + if_cond +
		// cond + ▶ then. The step_complete (footer) is a SEPARATE line
		// containing [3/3] + if_cond + cond + ✓ 1ms with NO arrow.
		require.Contains(t, got, "✓ 1ms",
			"if_cond footer marker + duration must be present. Output: %q", got)
		require.Contains(t, got, "▶ then",
			"header line must carry the new ▶ branch suffix (qkk migration). Output: %q", got)
		require.NotContains(t, got, "     → then",
			"old qkk standalone-line shape must NOT appear. Output: %q", got)

		// Defensive: NO line equals exactly "     → then".
		for _, line := range strings.Split(got, "\n") {
			require.NotEqual(t, "     → then", line,
				"no rendered line may match the old standalone shape exactly. Output: %q", got)
		}

		// New rhy contract: the line containing "✓ 1ms" (the FOOTER) must
		// NOT contain "→" or "▶" — branch suffix moved to the header line.
		var footerFound, headerFound bool
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, "✓ 1ms") {
				require.NotContains(t, line, "→",
					"footer line must NOT contain '→' (branch suffix is on header now). Line: %q", line)
				require.NotContains(t, line, "▶",
					"footer line must NOT contain '▶' (header glyph). Line: %q", line)
				footerFound = true
			}
			if strings.Contains(line, "▶ then") {
				require.Contains(t, line, "[3/3]", "header line must carry counter")
				require.Contains(t, line, "if_cond", "header line must carry kind")
				require.Contains(t, line, "cond", "header line must carry the cond label")
				headerFound = true
			}
		}
		require.True(t, footerFound, "expected at least one line containing '✓ 1ms'. Output: %q", got)
		require.True(t, headerFound, "expected header line containing '▶ then'. Output: %q", got)
	})

	t.Run("else", func(t *testing.T) {
		got := runBranchInlineSequence(t, "else")

		require.Contains(t, got, "✓ 1ms",
			"if_cond footer marker + duration must be present. Output: %q", got)
		require.Contains(t, got, "▶ else",
			"header line must carry ▶ else (qkk migration). Output: %q", got)
		require.NotContains(t, got, "     → else",
			"old qkk standalone-line shape must NOT appear. Output: %q", got)

		var footerFound, headerFound bool
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, "✓ 1ms") {
				require.NotContains(t, line, "→",
					"footer line must NOT contain '→'. Line: %q", line)
				require.NotContains(t, line, "▶",
					"footer line must NOT contain '▶'. Line: %q", line)
				footerFound = true
			}
			if strings.Contains(line, "▶ else") {
				headerFound = true
			}
		}
		require.True(t, footerFound, "expected at least one line containing '✓ 1ms'. Output: %q", got)
		require.True(t, headerFound, "expected header line containing '▶ else'. Output: %q", got)
	})
}

// TestProgress_OrphanBranchEvent_NoStandaloneLine: quick 260503-rhy
// migration — a lone branch event (no preceding step_dispatch caching
// total) DOES emit an if_cond header now (D-RHY-03). The header carries
// [N/0] (zero total since cache miss) + if_cond + cond + ▶ branch. The
// QKK defense — "no line equals the OLD standalone shape '     → then'"
// — remains valid: the old standalone arrow shape with 5-space indent
// must NEVER appear.
func TestProgress_OrphanBranchEvent_NoStandaloneLine(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "branch"),
		slog.Int("idx", 99),
		slog.String("path", "99"),
		slog.String("branch", "then"),
	)

	got := progressOut.String()
	// Header is emitted (rhy: branch event renders the header inline).
	require.Contains(t, got, "if_cond",
		"orphan branch must emit if_cond header (D-RHY-03). Output: %q", got)
	require.Contains(t, got, "▶ then",
		"orphan branch header must carry ▶ then. Output: %q", got)

	// QKK defense preserved: the OLD standalone "     → then" shape
	// must NEVER appear (5-space indent + arrow + branch name).
	for _, line := range strings.Split(got, "\n") {
		require.NotEqual(t, "     → then", line,
			"no rendered line may match the old qkk standalone shape. Output: %q", got)
	}
	require.NotContains(t, got, "→ then",
		"orphan branch must NOT emit the old qkk inline-arrow shape. Output: %q", got)
}

// TestProgress_StepCompleteWithoutBufferedBranch_NoSuffix: a step_complete
// with no prior branch event renders the existing format verbatim — no
// trailing arrow.
func TestProgress_StepCompleteWithoutBufferedBranch_NoSuffix(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_complete"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("kind", "step"),
		slog.String("path", "1"),
		slog.String("status", "ok"),
		slog.Int64("duration_ms", 42),
		slog.String("summary", "status=200"),
	)

	got := progressOut.String()
	require.Contains(t, got, "✓ 42ms  status=200",
		"existing step_complete format must be preserved verbatim. Output: %q", got)
	require.NotContains(t, got, "→",
		"no suffix arrow when buffer is empty. Output: %q", got)
}

// ---------------------------------------------------------------------------
// Quick 260503-qx1: kind + label persist on step_complete line
// ---------------------------------------------------------------------------
//
// Mirrors the step_dispatch shape on the step_complete line so user-defined
// step names (D4.1-15 step(name="...")) persist past completion. The label
// MUST appear BEFORE the marker on the same line; the kind word MUST come
// from ev.KindAttr (not a hardcoded "step" string) so if_cond/script/
// for_each_parallel/call_flow rows render with the correct kind.

// TestProgress_StepCompleteIncludesKindAndLabel: a step_complete record
// carrying kind="step" + label="Get repo octocat/Hello-World" renders a
// single line containing the counter, padded kind, label, marker,
// duration, and summary. The label appears BEFORE the marker.
func TestProgress_StepCompleteIncludesKindAndLabel(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 1), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "Get repo octocat/Hello-World"),
			slog.String("path", "1"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 234),
			slog.String("summary", "status=200"),
		}},
	})

	require.Contains(t, out, "[1/3]", "completion line must carry [N/M] counter")
	require.Contains(t, out, "step", "completion line must carry kind word")
	require.Contains(t, out, "Get repo octocat/Hello-World",
		"completion line must carry the user-defined label. Output: %q", out)
	require.Contains(t, out, "✓ 234ms", "marker + duration must be present")
	require.Contains(t, out, "status=200", "summary must be present")

	// Label must appear BEFORE the marker on the same line.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Get repo octocat/Hello-World") {
			labelIdx := strings.Index(line, "Get repo octocat/Hello-World")
			markerIdx := strings.Index(line, "✓")
			require.Greater(t, markerIdx, labelIdx,
				"label must appear before the ✓ marker. Line: %q", line)
		}
	}
}

// TestProgress_StepCompleteIncludesKindAndLabel_Err: same shape with an
// err status; kind+label persist on the failure line.
func TestProgress_StepCompleteIncludesKindAndLabel_Err(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 2), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "Get repo octocat/Hello-World"),
			slog.String("path", "2"),
			slog.String("status", "err"),
			slog.Int64("duration_ms", 120),
			slog.String("summary", "HTTP 404"),
		}},
	})

	require.Contains(t, out, "[2/3]", "err completion line must carry counter")
	require.Contains(t, out, "step", "err completion line must carry kind word")
	require.Contains(t, out, "Get repo octocat/Hello-World",
		"err completion line must carry the user-defined label. Output: %q", out)
	require.Contains(t, out, "✗ 120ms", "err marker + duration must be present")
	require.Contains(t, out, "HTTP 404", "err summary must be present")
}

// TestProgress_StepCompleteIncludesLabelWithBranchSuffix: quick 260503-rhy
// migration — branch event now emits a HEADER line (counter + if_cond +
// cond + ▶ then). The step_complete event emits a separate FOOTER line
// (counter + if_cond + cond + ✓ + ms) with NO arrow. Both lines must
// carry counter+kind+label so user-defined labels persist past dispatch.
func TestProgress_StepCompleteIncludesLabelWithBranchSuffix(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 3), slog.Int("total", 3),
			slog.String("kind", "if_cond"),
			slog.String("label", "cond"),
			slog.String("path", "3"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "branch"),
			slog.Int("idx", 3),
			slog.String("path", "3"),
			slog.String("branch", "then"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 3), slog.Int("total", 3),
			slog.String("kind", "if_cond"),
			slog.String("label", "cond"),
			slog.String("path", "3"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 1),
			slog.String("summary", ""),
		}},
	})

	require.Contains(t, out, "if_cond", "header+footer must carry the if_cond kind word")
	require.Contains(t, out, "cond",
		"header+footer must carry the cond label. Output: %q", out)
	require.Contains(t, out, "✓ 1ms", "footer marker + duration must be present")
	require.Contains(t, out, "▶ then", "header carries ▶ branch (rhy: branch suffix moved off footer)")

	// Header line: contains [3/3] + if_cond + cond + ▶ then.
	// Footer line: contains [3/3] + if_cond + cond + ✓ + 1ms (NO arrow).
	var foundHeader, foundFooter bool
	for _, line := range strings.Split(out, "\n") {
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
		"expected header line with [3/3] + if_cond + cond + ▶ then. Output: %q", out)
	require.True(t, foundFooter,
		"expected footer line with [3/3] + if_cond + cond + ✓ + 1ms (no arrow). Output: %q", out)
}

// TestProgress_StepCompleteEmptyLabel_NoCrash: a step_complete with
// label="" renders without panic and still contains the kind + marker.
func TestProgress_StepCompleteEmptyLabel_NoCrash(t *testing.T) {
	require.NotPanics(t, func() {
		out := emitProgress(t, []struct {
			msg   string
			attrs []slog.Attr
		}{
			{"skytime", []slog.Attr{
				slog.String("event", "step_complete"),
				slog.Int("idx", 1), slog.Int("total", 1),
				slog.String("kind", "step"),
				slog.String("label", ""),
				slog.String("path", "1"),
				slog.String("status", "ok"),
				slog.Int64("duration_ms", 50),
				slog.String("summary", "status=200"),
			}},
		})

		require.Contains(t, out, "step", "kind word must still render with empty label")
		require.Contains(t, out, "✓ 50ms",
			"marker + duration must still render with empty label. Output: %q", out)
	})
}

// ---------------------------------------------------------------------------
// Quick 260502-onc Fix C: flow_failed renderer + lastErr lifecycle.
// ---------------------------------------------------------------------------

// emitProgress is a tiny helper that drives the renderer with a series of
// records — used by the flow_failed tests below to keep the wiring
// concise.
func emitProgress(t *testing.T, calls []struct {
	msg   string
	attrs []slog.Attr
}) string {
	t.Helper()
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)
	for _, c := range calls {
		logger.LogAttrs(context.Background(), slog.LevelInfo, c.msg, c.attrs...)
	}
	return progressOut.String()
}

// TestProgress_FlowFailed: a [flow_start, step_dispatch, step_complete err,
// flow_complete err_count=1] sequence renders the flow_failed line
// citing the failing step idx + total + summary.
func TestProgress_FlowFailed(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		{"skytime", []slog.Attr{
			slog.String("event", "flow_start"),
			slog.String("flow_name", "bad"),
			slog.Int("step_count", 3),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 2), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "gh.get(/missing)"),
			slog.String("path", "2"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 2), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("path", "2"),
			slog.String("status", "err"),
			slog.Int64("duration_ms", 120),
			slog.String("summary", "HTTP 404 GET /missing"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "flow_complete"),
			slog.Int("ok_count", 1),
			slog.Int("err_count", 1),
			slog.Int64("total_ms", 300),
		}},
	})

	require.Contains(t, out, "[skytime]", "banner column must remain stable")
	require.Contains(t, out, "flow failed", "Fix C: err_count > 0 → flow failed line")
	require.Contains(t, out, "step 2/3", "must cite the failing step's idx/total")
	require.Contains(t, out, "HTTP 404", "must cite the failing step's summary as reason")
	require.Contains(t, out, "300ms", "must include total_ms")
	require.NotContains(t, out, "flow complete",
		"Fix C: failure path is mutually exclusive with the success line")
}

// TestProgress_FlowComplete_NoFailure: ok_count > 0 + err_count == 0
// keeps the existing flow complete renderer.
func TestProgress_FlowComplete_NoFailure(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		{"skytime", []slog.Attr{
			slog.String("event", "flow_start"),
			slog.String("flow_name", "good"),
			slog.Int("step_count", 3),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "flow_complete"),
			slog.Int("ok_count", 3),
			slog.Int("err_count", 0),
			slog.Int64("total_ms", 433),
		}},
	})
	require.Contains(t, out, "flow complete  3/3 steps")
	require.NotContains(t, out, "flow failed",
		"happy path must NOT render flow failed")
}

// TestProgress_LastErrResetsOnFlowStart: a long-lived handler (one
// process, multiple workflow executions) must reset lastErr on each
// flow_start so the previous run's failure context does not leak into
// the next run's render.
func TestProgress_LastErrResetsOnFlowStart(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		// Run 1: failure
		{"skytime", []slog.Attr{
			slog.String("event", "flow_start"),
			slog.String("flow_name", "first"),
			slog.Int("step_count", 1),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 1), slog.Int("total", 1),
			slog.String("kind", "step"),
			slog.String("path", "1"),
			slog.String("status", "err"),
			slog.Int64("duration_ms", 50),
			slog.String("summary", "HTTP 500"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "flow_complete"),
			slog.Int("ok_count", 0),
			slog.Int("err_count", 1),
			slog.Int64("total_ms", 50),
		}},
		// Run 2: success — must NOT carry "HTTP 500" forward.
		{"skytime", []slog.Attr{
			slog.String("event", "flow_start"),
			slog.String("flow_name", "second"),
			slog.Int("step_count", 1),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "flow_complete"),
			slog.Int("ok_count", 1),
			slog.Int("err_count", 0),
			slog.Int64("total_ms", 75),
		}},
	})

	require.Contains(t, out, "flow complete  1/1 steps",
		"second flow_complete must render success line — lastErr leaked otherwise")
	// The first run's "flow failed" + "HTTP 500" are still expected in the
	// stream (they DID happen in run 1) — the leak we're guarding against
	// is the SECOND run's flow_complete being mis-rendered as flow failed.
	// Count occurrences:
	require.Equal(t, 1, strings.Count(out, "flow failed"),
		"Fix C: lastErr reset on flow_start — only run 1 emits flow failed")
	require.Equal(t, 1, strings.Count(out, "flow complete"),
		"Fix C: only run 2 emits flow complete")
}

// TestProgress_FlowFailed_NoLastErr: defensive — flow_complete with
// err_count > 0 but NO preceding step_complete-with-err record (a
// malformed event sequence that should not occur in practice). The
// renderer must NOT crash; it falls back to a placeholder summary.
func TestProgress_FlowFailed_NoLastErr(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		{"skytime", []slog.Attr{
			slog.String("event", "flow_start"),
			slog.String("flow_name", "weird"),
			slog.Int("step_count", 1),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "flow_complete"),
			slog.Int("ok_count", 0),
			slog.Int("err_count", 1),
			slog.Int64("total_ms", 10),
		}},
	})
	require.Contains(t, out, "flow failed",
		"err_count > 0 without lastErr must still render the failure line")
	require.Contains(t, out, "(no per-step error captured)",
		"placeholder summary must indicate the missing step context")
}

// TestProgress_PassthroughOnNonSkytimeRecord: a logger.Info with NO
// `event` attribute MUST flow through to the wrapped charm-log handler
// buffer, NOT to the progress writer.
func TestProgress_PassthroughOnNonSkytimeRecord(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)

	logger.Info("plain SDK log message", slog.String("foo", "bar"))

	require.Empty(t, progressOut.String(),
		"progress writer must not receive records lacking the event attribute")
	require.Contains(t, passOut.String(), "plain SDK log message",
		"non-event records must reach the wrapped handler")
}

// ---------------------------------------------------------------------------
// Phase 04.1-06 Task 1: Mode selection — TTY + verbose plumbing for live block
// ---------------------------------------------------------------------------
//
// These tests pin the refactor that splits progress.go into a static path
// (the previous Phase 4 + quick-260502 renderer) and a forthcoming live-block
// path (D4.1-17..21). useLiveBlock() is the discriminator — false on
// non-TTY, false on --verbose, false on Windows (build-tag enforced
// elsewhere), true otherwise.
//
// Tests 1, 2, 4 work after Task 1's mode-selection refactor.
// Test 3 verifies live-path activation; it is t.Skip()'d here and
// un-skipped in Task 3 once Handle() routes through the live renderer.

// boolPtr returns a pointer to b. Local helper so tests can pass
// progressHandlerOptions.ForceTTY.
func boolPtr(b bool) *bool { return &b }

// emitTestEvents drives a sequence of events covering every live-block
// rendering path (flow_start → step_dispatch → step_complete → flow_complete).
// Returns the rendered output as captured by the progressOut buffer.
func emitTestEvents(t *testing.T, h *progressHandler) string {
	t.Helper()
	logger := slog.New(h)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "flow_start"),
		slog.String("flow_name", "test_flow"),
		slog.Int("step_count", 1),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_dispatch"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("kind", "step"),
		slog.String("label", "gh.get(/x)"),
		slog.String("path", "1"),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_complete"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("kind", "step"),
		slog.String("label", "gh.get(/x)"),
		slog.String("path", "1"),
		slog.String("status", "ok"),
		slog.Int64("duration_ms", 50),
		slog.String("summary", "status=200"),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "flow_complete"),
		slog.Int("ok_count", 1),
		slog.Int("err_count", 0),
		slog.Int64("total_ms", 50),
	)
	// Drain any live-renderer goroutine so output is fully flushed.
	h.Close()
	return ""
}

// TestProgress_StaticPath_NonTTY: a non-TTY (bytes.Buffer) handler emits
// plain ASCII Bazel-style lines. NO ANSI escape sequences appear.
func TestProgress_StaticPath_NonTTY(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	// Default constructor — ForceTTY not set, so isTTY() returns false on a
	// *bytes.Buffer (not a *os.File). Verbose=false default.
	handler := newProgressHandler(passthrough, &progressOut)
	emitTestEvents(t, handler)

	got := progressOut.String()
	require.NotContains(t, got, "\x1b[", "non-TTY output must contain NO ANSI escape sequences (got: %q)", got)
	require.Contains(t, got, "[skytime] flow test_flow", "static-path flow_start banner")
	require.Contains(t, got, "[1/1]", "static-path step_dispatch counter")
	require.Contains(t, got, "✓", "static-path step_complete OK marker")
	require.Contains(t, got, "50ms", "static-path duration")
	require.Contains(t, got, "[skytime] flow complete", "static-path flow_complete summary")
}

// TestProgress_StaticPath_VerboseEvenOnTTY: when Verbose=true, the live
// block is disabled (D4.1-20) — even a forced-TTY handler emits plain
// static lines. NO live-block ANSI cursor sequences.
//
// Note: static-path output on a TTY still wraps headers/markers in
// color ANSI codes (the existing Phase 4 behavior). The verbose
// preemption rule applies to the LIVE BLOCK redraw (cursor-up +
// line-clear), not to color output. Tests assert the absence of the
// load-bearing live-block sequences only.
func TestProgress_StaticPath_VerboseEvenOnTTY(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  true,
		ForceTTY: boolPtr(true),
	})
	emitTestEvents(t, handler)

	got := progressOut.String()
	require.NotContains(t, got, "\x1b[1A", "verbose=true must NOT emit cursor-up sequences (live block disabled per D4.1-20)")
	require.NotContains(t, got, "\x1b[2K", "verbose=true must NOT emit clear-line sequences")
	require.NotContains(t, got, "\x1b[?25l", "verbose=true must NOT emit cursor-hide sequences")
	require.Contains(t, got, "flow test_flow", "verbose=true still renders Bazel-style static lines")
	require.Contains(t, got, "1 steps  starting", "verbose=true still renders flow_start banner")
}

// TestProgress_LivePathChosen_TTYNonVerbose: with ForceTTY=true and
// Verbose=false, useLiveBlock() returns true and Handle() routes to
// the live renderer. After Task 3 wires the dispatch, a single
// step_dispatch produces ANSI cursor-up codes via the live block.
func TestProgress_LivePathChosen_TTYNonVerbose(t *testing.T) {
	progressOut := &safeBuffer{}
	var passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, progressOut, progressHandlerOptions{
		Verbose:  false,
		ForceTTY: boolPtr(true),
	})
	defer handler.Close()

	logger := slog.New(handler)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_dispatch"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("kind", "step"),
		slog.String("label", "gh.get(/x)"),
		slog.String("path", "1"),
	)

	// Wait for one tick so the live goroutine emits at least one redraw.
	time.Sleep(150 * time.Millisecond)

	got := progressOut.String()
	require.Contains(t, got, "\x1b[1A",
		"TTY + non-verbose Handle() must route to live renderer (cursor-up sequence). Output: %q", got)
}

// ---------------------------------------------------------------------------
// Phase 04.1-06 Task 3: live renderer wired into Handle() + Close lifecycle
// ---------------------------------------------------------------------------

// TestProgress_LiveBlock_DrainsOnFlowComplete: a full event sequence
// followed by handler.Close() must result in a clean static "[skytime]
// flow complete" line in the buffer (after stripping ANSI).
func TestProgress_LiveBlock_DrainsOnFlowComplete(t *testing.T) {
	progressOut := &safeBuffer{}
	var passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, progressOut, progressHandlerOptions{
		Verbose:  false,
		ForceTTY: boolPtr(true),
	})

	logger := slog.New(handler)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "flow_start"),
		slog.String("flow_name", "test"),
		slog.Int("step_count", 1),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_dispatch"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("kind", "step"),
		slog.String("label", "gh.get(/x)"),
		slog.String("path", "1"),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_complete"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("status", "ok"),
		slog.Int64("duration_ms", 50),
		slog.String("summary", "status=200"),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "flow_complete"),
		slog.Int("ok_count", 1),
		slog.Int("err_count", 0),
		slog.Int64("total_ms", 50),
	)
	handler.Close()

	stripped := stripAnsiTest(progressOut.String())
	require.Contains(t, stripped, "[skytime] flow complete",
		"flow_complete must render as a clean static line after Close drain. Stripped: %q", stripped)
}

// TestProgress_LiveBlock_VerboseRemainsStatic: verbose=true + ForceTTY=true
// → Handle stays on static path, no live-block ANSI sequences emitted.
func TestProgress_LiveBlock_VerboseRemainsStatic(t *testing.T) {
	progressOut := &safeBuffer{}
	var passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, progressOut, progressHandlerOptions{
		Verbose:  true,
		ForceTTY: boolPtr(true),
	})
	defer handler.Close()

	logger := slog.New(handler)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "flow_start"),
		slog.String("flow_name", "test"),
		slog.Int("step_count", 1),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_dispatch"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("kind", "step"),
		slog.String("label", "gh.get(/x)"),
		slog.String("path", "1"),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "flow_complete"),
		slog.Int("ok_count", 1),
		slog.Int("err_count", 0),
		slog.Int64("total_ms", 50),
	)

	got := progressOut.String()
	// Static-path color codes are OK; LIVE-BLOCK sequences are not.
	require.NotContains(t, got, "\x1b[1A",
		"verbose=true must NOT emit cursor-up (live block off per D4.1-20)")
	require.NotContains(t, got, "\x1b[2K",
		"verbose=true must NOT emit clear-line")
	require.NotContains(t, got, "\x1b[?25l",
		"verbose=true must NOT emit cursor-hide")
}

// TestProgress_LiveBlock_LifecycleNoLeak: construct a live-mode handler,
// never send any event, call Close. The render goroutine must exit
// promptly. Failure mode: Close blocks forever.
func TestProgress_LiveBlock_LifecycleNoLeak(t *testing.T) {
	progressOut := &safeBuffer{}
	var passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, progressOut, progressHandlerOptions{
		Verbose:  false,
		ForceTTY: boolPtr(true),
	})

	// Force lazy renderer init by sending one event so liveOnce fires.
	logger := slog.New(handler)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "flow_start"),
		slog.String("flow_name", "leak-test"),
		slog.Int("step_count", 0),
	)

	done := make(chan struct{})
	go func() {
		handler.Close()
		close(done)
	}()
	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("handler.Close did not return within 2s — render goroutine leaked")
	}
}

// TestNewProgressHandler_AcceptsVerboseFlag: the new options-based
// constructor wires Verbose + ForceTTY into the handler such that
// useLiveBlock() returns false for verbose=true.
func TestNewProgressHandler_AcceptsVerboseFlag(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})

	verboseTTY := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  true,
		ForceTTY: boolPtr(true),
	})
	require.False(t, verboseTTY.useLiveBlock(),
		"verbose=true must disable live block even on TTY (D4.1-20)")

	nonVerboseTTY := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  false,
		ForceTTY: boolPtr(true),
	})
	require.True(t, nonVerboseTTY.useLiveBlock(),
		"verbose=false + TTY must enable live block (D4.1-17)")

	nonTTY := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  false,
		ForceTTY: boolPtr(false),
	})
	require.False(t, nonTTY.useLiveBlock(),
		"non-TTY must disable live block (D4.1-21)")
}

// ---------------------------------------------------------------------------
// Phase 04.2-05 Task 1: progressEvent extension + attrMap.strSlice + dispatch
// ---------------------------------------------------------------------------
//
// These tests pin the slog-record → progressEvent translation for the new
// `event=result_bound` shape (D4.2-15): alias (string), keys ([]string),
// path (string). The buildProgressEvent function gains Alias + Keys
// extraction; attrMap gains a strSlice helper that tolerates both
// []string (pre-Resolve) and []any (post-Resolve degradation per slog
// docs); renderBazelLine dispatches event=result_bound to a new
// renderResultBound method.

// TestBuildProgressEvent_ResultBound: a slog.Record with attrs
// `{event:result_bound, alias:X, keys:[c,a,b], path:0a}` translates
// into a progressEvent with Kind="result_bound", Alias="X",
// Keys=["c","a","b"], Path="0a". Insertion order preserved (D3-23).
func TestBuildProgressEvent_ResultBound(t *testing.T) {
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "skytime", 0)
	r.AddAttrs(
		slog.String("event", "result_bound"),
		slog.String("alias", "X"),
		slog.Any("keys", []string{"c", "a", "b"}),
		slog.String("path", "0a"),
	)

	ev := buildProgressEvent(r)
	require.Equal(t, "result_bound", ev.Kind, "Kind mirrors event attr")
	require.Equal(t, "X", ev.Alias, "Alias from alias attr")
	require.Equal(t, []string{"c", "a", "b"}, ev.Keys,
		"Keys preserve source-insertion order (D3-23)")
	require.Equal(t, "0a", ev.Path, "Path attribute carried through")
}

// TestAttrMap_StrSlice_HandlesStringSlice: a slog.Value carrying a
// []string returns the same slice from strSlice.
func TestAttrMap_StrSlice_HandlesStringSlice(t *testing.T) {
	m := attrMap{"keys": slog.AnyValue([]string{"a", "b"})}
	require.Equal(t, []string{"a", "b"}, m.strSlice("keys"))
}

// TestAttrMap_StrSlice_MissingReturnsNil: absent key yields nil (defensive
// — caller treats nil as "no keys to render").
func TestAttrMap_StrSlice_MissingReturnsNil(t *testing.T) {
	m := attrMap{}
	require.Nil(t, m.strSlice("keys"))
}

// TestAttrMap_StrSlice_WrongTypeReturnsNil: a non-[]string value yields
// nil rather than panicking (defensive — protects the renderer from a
// malformed event).
func TestAttrMap_StrSlice_WrongTypeReturnsNil(t *testing.T) {
	m := attrMap{"keys": slog.IntValue(42)}
	require.Nil(t, m.strSlice("keys"))
}

// TestAttrMap_StrSlice_HandlesAnySlice: a slog.Value carrying []any
// (the post-Resolve degraded shape per slog docs) is coerced
// element-by-element back to []string. Mixed-type elements yield "" for
// non-string entries rather than panicking.
func TestAttrMap_StrSlice_HandlesAnySlice(t *testing.T) {
	pure := attrMap{"keys": slog.AnyValue([]any{"a", "b", "c"})}
	require.Equal(t, []string{"a", "b", "c"}, pure.strSlice("keys"),
		"[]any with all strings coerces to []string")

	mixed := attrMap{"keys": slog.AnyValue([]any{"a", 1, "c"})}
	require.Equal(t, []string{"a", "", "c"}, mixed.strSlice("keys"),
		"[]any with non-string element yields empty string for that slot (defensive)")
}

// TestRenderBazelLine_DispatchesResultBound: a slog.Record with
// event=result_bound routes to renderResultBound. Asserted via the
// captured output containing the load-bearing `→ ctx.<alias>` token.
// (Exact format pinned by Task 2 tests; this test only verifies dispatch.)
func TestRenderBazelLine_DispatchesResultBound(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "result_bound"),
		slog.String("alias", "myalias"),
		slog.Any("keys", []string{"x", "y"}),
		slog.String("path", "0a"),
	)

	got := progressOut.String()
	require.Contains(t, got, "→ ctx.myalias",
		"renderBazelLine must dispatch event=result_bound to renderResultBound. Output: %q", got)
}

// ---------------------------------------------------------------------------
// Quick 260503-rhy: render if_cond + for_each_parallel as scopes
// ---------------------------------------------------------------------------
//
// if_cond and for_each_parallel are SCOPES, not steps. The renderer emits
// a header (▶ branch / ▶ open), indented children (4 spaces per pathDepth),
// and a footer (✓/✗ ms) for these kinds. Leaf kinds (step / script /
// call_flow) keep the dispatch+complete row pair (qx1 shape) but inherit
// the depth-based indent rule from path.

// TestPathDepth: pin every D-RHY-06 case and a defensive multi-segment
// case. pathDepth is a pure leaf function shared by both renderers.
func TestPathDepth(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{
		{"", 0},
		{"3", 0},
		{"3a", 1},
		{"3b", 1},
		{"3.0", 1},
		{"3.1", 1},
		{"3a.0", 2},
		{"3.0.0", 2},
		{"3.0a", 2},
		{"3a.0.1b", 4}, // defensive — 2 dots + 2 letter-suffix segments = 4
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			require.Equal(t, tc.want, pathDepth(tc.path),
				"pathDepth(%q) = %d, want %d", tc.path, pathDepth(tc.path), tc.want)
		})
	}
}

// TestProgress_IfCond_RendersAsScope: drives a full if_cond sequence and
// asserts the new SCOPE rendering shape (header + indented children +
// footer with no arrow).
func TestProgress_IfCond_RendersAsScope(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		// step_dispatch idx=3 kind=if_cond — should emit NOTHING (D-RHY-01).
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 3), slog.Int("total", 3),
			slog.String("kind", "if_cond"),
			slog.String("label", "cond"),
			slog.String("path", "3"),
		}},
		// branch idx=3 path=3 branch=then — emits HEADER (D-RHY-03/10).
		{"skytime", []slog.Attr{
			slog.String("event", "branch"),
			slog.Int("idx", 3),
			slog.String("path", "3"),
			slog.String("branch", "then"),
		}},
		// child step inside the then branch (path=3a → depth 1 → 4-space indent).
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 1), slog.Int("total", 1),
			slog.String("kind", "step"),
			slog.String("label", "Get branches"),
			slog.String("path", "3a"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 1), slog.Int("total", 1),
			slog.String("kind", "step"),
			slog.String("label", "Get branches"),
			slog.String("path", "3a"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 445),
			slog.String("summary", "status=200"),
		}},
		// step_complete idx=3 kind=if_cond — emits FOOTER (D-RHY-04).
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 3), slog.Int("total", 3),
			slog.String("kind", "if_cond"),
			slog.String("label", "cond"),
			slog.String("path", "3"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 446),
			slog.String("summary", ""),
		}},
	})

	// (a) NO bare dispatch row for if_cond — no line should contain
	// "[3/3]" + "if_cond" WITHOUT either "▶" or "✓"/"✗".
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[3/3]") && strings.Contains(line, "if_cond") {
			hasArrow := strings.Contains(line, "▶")
			hasMarker := strings.Contains(line, "✓") || strings.Contains(line, "✗")
			require.True(t, hasArrow || hasMarker,
				"any [3/3] if_cond line must be either header (▶) or footer (✓/✗); bare dispatch is forbidden. Line: %q", line)
		}
	}

	// (b) EXACTLY one header line: [3/3] + if_cond + cond + ▶ then.
	headerCount := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[3/3]") &&
			strings.Contains(line, "if_cond") &&
			strings.Contains(line, "cond") &&
			strings.Contains(line, "▶ then") {
			headerCount++
		}
	}
	require.Equal(t, 1, headerCount,
		"expected exactly 1 if_cond header line (▶ then). Output: %q", out)

	// (c) EXACTLY one footer line: [3/3] + if_cond + cond + ✓ 446ms;
	// MUST NOT contain "▶" or "→".
	footerCount := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[3/3]") &&
			strings.Contains(line, "if_cond") &&
			strings.Contains(line, "cond") &&
			strings.Contains(line, "✓ 446ms") {
			require.NotContains(t, line, "▶",
				"footer line must NOT contain '▶'. Line: %q", line)
			require.NotContains(t, line, "→",
				"footer line must NOT contain '→'. Line: %q", line)
			footerCount++
		}
	}
	require.Equal(t, 1, footerCount,
		"expected exactly 1 if_cond footer line (✓ 446ms, no arrow). Output: %q", out)

	// (d) child step row: starts with FOUR spaces of indent, contains
	// step + Get branches + ✓ 445ms + status=200.
	childCount := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Get branches") &&
			strings.Contains(line, "✓ 445ms") &&
			strings.Contains(line, "status=200") {
			require.True(t, strings.HasPrefix(line, "    "),
				"child step row must start with FOUR spaces of indent (path 3a → depth 1). Line: %q", line)
			require.False(t, strings.HasPrefix(line, "        "),
				"child step row must NOT start with EIGHT spaces (depth 1, not 2). Line: %q", line)
			childCount++
		}
	}
	require.GreaterOrEqual(t, childCount, 1,
		"expected child step completion line. Output: %q", out)

	// (e) header + footer must NOT be indented (parent path "3" → depth 0).
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[3/3]") &&
			strings.Contains(line, "if_cond") &&
			(strings.Contains(line, "▶ then") || strings.Contains(line, "✓ 446ms")) {
			require.False(t, strings.HasPrefix(line, " "),
				"top-level if_cond header/footer must NOT be indented. Line: %q", line)
		}
	}
}

// TestProgress_IfCond_HeaderHasYellowArrow_OnTTY: same sequence with
// ForceTTY=true; assert raw output contains ansiYellow + "▶" + ansiReset
// on the header line; footer line must NOT contain ansiYellow.
func TestProgress_IfCond_HeaderHasYellowArrow_OnTTY(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandlerWithOptions(passthrough, &progressOut, progressHandlerOptions{
		Verbose:  true, // verbose=true keeps us on the static path on a TTY
		ForceTTY: boolPtr(true),
	})
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_dispatch"),
		slog.Int("idx", 3), slog.Int("total", 3),
		slog.String("kind", "if_cond"),
		slog.String("label", "cond"),
		slog.String("path", "3"),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "branch"),
		slog.Int("idx", 3),
		slog.String("path", "3"),
		slog.String("branch", "then"),
	)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_complete"),
		slog.Int("idx", 3), slog.Int("total", 3),
		slog.String("kind", "if_cond"),
		slog.String("label", "cond"),
		slog.String("path", "3"),
		slog.String("status", "ok"),
		slog.Int64("duration_ms", 1),
		slog.String("summary", ""),
	)

	raw := progressOut.String()
	require.Contains(t, raw, ansiYellow+"▶"+ansiReset,
		"header arrow ▶ must be wrapped in ansiYellow + ansiReset. Output: %q", raw)

	// Footer line must NOT carry ansiYellow.
	stripped := stripAnsiTest(raw)
	for i, sLine := range strings.Split(stripped, "\n") {
		if strings.Contains(sLine, "✓ 1ms") {
			// Find the same line in the raw output by index.
			rawLines := strings.Split(raw, "\n")
			if i < len(rawLines) {
				require.NotContains(t, rawLines[i], ansiYellow,
					"footer line must NOT carry ansiYellow. Raw line: %q", rawLines[i])
			}
		}
	}
}

// TestProgress_ForEachParallel_RendersAsScope: drives a for_each_parallel
// sequence over 3 items; pins header + indented children + footer.
func TestProgress_ForEachParallel_RendersAsScope(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		// HEADER: step_dispatch idx=2 kind=for_each_parallel — emits "▶ open".
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 2), slog.Int("total", 3),
			slog.String("kind", "for_each_parallel"),
			slog.String("label", "items=3"),
			slog.String("path", "2"),
		}},
		// 3 child step iterations — paths 2.0, 2.1, 2.2 (depth 1 → 4-space indent).
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 0), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "Read x"),
			slog.String("path", "2.0"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 0), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "Read x"),
			slog.String("path", "2.0"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 234),
			slog.String("summary", "status=200"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 1), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "Read y"),
			slog.String("path", "2.1"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 1), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "Read y"),
			slog.String("path", "2.1"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 250),
			slog.String("summary", "status=200"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 2), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "Read z"),
			slog.String("path", "2.2"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 2), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "Read z"),
			slog.String("path", "2.2"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 246),
			slog.String("summary", "status=200"),
		}},
		// FOOTER: step_complete kind=for_each_parallel.
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 2), slog.Int("total", 3),
			slog.String("kind", "for_each_parallel"),
			slog.String("label", "items=3"),
			slog.String("path", "2"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 730),
			slog.String("summary", ""),
		}},
	})

	// (a) header line: [2/ + for_each_parallel + items=3 + ▶ open.
	headerCount := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[2/") &&
			strings.Contains(line, "for_each_parallel") &&
			strings.Contains(line, "items=3") &&
			strings.Contains(line, "▶ open") {
			headerCount++
			require.False(t, strings.HasPrefix(line, " "),
				"top-level for_each header must NOT be indented. Line: %q", line)
		}
	}
	require.Equal(t, 1, headerCount,
		"expected exactly 1 for_each_parallel header. Output: %q", out)

	// (b) footer line: ✓ 730ms; NO arrow.
	footerCount := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[2/") &&
			strings.Contains(line, "for_each_parallel") &&
			strings.Contains(line, "items=3") &&
			strings.Contains(line, "✓ 730ms") {
			require.NotContains(t, line, "▶",
				"footer must NOT contain '▶'. Line: %q", line)
			require.NotContains(t, line, "→",
				"footer must NOT contain '→'. Line: %q", line)
			footerCount++
			require.False(t, strings.HasPrefix(line, " "),
				"top-level for_each footer must NOT be indented. Line: %q", line)
		}
	}
	require.Equal(t, 1, footerCount,
		"expected exactly 1 for_each_parallel footer. Output: %q", out)

	// (c) THREE child step lines, each indented 4 spaces.
	indentedChildCount := 0
	labels := []string{"Read x", "Read y", "Read z"}
	for _, lbl := range labels {
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, lbl) &&
				strings.Contains(line, "✓") &&
				strings.HasPrefix(line, "    ") {
				require.False(t, strings.HasPrefix(line, "        "),
					"child must be indented 4 (not 8) spaces for path 2.N. Line: %q", line)
				found = true
				break
			}
		}
		if found {
			indentedChildCount++
		}
	}
	require.Equal(t, 3, indentedChildCount,
		"expected 3 indented child step completion lines (Read x, y, z). Output: %q", out)
}

// TestProgress_NestedScope_DoubleIndent: nested case — for_each at path "3",
// inner if_cond at path "3.0", inner step at "3.0a". The inner step row
// MUST start with EIGHT spaces of indent (depth 2).
func TestProgress_NestedScope_DoubleIndent(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		// outer for_each header
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 3), slog.Int("total", 3),
			slog.String("kind", "for_each_parallel"),
			slog.String("label", "items=1"),
			slog.String("path", "3"),
		}},
		// inner if_cond at path 3.0 — branch then (depth 1: 4-space indent on header)
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 0), slog.Int("total", 1),
			slog.String("kind", "if_cond"),
			slog.String("label", "cond"),
			slog.String("path", "3.0"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "branch"),
			slog.Int("idx", 0),
			slog.String("path", "3.0"),
			slog.String("branch", "then"),
		}},
		// innermost step at path 3.0a — depth 2 → 8-space indent
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 1), slog.Int("total", 1),
			slog.String("kind", "step"),
			slog.String("label", "deep step"),
			slog.String("path", "3.0a"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 1), slog.Int("total", 1),
			slog.String("kind", "step"),
			slog.String("label", "deep step"),
			slog.String("path", "3.0a"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 12),
			slog.String("summary", "status=200"),
		}},
	})

	// inner step row at path "3.0a" must start with 8 spaces (depth 2).
	found := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "deep step") &&
			strings.Contains(line, "✓ 12ms") {
			require.True(t, strings.HasPrefix(line, "        "),
				"path 3.0a (depth 2) must indent 8 spaces. Line: %q", line)
			found = true
			break
		}
	}
	require.True(t, found, "expected innermost step completion line. Output: %q", out)
}

// TestProgress_LeafKinds_KeepExistingShape: top-level step dispatch +
// complete still emit BOTH events (qx1 shape preserved), neither indented.
func TestProgress_LeafKinds_KeepExistingShape(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 1), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "leaf"),
			slog.String("path", "1"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 1), slog.Int("total", 3),
			slog.String("kind", "step"),
			slog.String("label", "leaf"),
			slog.String("path", "1"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 50),
			slog.String("summary", "status=200"),
		}},
	})

	// Both events render. Dispatch row: contains [1/3] + step + leaf.
	dispatchFound, completeFound := false, false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[1/3]") &&
			strings.Contains(line, "step") &&
			strings.Contains(line, "leaf") {
			require.False(t, strings.HasPrefix(line, " "),
				"top-level leaf row must not be indented. Line: %q", line)
			if strings.Contains(line, "✓") {
				completeFound = true
			} else {
				dispatchFound = true
			}
		}
	}
	require.True(t, dispatchFound, "leaf dispatch row must render. Output: %q", out)
	require.True(t, completeFound, "leaf completion row must render. Output: %q", out)
}

// TestProgress_StepDispatch_IfCond_Suppressed: a SOLO step_dispatch
// idx=3 kind=if_cond with no branch/complete must produce ZERO output
// on the renderer (D-RHY-01 — header delegated to branch event).
func TestProgress_StepDispatch_IfCond_Suppressed(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 3), slog.Int("total", 3),
			slog.String("kind", "if_cond"),
			slog.String("label", "cond"),
			slog.String("path", "3"),
		}},
	})

	require.Equal(t, "", out,
		"solo if_cond step_dispatch must produce zero output (D-RHY-01). Output: %q", out)
}

// TestProgress_StepComplete_IfCond_NoBranchSuffix: a branch event followed
// by step_complete kind=if_cond emits a HEADER (▶ then) and a separate
// FOOTER (✓ 1ms, no arrow). The footer must NOT contain "→ then".
func TestProgress_StepComplete_IfCond_NoBranchSuffix(t *testing.T) {
	out := emitProgress(t, []struct {
		msg   string
		attrs []slog.Attr
	}{
		{"skytime", []slog.Attr{
			slog.String("event", "step_dispatch"),
			slog.Int("idx", 3), slog.Int("total", 3),
			slog.String("kind", "if_cond"),
			slog.String("label", "cond"),
			slog.String("path", "3"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "branch"),
			slog.Int("idx", 3),
			slog.String("path", "3"),
			slog.String("branch", "then"),
		}},
		{"skytime", []slog.Attr{
			slog.String("event", "step_complete"),
			slog.Int("idx", 3), slog.Int("total", 3),
			slog.String("kind", "if_cond"),
			slog.String("label", "cond"),
			slog.String("path", "3"),
			slog.String("status", "ok"),
			slog.Int64("duration_ms", 1),
			slog.String("summary", ""),
		}},
	})

	// Footer line carries ✓ 1ms, NOT "→ then".
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "✓ 1ms") {
			require.NotContains(t, line, "→ then",
				"footer line must NOT contain branch suffix '→ then'. Line: %q", line)
			require.NotContains(t, line, "▶",
				"footer line must NOT contain '▶'. Line: %q", line)
		}
	}

	// Header line carries ▶ then (emitted by renderBranch).
	headerFound := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "▶ then") {
			require.Contains(t, line, "if_cond", "header line must carry kind word")
			require.Contains(t, line, "cond", "header line must carry label")
			headerFound = true
		}
	}
	require.True(t, headerFound,
		"header line containing '▶ then' must be present. Output: %q", out)
}
