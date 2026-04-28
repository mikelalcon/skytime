package activity

import (
	"context"

	"go.temporal.io/sdk/activity"
)

// BatchProgress is the heartbeat payload emitted after each action completes.
// Phase 2 keeps it minimal; v1.x can add op name, elapsed time, etc. without
// breaking the wire format because Temporal's default DataConverter (JSON)
// tolerates additional fields on round-trip.
//
// D2-16: heartbeat between every action with payload {Action, Total}. The
// struct is deliberately small (two ints) so JSON serialization through
// Temporal's default converter is fast and payload size is predictable.
// Pitfall 6 (non-serializable heartbeat) is closed by keeping every field a
// value type — no func, no chan, no unsafe.Pointer. A sentinel test
// (TestBatchProgress_NoFunctionsOrChannels) defends this at CI time.
type BatchProgress struct {
	// Action is the zero-based index of the action that just completed in
	// the input batch.
	Action int `json:"action"`
	// Total is the count of actions in the input batch (so an external
	// observer can compute progress as Action/Total).
	Total int `json:"total"`
}

// heartbeatEmitter is the small interface ExecuteBatch (02-03) calls between
// actions. The interface lets tests inject fakes that capture emitted payloads
// without spinning up TestActivityEnvironment — the cache + classify tests
// stay pure-Go, fast, and race-clean.
type heartbeatEmitter interface {
	emit(ctx context.Context, progress BatchProgress)
}

// realHeartbeatEmitter is the production implementation registered with the
// worker by activity.New. It calls activity.RecordHeartbeat directly.
//
// Cancellation note (RESEARCH §"Heartbeat-context interaction"): if the
// workflow has cancelled, RecordHeartbeat is fire-and-forget — the SDK logs
// at WARN. The caller (ExecuteBatch in 02-03) MUST check ctx.Err() separately
// after each emit to honor cancellation cooperatively.
type realHeartbeatEmitter struct{}

// emit forwards to activity.RecordHeartbeat. Compile-time enforced via the
// var _ heartbeatEmitter = realHeartbeatEmitter{} assertion in heartbeat_test.go.
func (realHeartbeatEmitter) emit(ctx context.Context, progress BatchProgress) {
	activity.RecordHeartbeat(ctx, progress)
}
