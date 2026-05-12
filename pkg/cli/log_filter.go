package cli

import (
	"context"
	"log/slog"
)

// logKindFilterHandler is a slog.Handler decorator that drops records
// where `kind=log` AND `event ∈ {step_dispatch, step_complete}`. Every
// other record — including the user-message record emitted by the
// log.<level> walker (no event attr, msg starts with "[skytime/log] ")
// and unrelated kinds (script, step, for_each_parallel, if_cond, ...)
// — is delegated to the wrapped handler unchanged.
//
// Per Phase 7.2.1 D-7.2.1-13: human-mode renderer SUPPRESSES kind=log
// dispatch/complete frames; the user-message record passes through.
// Per D-7.2.1-14: JSON-log mode emits ALL THREE records verbatim — so
// this handler is wired into the non-JSON branch of setupServerLogging
// ONLY (the JSON branch in setupServerLogging skips this wrap).
// Per D-7.2.1-15: the discriminator is the literal string `"log"` on
// the `kind` attr (NOT `kind="log.<level>"`) — the slog record's own
// level carries info/warn/error/debug.
//
// First-of-its-kind in pkg/cli — there is no precedent for an
// attribute-filtering slog.Handler decorator. progressHandler is the
// closest cousin (also implements slog.Handler) but it dispatches by
// event type rather than filtering kinds; this decorator is simpler.
type logKindFilterHandler struct {
	wrapped slog.Handler
}

// newLogKindFilterHandler wraps a slog.Handler so kind=log
// step_dispatch/step_complete records are dropped before reaching the
// underlying writer. The user-message record (msg="[skytime/log] ...")
// has no `event` attr and is delegated unchanged.
func newLogKindFilterHandler(wrapped slog.Handler) *logKindFilterHandler {
	return &logKindFilterHandler{wrapped: wrapped}
}

// Enabled defers entirely to the wrapped handler — this decorator does
// not filter by level.
func (h *logKindFilterHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.wrapped.Enabled(ctx, level)
}

// Handle drops records matching kind=log + event ∈ {step_dispatch, step_complete}
// (D-7.2.1-13 / D-7.2.1-15: single `kind="log"` discriminator). Everything
// else is delegated. Inspection uses r.Attrs (slog's canonical iteration
// API — handles WithAttrs-prepended attrs too).
func (h *logKindFilterHandler) Handle(ctx context.Context, r slog.Record) error {
	var (
		kind  string
		event string
	)
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "kind":
			kind = a.Value.String()
		case "event":
			event = a.Value.String()
		}
		// Continue iterating only until we've seen both fields — bounded
		// short-circuit; slog.Record.Attrs caps at ~20 attrs in practice.
		return !(kind != "" && event != "")
	})
	if kind == "log" && (event == "step_dispatch" || event == "step_complete") {
		return nil
	}
	return h.wrapped.Handle(ctx, r)
}

// WithAttrs returns a new logKindFilterHandler wrapping the wrapped
// handler's WithAttrs result, preserving the decorator across attr
// chaining (slog.Logger.With(...) flows through here).
func (h *logKindFilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &logKindFilterHandler{wrapped: h.wrapped.WithAttrs(attrs)}
}

// WithGroup returns a new logKindFilterHandler wrapping the wrapped
// handler's WithGroup result.
func (h *logKindFilterHandler) WithGroup(name string) slog.Handler {
	return &logKindFilterHandler{wrapped: h.wrapped.WithGroup(name)}
}
