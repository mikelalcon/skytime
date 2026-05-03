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
				slog.String("path", "1"),
				slog.String("status", "ok"),
				slog.Int64("duration_ms", 234),
				slog.String("summary", "status=200"),
			}},
			want: expect{"✓", "234ms", "status=200"},
		},
		{
			name: "step_complete err",
			call: call{"skytime", []slog.Attr{
				slog.String("event", "step_complete"),
				slog.Int("idx", 2), slog.Int("total", 3),
				slog.String("kind", "step"),
				slog.String("path", "2"),
				slog.String("status", "err"),
				slog.Int64("duration_ms", 120),
				slog.String("summary", "connection refused"),
			}},
			want: expect{"✗", "120ms", "connection refused"},
		},
		{
			name: "step_dispatch if_cond kind",
			call: call{"skytime", []slog.Attr{
				slog.String("event", "step_dispatch"),
				slog.Int("idx", 3), slog.Int("total", 3),
				slog.String("kind", "if_cond"),
				slog.String("label", "ctx.health"),
				slog.String("path", "3"),
			}},
			want: expect{"[3/3]", "if_cond", "ctx.health"},
		},
		{
			name: "branch then arrow",
			call: call{"skytime", []slog.Attr{
				slog.String("event", "branch"),
				slog.Int("idx", 3),
				slog.String("path", "3"),
				slog.String("branch", "then"),
			}},
			want: expect{"→", "then"},
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
