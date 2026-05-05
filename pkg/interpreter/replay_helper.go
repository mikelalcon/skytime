package interpreter

// EventCapture + RunOnceCapturing — public Phase 5 D5-D1 (always-on
// replay determinism) + D5-D4 (event-stream diff scope) infrastructure.
//
// Plan 03 lifts the file-private helper from
// pkg/interpreter/replay_determinism_test.go::runOnceCapturing into a
// non-test API so pkg/testing's tester.run driver (Plan 04) and the
// FirstDivergentEvent reporter (Plan 03 Task 2) compose against the
// same machinery the existing internal replay tests use.
//
// The capture buffer records both:
//
//   - Interpreter slog events emitted via workflow.GetLogger (the
//     same events that drive the live progress block: flow_start,
//     step_dispatch, step_complete, branch, result_bound,
//     flow_complete). Routed via testsuite.SetLogger which routes
//     workflow.GetLogger(ctx) through the supplied log.Logger.
//
//   - Activity-boundary events captured via TestWorkflowEnvironment's
//     SetOnActivityStartedListener / SetOnActivityCompletedListener
//     (Investigation 2 — TestWorkflowEnvironment has NO
//     GetWorkflowHistory, so we use the listener pair to surface the
//     batch dispatched + per-action results).
//
// EventCapture is concurrency-safe under -race; Snapshot+Serialize
// take a defensive copy under the same mutex that guards appends.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/stretchr/testify/mock"

	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// EventCapture is the public capture buffer for replay-determinism
// diffing (D5-D1 + D5-D4). Implements the temporal log.Logger
// interface so testsuite.SetLogger routes interpreter slog events
// through it.
type EventCapture struct {
	mu      sync.Mutex
	records []EventRecord
}

// EventRecord is one captured slog or activity-boundary entry.
//
// Kind is one of:
//   - "slog"               (workflow.GetLogger emission)
//   - "activity_started"   (SetOnActivityStartedListener fire)
//   - "activity_completed" (SetOnActivityCompletedListener fire)
//
// For "slog" records, Level + Message + KV carry the original
// keyvals slice.
//
// For "activity_started" records, Args holds the decoded
// []*dag.ActionRef batch passed to ExecuteBatch.
//
// For "activity_completed" records, Results holds the decoded
// dag.ActionResults slice; Err holds err.Error() when the activity
// failed.
type EventRecord struct {
	Kind    string
	Level   string
	Message string
	KV      []any
	Args    []*dag.ActionRef
	Results dag.ActionResults
	Err     string
}

// NewEventCapture returns a fresh capture buffer.
func NewEventCapture() *EventCapture {
	return &EventCapture{}
}

// Debug / Info / Warn / Error implement log.Logger. The activity
// goroutine calls these via workflow.GetLogger(ctx), the testsuite's
// SetLogger routes them through this capture.
func (c *EventCapture) Debug(msg string, kv ...any) { c.appendSlog("DEBUG", msg, kv) }
func (c *EventCapture) Info(msg string, kv ...any)  { c.appendSlog("INFO", msg, kv) }
func (c *EventCapture) Warn(msg string, kv ...any)  { c.appendSlog("WARN", msg, kv) }
func (c *EventCapture) Error(msg string, kv ...any) { c.appendSlog("ERROR", msg, kv) }

func (c *EventCapture) appendSlog(level, msg string, kv []any) {
	// Defensive copy so callers can mutate kv after the call without
	// corrupting our records.
	out := make([]any, len(kv))
	copy(out, kv)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, EventRecord{Kind: "slog", Level: level, Message: msg, KV: out})
}

// onActivityStarted is the SetOnActivityStartedListener callback. The
// listener type uses the public activity.Info alias.
func (c *EventCapture) onActivityStarted(_ *sdkactivity.Info, _ context.Context, args converter.EncodedValues) {
	var batch []*dag.ActionRef
	if err := args.Get(&batch); err != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.records = append(c.records, EventRecord{
			Kind:    "activity_started",
			Message: fmt.Sprintf("activity_started decode error: %v", err),
		})
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, EventRecord{
		Kind: "activity_started",
		Args: batch,
	})
}

// onActivityCompleted is the SetOnActivityCompletedListener callback.
func (c *EventCapture) onActivityCompleted(_ *sdkactivity.Info, result converter.EncodedValue, err error) {
	var results dag.ActionResults
	if result != nil && result.HasValue() {
		_ = result.Get(&results)
	}
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, EventRecord{
		Kind:    "activity_completed",
		Results: results,
		Err:     errStr,
	})
}

// Snapshot returns a defensive copy of the records slice.
func (c *EventCapture) Snapshot() []EventRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]EventRecord, len(c.records))
	copy(out, c.records)
	return out
}

// Serialize renders the capture as a stable byte string for diff. Per
// record format: "<KIND>|<LEVEL>|<MESSAGE>|<sorted-kv-pairs>\n". KV
// pairs are sorted alphabetically by key — Go map iteration is
// randomized, sorting eliminates a class of false-positive divergence
// reports.
func (c *EventCapture) Serialize() []byte {
	snap := c.Snapshot()
	var b strings.Builder
	for _, r := range snap {
		b.WriteString(serializeOneRecord(r))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// serializeOneRecord renders one EventRecord deterministically. The
// pkg/testing FirstDivergentEvent helper compares record-by-record so
// the per-record format here MUST match what the diff helper uses.
func serializeOneRecord(r EventRecord) string {
	var b strings.Builder
	b.WriteString(r.Kind)
	b.WriteByte('|')
	b.WriteString(r.Level)
	b.WriteByte('|')
	b.WriteString(r.Message)
	switch r.Kind {
	case "slog":
		b.WriteByte('|')
		writeSortedKVs(&b, r.KV)
	case "activity_started":
		b.WriteByte('|')
		// Render the action batch as "kind1,kind2,..." — payload
		// content lives in the slog step_dispatch event already; here
		// we just need a stable structural marker.
		for i, ref := range r.Args {
			if i > 0 {
				b.WriteByte(',')
			}
			if ref == nil {
				b.WriteString("<nil>")
				continue
			}
			b.WriteString(ref.Kind_)
			b.WriteByte('(')
			writeSortedKwargs(&b, ref.Kwargs)
			b.WriteByte(')')
		}
	case "activity_completed":
		b.WriteByte('|')
		// Render result count + first nonretryable msg if any. Full
		// payload comparison happens via the slog event stream; this
		// is a structural marker.
		fmt.Fprintf(&b, "n=%d", len(r.Results))
		if r.Err != "" {
			fmt.Fprintf(&b, "|err=%s", r.Err)
		}
	}
	return b.String()
}

// writeSortedKVs writes sorted "key=value" pairs from a flat keyvals
// slice. Adjacent (k, v) entries are paired by index; trailing odd
// element (if any) is dropped.
func writeSortedKVs(b *strings.Builder, kv []any) {
	pairs := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		ks, ok := kv[i].(string)
		if !ok {
			continue
		}
		pairs[ks] = fmt.Sprintf("%v", kv[i+1])
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(pairs[k])
	}
}

// writeSortedKwargs renders an ActionRef.Kwargs *Dict deterministically.
// Starlark *Dict.String() emits a stable "{k1: v1, k2: v2}" form in
// insertion order (per the language contract documented in
// Plan 04.1-05a). For replay-determinism diffing we only need
// structural equality, so the dict's own String() representation is
// sufficient — no manual key extraction.
func writeSortedKwargs(b *strings.Builder, d any) {
	if d == nil {
		return
	}
	fmt.Fprintf(b, "%v", d)
}

// Compile-time check that EventCapture satisfies log.Logger.
var _ log.Logger = (*EventCapture)(nil)

// RunOnceCapturing executes parsed against a fresh
// TestWorkflowEnvironment, capturing slog events + activity-boundary
// events. Pass mockCallback == nil for activity-free flows
// (if_cond/script-only) — the helper skips OnActivity wiring entirely
// and any ExecuteActivity call would panic, surfacing the misuse.
//
// Plan 03 contract:
//   - parsed: required (panics on nil — programming error in caller)
//   - hash: registered as the content hash; tests typically pass a
//     fixed sentinel like "test-hash" or sha256 of source bytes
//   - init: workflow input state map
//   - mockCallback: when non-nil, registered as the "ExecuteBatch"
//     activity body via env.OnActivity. The signature MUST match
//     pkg/activity.ExecuteBatch verbatim (Pitfall 1).
//
// Plan 03 Task 1 lift target: replaces
// pkg/interpreter/replay_determinism_test.go::runOnceCapturing.
func RunOnceCapturing(
	parsed *ParsedFlow,
	hash string,
	init map[string]any,
	mockCallback func(context.Context, []*dag.ActionRef) ([]dag.ActionResult, error),
) (*EventCapture, map[string]any, error) {
	if parsed == nil {
		return nil, nil, errors.New("RunOnceCapturing: parsed must not be nil")
	}
	if parsed.Flow == nil {
		return nil, nil, errors.New("RunOnceCapturing: parsed.Flow must not be nil")
	}

	cap := NewEventCapture()
	registry := NewRegistry()
	if err := registry.Register(parsed.Flow.Name, hash, parsed); err != nil {
		return nil, nil, fmt.Errorf("RunOnceCapturing: registry.Register: %w", err)
	}
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(cap)
	env := ts.NewTestWorkflowEnvironment()
	env.SetOnActivityStartedListener(cap.onActivityStarted)
	env.SetOnActivityCompletedListener(cap.onActivityCompleted)

	if mockCallback != nil {
		fake := func(_ context.Context, _ []*dag.ActionRef) ([]dag.ActionResult, error) {
			return nil, nil
		}
		env.RegisterActivityWithOptions(fake, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})
		// Pitfall 1: ExecuteBatch has 2 args; mock.Anything passed
		// EXACTLY twice. testify/mock dispatches reflectively and
		// any mismatch panics deep inside the SDK.
		env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(mockCallback)
	}

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName:    parsed.Flow.Name,
		ContentHash: hash,
		InitState:   init,
	})
	if !env.IsWorkflowCompleted() {
		return cap, nil, errors.New("RunOnceCapturing: workflow did not complete")
	}
	wfErr := env.GetWorkflowError()
	var out map[string]any
	if wfErr == nil {
		_ = env.GetWorkflowResult(&out)
	}
	return cap, out, wfErr
}
