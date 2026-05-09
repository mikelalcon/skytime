package receiver

import (
	"log/slog"
	"time"
)

// requestRecord accumulates the per-request log fields during handler
// execution. Emitted via emit() at handler exit (defer-after-response
// per § Pattern 4 in 07.1-RESEARCH.md). LOCKED FIELDS — D-7.1-15:
//
//	method, path, status, duration_ms, source_kind, event,
//	flows_dispatched, workflow_ids, error_class.
//
// CRITICAL: NO request body, NO response body, NO header VALUES (only
// header NAMES if useful), NO secrets. The errorClass is a stable
// taxonomy from status.go's const block.
type requestRecord struct {
	method          string
	path            string
	status          int
	start           time.Time
	sourceKind      string
	event           string   // "" for non-GitHub sources
	flowsDispatched []string // names of flows that fired this delivery
	workflowIDs     []string // composed WorkflowIDs that landed
	errorClass      string   // one of the errorClass* consts above
}

// emit writes the log record. Always emitted via defer at handler
// exit — no early-returns short-circuit it.
func (r *requestRecord) emit(logger *slog.Logger) {
	durationMs := time.Since(r.start).Milliseconds()
	attrs := []any{
		"method", r.method,
		"path", r.path,
		"status", r.status,
		"duration_ms", durationMs,
		"source_kind", r.sourceKind,
		"error_class", r.errorClass,
	}
	if r.event != "" {
		attrs = append(attrs, "event", r.event)
	}
	if r.flowsDispatched != nil {
		attrs = append(attrs, "flows_dispatched", r.flowsDispatched)
	}
	if r.workflowIDs != nil {
		attrs = append(attrs, "workflow_ids", r.workflowIDs)
	}
	logger.Info("webhook delivery", attrs...)
}
