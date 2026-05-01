package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// progressHandler wraps another slog.Handler and intercepts records
// carrying a "flow_name" attribute, rendering them as compact progress
// lines on its output writer. Records without "flow_name" pass through
// to the wrapped handler unchanged.
//
// Phase 4 W5 ships the handler; the interpreter walkers (pkg/interpreter)
// will be updated in Phase 5/6 to emit `flow_name`/`step_kind`/`action_kind`
// attrs at each tick. Until then, this handler is transparently a no-op
// (no records carry the attribute → all pass through).
//
// Format (D4-06 spec):
//
//	[skytime] flow=<flow_name> step=<step_kind> action=<action_kind> at <pos> elapsed=<ms>ms <message>
//
// Missing attributes are dropped from the line; the ordering is fixed
// for greppability.
type progressHandler struct {
	wrapped slog.Handler
	out     io.Writer
}

// newProgressHandler returns a handler that writes progress lines to
// out and delegates everything else to wrapped.
func newProgressHandler(wrapped slog.Handler, out io.Writer) *progressHandler {
	return &progressHandler{wrapped: wrapped, out: out}
}

// Enabled delegates to the wrapped handler. Skytime-namespaced records
// (those carrying flow_name) are rendered to the progress writer
// regardless of level — they are progress events, not log severity
// signals — but slog calls Enabled BEFORE invoking Handle, so we must
// not return false for Skytime records. Returning the wrapped handler's
// answer is a pragmatic v1 choice: production CLI runs at INFO+, and
// the interpreter emits progress at INFO; if a future emitter uses
// DEBUG level, raise --debug or override the wrapped handler's level.
func (p *progressHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return p.wrapped.Enabled(ctx, level)
}

// Handle routes the record to the progress writer when it carries the
// flow_name attribute, or to the wrapped handler otherwise.
func (p *progressHandler) Handle(ctx context.Context, r slog.Record) error {
	if hasAttr(r, "flow_name") {
		return p.renderProgressLine(r)
	}
	return p.wrapped.Handle(ctx, r)
}

// WithAttrs returns a new progressHandler whose wrapped handler has the
// attrs applied. The progress writer is unchanged — pre-applied attrs
// are part of the wrapped handler's state.
func (p *progressHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &progressHandler{wrapped: p.wrapped.WithAttrs(attrs), out: p.out}
}

// WithGroup returns a new progressHandler whose wrapped handler is
// scoped under the named group. The progress writer is unchanged.
func (p *progressHandler) WithGroup(name string) slog.Handler {
	return &progressHandler{wrapped: p.wrapped.WithGroup(name), out: p.out}
}

// renderProgressLine formats one Skytime progress record. Layout:
//
//	[skytime] flow=<f> step=<k> action=<a> at <pos> elapsed=<ms>ms <message>
//
// Missing attrs are skipped; ordering is fixed for greppability.
func (p *progressHandler) renderProgressLine(r slog.Record) error {
	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	parts := []string{"[skytime]"}
	if v, ok := attrs["flow_name"]; ok {
		parts = append(parts, "flow="+v)
	}
	if v, ok := attrs["step_kind"]; ok {
		parts = append(parts, "step="+v)
	}
	if v, ok := attrs["action_kind"]; ok {
		parts = append(parts, "action="+v)
	}
	if v, ok := attrs["pos"]; ok {
		parts = append(parts, "at "+v)
	}
	if v, ok := attrs["elapsed_ms"]; ok {
		parts = append(parts, "elapsed="+v+"ms")
	}
	line := strings.Join(parts, " ")
	if r.Message != "" {
		line = line + " " + r.Message
	}
	_, err := fmt.Fprintln(p.out, line)
	return err
}

// hasAttr returns true when r contains an attribute with the given key.
func hasAttr(r slog.Record, key string) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = true
			return false // stop iteration
		}
		return true
	})
	return found
}
