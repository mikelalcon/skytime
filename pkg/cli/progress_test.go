package cli

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

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
