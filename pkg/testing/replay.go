package testing

// Plan 03 Task 2 — pkg/testing.RunOnceCapturing thin wrapper that
// threads Plan 02's mock router callback into
// interpreter.RunOnceCapturing. Plan 04's tester.run driver and the
// Phase 5 integration test (TestAttempts_IncrementOnRetry) compose
// against this wrapper rather than calling the interpreter helper
// directly.

import (
	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// RunOnceCapturing wraps interpreter.RunOnceCapturing with the mock
// router callback (built from buildExecuteBatchCallback). Plan 04's
// tester.run driver and Plan 03's integration test consume this.
//
// Parameters:
//   - parsed: required *interpreter.ParsedFlow
//   - hash: content hash registered alongside the flow
//   - init: workflow input state map
//   - reg: required *MockRegistry (the router calls reg.Match for
//     every action in every batch)
//   - attempts: optional *AttemptCounter; nil → allocate fresh
//   - lookup: optional StepIndexLookup for D5-D3 attribution; nil →
//     router falls back to (FlowName, -1, idx) keys
//
// Returns:
//   - *interpreter.EventCapture (slog + activity_started/completed
//     records; same shape Plan 04 will diff against)
//   - final workflow state (nil on workflow error)
//   - workflow error (non-nil if the workflow failed)
//
// D5-D1 mandate: callers wishing to enforce replay determinism call
// this helper TWICE with identical inputs and pass the two captures
// to FirstDivergentEvent. Plan 04 will hide that second call behind
// tester.run.
func RunOnceCapturing(
	parsed *interpreter.ParsedFlow,
	hash string,
	init map[string]any,
	reg *MockRegistry,
	attempts *AttemptCounter,
	lookup StepIndexLookup,
) (*interpreter.EventCapture, map[string]any, error) {
	return RunOnceCapturingWithSiblings(parsed, hash, init, reg, attempts, lookup, nil)
}

// RunOnceCapturingWithSiblings is the multi-flow variant of
// RunOnceCapturing. The Tier-3 driver (tester.run, pkg/testing/builtin_run.go)
// uses this so a *_test.star file declaring multiple flow(...) blocks
// can exercise call_flow targets — the harness registers every parsed
// flow against the test workflow's FlowRegistry so child-flow lookups
// resolve at execution time.
//
// `siblings` is keyed by flow name and pairs each parsed flow with its
// content hash. The entry flow is registered exactly once even if it
// also appears in `siblings`; the underlying interpreter helper skips
// duplicate registrations defensively.
func RunOnceCapturingWithSiblings(
	parsed *interpreter.ParsedFlow,
	hash string,
	init map[string]any,
	reg *MockRegistry,
	attempts *AttemptCounter,
	lookup StepIndexLookup,
	siblings map[string]interpreter.SiblingFlow,
) (*interpreter.EventCapture, map[string]any, error) {
	if attempts == nil {
		attempts = NewAttemptCounter()
	}
	flowName := ""
	if parsed != nil && parsed.Flow != nil {
		flowName = parsed.Flow.Name
	}
	cb := buildExecuteBatchCallback(flowName, reg, attempts, lookup)
	return interpreter.RunOnceCapturingWithSiblings(parsed, hash, init, cb, siblings)
}
