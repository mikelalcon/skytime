package events

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

// DefaultReplayHistoryThreshold is the HistoryLength delta (per poll
// cycle) above which the poller publishes a workflow_replayed event.
// Tunable via PollerConfig.ReplayHistoryThreshold and the CLI flag
// --replay-history-threshold (Plan 04 wires the flag).
//
// 50 is a heuristic — Research Open Question 2 / Pitfall 5. Workflows
// with long retry chains may need a higher threshold; we expose the knob
// rather than guessing for every customer.
const DefaultReplayHistoryThreshold = 50

// listClient is the narrowed client surface the poller uses. Tests
// implement this directly instead of the ~40-method client.Client
// interface (the real client.Client satisfies it transitively).
type listClient interface {
	ListOpenWorkflow(ctx context.Context, req *workflowservice.ListOpenWorkflowExecutionsRequest) (*workflowservice.ListOpenWorkflowExecutionsResponse, error)
	ListClosedWorkflow(ctx context.Context, req *workflowservice.ListClosedWorkflowExecutionsRequest) (*workflowservice.ListClosedWorkflowExecutionsResponse, error)
}

// PollerConfig holds tunable knobs. Zero values fall back to documented
// defaults via applyDefaults.
type PollerConfig struct {
	// Namespace is the Temporal namespace to list workflows in. Defaults
	// to "default".
	Namespace string

	// PollInterval is the cadence between ListWorkflow calls. Defaults to
	// 2 * time.Second per D-7.3-02 / Research Pitfall 4.
	PollInterval time.Duration

	// MaxPageSize is the per-call ListWorkflow page cap. Defaults to 100
	// (last-100-closed per D-7.3-12).
	MaxPageSize int32

	// ReplayHistoryThreshold is the HistoryLength delta above which a
	// workflow_replayed event fires. Defaults to
	// DefaultReplayHistoryThreshold (50).
	ReplayHistoryThreshold int64
}

func (c *PollerConfig) applyDefaults() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.MaxPageSize <= 0 {
		c.MaxPageSize = 100
	}
	if c.ReplayHistoryThreshold <= 0 {
		c.ReplayHistoryThreshold = DefaultReplayHistoryThreshold
	}
}

// Poller polls Temporal's visibility API on a fixed cadence and publishes
// deltas to the broadcaster. ONE poller per server process (D-7.3-02) —
// browser count does not multiply Temporal load.
//
// Event taxonomy (D-7.3-05):
//   - workflow_started — first sight of a WorkflowID
//   - workflow_status_changed — RawStatus enum changed since last tick
//   - workflow_replayed — same workflow still RUNNING but HistoryLength
//     jumped by more than ReplayHistoryThreshold (worker-restart heuristic
//     per Research Pitfall 5)
type Poller struct {
	client      listClient
	broadcaster *Broadcaster
	cfg         PollerConfig
	logger      *slog.Logger

	mu   sync.RWMutex
	last map[string]WorkflowState // keyed by WorkflowID
}

// NewPoller constructs a Poller. c must implement the minimal listClient
// surface (the real client.Client satisfies it).
func NewPoller(c client.Client, b *Broadcaster, cfg PollerConfig, logger *slog.Logger) *Poller {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Poller{
		client:      c,
		broadcaster: b,
		cfg:         cfg,
		logger:      logger,
		last:        map[string]WorkflowState{},
	}
}

// newPollerInternal is the test seam: takes the narrowed listClient
// directly so tests do not need to satisfy the full client.Client.
func newPollerInternal(c listClient, b *Broadcaster, cfg PollerConfig, logger *slog.Logger) *Poller {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Poller{
		client:      c,
		broadcaster: b,
		cfg:         cfg,
		logger:      logger,
		last:        map[string]WorkflowState{},
	}
}

// CurrentSnapshot returns the most recent workflow list. Used as half of
// the broadcaster's SnapshotFunc (the deliveries half comes from the
// receiver's RingBuffer; Plan 04 wires them together).
func (p *Poller) CurrentSnapshot() []WorkflowState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]WorkflowState, 0, len(p.last))
	for _, w := range p.last {
		out = append(out, w)
	}
	return out
}

// Run blocks until ctx is cancelled. Each tick: fetch open + closed,
// diff vs. last snapshot, publish deltas, update last.
func (p *Poller) Run(ctx context.Context) {
	// Initial tick immediately so the first snapshot lands fast (don't
	// wait a full PollInterval for the dashboard to populate on boot).
	p.tick(ctx)

	t := time.NewTicker(p.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

// tick performs one poll cycle. Errors are logged + swallowed (Research
// Pitfall 4: don't crash the loop on transient ListWorkflow failures).
func (p *Poller) tick(ctx context.Context) {
	cur, err := p.fetch(ctx)
	if err != nil {
		p.logger.Warn("workflow poll failed; will retry on next tick", "err", err)
		return
	}
	p.diffAndPublish(cur)
}

// fetch hits ListOpenWorkflow + ListClosedWorkflow once each and merges
// the responses into a single map keyed by WorkflowID. Closed wins on
// duplicate (it carries the terminal Status).
func (p *Poller) fetch(ctx context.Context) (map[string]WorkflowState, error) {
	open, err := p.client.ListOpenWorkflow(ctx, &workflowservice.ListOpenWorkflowExecutionsRequest{
		Namespace:       p.cfg.Namespace,
		MaximumPageSize: p.cfg.MaxPageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list open: %w", err)
	}
	closed, err := p.client.ListClosedWorkflow(ctx, &workflowservice.ListClosedWorkflowExecutionsRequest{
		Namespace:       p.cfg.Namespace,
		MaximumPageSize: p.cfg.MaxPageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list closed: %w", err)
	}
	out := map[string]WorkflowState{}
	for _, e := range open.Executions {
		ws := stateFromInfo(e)
		out[ws.WorkflowID] = ws
	}
	for _, e := range closed.Executions {
		ws := stateFromInfo(e)
		out[ws.WorkflowID] = ws // closed wins on duplicate (terminal state)
	}
	return out, nil
}

// diffAndPublish compares cur against the last snapshot and Publishes
// the appropriate event for each delta. Replay counts are carried
// forward across no-delta ticks so the dashboard's running total
// persists.
func (p *Poller) diffAndPublish(cur map[string]WorkflowState) {
	p.mu.Lock()
	prev := p.last
	for id, w := range cur {
		old, existed := prev[id]
		switch {
		case !existed:
			// First sight of this WorkflowID — workflow_started.
			p.broadcaster.Publish(Event{Name: "workflow_started", Payload: w})
		case old.RawStatus != w.RawStatus:
			// Enum changed — workflow_status_changed. Preserve replay
			// count across the status flip.
			w.ReplayCount = old.ReplayCount
			cur[id] = w
			p.broadcaster.Publish(Event{Name: "workflow_status_changed", Payload: w})
		case w.Status == "running" && (w.HistoryLength-old.HistoryLength) > p.cfg.ReplayHistoryThreshold:
			// Still running but HistoryLength jumped — worker-restart
			// replay heuristic (Research Pitfall 5 / Open Question 2).
			w.ReplayCount = old.ReplayCount + 1
			cur[id] = w
			p.broadcaster.Publish(Event{Name: "workflow_replayed", Payload: w})
		default:
			// No-op tick — preserve replay count so the dashboard's
			// running total persists across normal progression.
			w.ReplayCount = old.ReplayCount
			cur[id] = w
		}
	}
	p.last = cur
	p.mu.Unlock()
}

// stateFromInfo maps a Temporal WorkflowExecutionInfo to the dashboard's
// WorkflowState shape. D-7.3-11 status grouping: CANCELED / TERMINATED /
// TIMED_OUT all render as "failed" in the user-facing bucket.
func stateFromInfo(e *workflowpb.WorkflowExecutionInfo) WorkflowState {
	ws := WorkflowState{
		WorkflowID:    e.Execution.GetWorkflowId(),
		Status:        statusBucket(e.Status),
		HistoryLength: e.HistoryLength,
		RawStatus:     e.Status.String(),
	}
	if e.Type != nil {
		ws.FlowName = e.Type.GetName()
	}
	if e.StartTime != nil {
		ws.StartTime = e.StartTime.AsTime()
	}
	if e.CloseTime != nil {
		ct := e.CloseTime.AsTime()
		ws.CloseTime = &ct
	}
	return ws
}

// statusBucket reduces the WorkflowExecutionStatus enum to four
// user-facing buckets per D-7.3-11 (the replayed bucket is set by the
// differ, not the enum). CANCELED / TERMINATED / TIMED_OUT all map to
// "failed" — D-7.3-11 intentionally collapses these for the teaching
// dashboard.
func statusBucket(s enumspb.WorkflowExecutionStatus) string {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return "running"
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "completed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "failed"
	default:
		return "unknown"
	}
}
