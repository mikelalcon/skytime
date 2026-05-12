package events

import (
	"time"

	"github.com/mikelalcon/skytime/pkg/cli/server/web/deliveries"
)

// Event is the wire shape the broadcaster fans out. Name is the SSE event
// name ("snapshot", "workflow_started", "workflow_status_changed",
// "workflow_replayed", "delivery_received", "shutdown"). Payload is
// JSON-marshaled by the SSE handler — the broadcaster does not interpret
// it.
type Event struct {
	Name    string
	Payload any
}

// WorkflowState is the per-workflow row surfaced on the dashboard
// (D-7.3-09 columns + replay count from D-7.3-14).
//
// JSON tags use snake_case so the SSE payload shape stays
// browser-friendly.
type WorkflowState struct {
	WorkflowID    string     `json:"workflow_id"`
	FlowName      string     `json:"flow_name"`
	Status        string     `json:"status"` // running / completed / failed / replayed
	StartTime     time.Time  `json:"start_time"`
	CloseTime     *time.Time `json:"close_time,omitempty"`
	HistoryLength int64      `json:"history_length"`
	ReplayCount   int        `json:"replay_count,omitempty"`
	RawStatus     string     `json:"raw_status,omitempty"` // e.g., "TERMINATED" — for the tooltip
}

// DeliveryState is a re-export alias for [deliveries.Delivery] so the
// events package presents one cohesive surface to the SSE handler (Plan 04)
// without forcing it to import two sibling packages. The underlying type
// is the source of truth (Plan 02) — this alias is purely an
// import-hygiene convenience.
type DeliveryState = deliveries.Delivery

// Snapshot is the one-time payload sent on a new SSE subscription.
// Always captured under the broadcaster mutex (Research Pitfall 1).
type Snapshot struct {
	Workflows  []WorkflowState       `json:"workflows"`
	Deliveries []deliveries.Delivery `json:"deliveries"`
}
