package deliveries

import "time"

// Delivery records one webhook delivery surfaced on the dashboard
// (UI-02). All sensitive header values are pre-redacted by
// RedactHeaders before this struct is constructed — see Pitfall 6
// (slot-reuse: callers MUST pass the redacted-map copy, not the raw
// http.Header).
//
// JSON tags use snake_case so the SSE payload shape stays
// browser-friendly.
type Delivery struct {
	ID             string            `json:"id"`
	ReceivedAt     time.Time         `json:"received_at"`
	Source         string            `json:"source"` // e.g., "github.webhook", "http.webhook"
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	Headers        map[string]string `json:"headers"` // already redacted
	PayloadSummary string            `json:"payload_summary"`
	PayloadFull    string            `json:"payload_full,omitempty"` // full body for <details> expansion; may be truncated to PayloadFullMax
	Status         int               `json:"status"`
	WorkflowIDs    []string          `json:"workflow_ids,omitempty"`
	ErrorClass     string            `json:"error_class,omitempty"`
}

// PayloadFullMax caps PayloadFull at 64KB so a single huge body
// cannot bloat the buffer beyond ~6.5MB total. Per D-7.3-22 the
// buffer is non-persistent in-memory only.
const PayloadFullMax = 64 * 1024
