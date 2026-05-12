package cli

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recordingHandler is a minimal slog.Handler that captures records into
// a buffer (JSON-encoded one per line) so tests can assert which records
// reached the wrapped handler.
type recordingHandler struct {
	inner slog.Handler
	buf   *bytes.Buffer
}

func newRecordingHandler() (*recordingHandler, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &recordingHandler{
		inner: slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
		buf:   buf,
	}, buf
}

func (h *recordingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *recordingHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.inner.Handle(ctx, r)
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &recordingHandler{inner: h.inner.WithAttrs(attrs), buf: h.buf}
}

func (h *recordingHandler) WithGroup(name string) slog.Handler {
	return &recordingHandler{inner: h.inner.WithGroup(name), buf: h.buf}
}

func makeRecord(level slog.Level, msg string, kvs ...any) slog.Record {
	r := slog.NewRecord(time.Now(), level, msg, 0)
	r.Add(kvs...)
	return r
}

func TestLogKindFilterHandler_SuppressesLogDispatch(t *testing.T) {
	rec, buf := newRecordingHandler()
	h := newLogKindFilterHandler(rec)
	err := h.Handle(context.Background(), makeRecord(
		slog.LevelInfo, "skytime",
		"event", "step_dispatch", "kind", "log", "label", "log",
	))
	require.NoError(t, err)
	require.Empty(t, buf.String(), "kind=log step_dispatch must be dropped")
}

func TestLogKindFilterHandler_SuppressesLogComplete(t *testing.T) {
	rec, buf := newRecordingHandler()
	h := newLogKindFilterHandler(rec)
	err := h.Handle(context.Background(), makeRecord(
		slog.LevelInfo, "skytime",
		"event", "step_complete", "kind", "log", "status", "ok",
	))
	require.NoError(t, err)
	require.Empty(t, buf.String(), "kind=log step_complete must be dropped")
}

func TestLogKindFilterHandler_PassesNonLogEvents(t *testing.T) {
	cases := []struct {
		name  string
		event string
		kind  string
	}{
		{"script_dispatch", "step_dispatch", "script"},
		{"script_complete", "step_complete", "script"},
		{"step_dispatch", "step_dispatch", "step"},
		{"foreach_dispatch", "step_dispatch", "for_each_parallel"},
		{"flow_start", "flow_start", ""},
		{"flow_complete", "flow_complete", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, buf := newRecordingHandler()
			h := newLogKindFilterHandler(rec)
			args := []any{"event", c.event}
			if c.kind != "" {
				args = append(args, "kind", c.kind)
			}
			err := h.Handle(context.Background(), makeRecord(slog.LevelInfo, "skytime", args...))
			require.NoError(t, err)
			require.NotEmpty(t, buf.String(), "non-log event must pass through")
			require.Contains(t, buf.String(), `"event":"`+c.event+`"`)
		})
	}
}

func TestLogKindFilterHandler_PassesUserMessageRecord(t *testing.T) {
	// CRITICAL: the user-message record from Plan 03 has NO `event` attr.
	// msg starts with "[skytime/log] " (walker prepends this). It MUST
	// reach the wrapped handler.
	rec, buf := newRecordingHandler()
	h := newLogKindFilterHandler(rec)
	err := h.Handle(context.Background(), makeRecord(
		slog.LevelInfo, "[skytime/log] weekly digest complete",
		"kind", "log", // walker may or may not attach kind to user-message; either way it survives — no `event` attr
	))
	require.NoError(t, err)
	require.Contains(t, buf.String(), "[skytime/log] weekly digest complete",
		"user-message record (no event attr) must pass through even with kind=log")
}

func TestLogKindFilterHandler_PassesGenericRecord(t *testing.T) {
	rec, buf := newRecordingHandler()
	h := newLogKindFilterHandler(rec)
	err := h.Handle(context.Background(), makeRecord(
		slog.LevelInfo, "skytime workflow start",
		"flow_name", "weekly_digest",
	))
	require.NoError(t, err)
	require.Contains(t, buf.String(), "skytime workflow start")
}

func TestLogKindFilterHandler_EnabledDelegates(t *testing.T) {
	rec, _ := newRecordingHandler()
	h := newLogKindFilterHandler(rec)
	require.True(t, h.Enabled(context.Background(), slog.LevelInfo))
	require.True(t, h.Enabled(context.Background(), slog.LevelDebug)) // recordingHandler uses LevelDebug
}

func TestLogKindFilterHandler_WithAttrsPreservesDecorator(t *testing.T) {
	rec, buf := newRecordingHandler()
	h := newLogKindFilterHandler(rec)
	h2 := h.WithAttrs([]slog.Attr{slog.String("server", "skytime")})
	_, isFilter := h2.(*logKindFilterHandler)
	require.True(t, isFilter, "WithAttrs must return *logKindFilterHandler to preserve filtering")

	// Sanity: filter still works after WithAttrs.
	err := h2.Handle(context.Background(), makeRecord(
		slog.LevelInfo, "skytime",
		"event", "step_dispatch", "kind", "log",
	))
	require.NoError(t, err)
	require.Empty(t, buf.String())
}

func TestLogKindFilterHandler_WithGroupPreservesDecorator(t *testing.T) {
	rec, _ := newRecordingHandler()
	h := newLogKindFilterHandler(rec)
	h2 := h.WithGroup("workflow")
	_, isFilter := h2.(*logKindFilterHandler)
	require.True(t, isFilter)
}
