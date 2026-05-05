package testing

import "sync"

// ActionKey identifies one mock dispatch slot for attempt counting.
//
// Replay-equality (D5-D1) requires that two consecutive runs produce
// identical (FlowName, StepIdx, ActionIdx) triplets — the router uses
// dag.ActionRef.Pos for diagnostic attribution but Pos is NOT part of
// the key (file-content-hash IDs change when cosmetic edits are made;
// the slot identity is structural). For Phase 5 we key on stable
// structural triples derived from the parsed flow.
//
// Plan 04's tester.run captures parsed once and builds a StepIndexLookup
// (see router.go) that maps each *dag.ActionRef to its (StepIdx,
// ActionIdx) within Flow.Steps.
type ActionKey struct {
	FlowName  string
	StepIdx   int
	ActionIdx int
}

// AttemptCounter is the per-(flow, step, action_idx) attempt
// dispenser. NextFor returns 1 on the first call, 2 on the second, etc.
//
// Defensive sync.Mutex: the mock callback runs in the activity
// goroutine and only ONE activity goroutine runs ExecuteBatch at a
// time per workflow, but the mutex protects against future
// cross-action concurrency without the call sites having to think
// about it. Tests under -race exercise the mutex directly.
type AttemptCounter struct {
	mu     sync.Mutex
	counts map[ActionKey]int
}

// NewAttemptCounter returns a fresh counter.
func NewAttemptCounter() *AttemptCounter {
	return &AttemptCounter{counts: map[ActionKey]int{}}
}

// NextFor returns the 1-indexed attempt count for the given key,
// incrementing the internal counter so the next call returns +1.
//
// The router computes ActionKey from (parsed.Flow.Name, stepIdx,
// actionIdx within the batch). stepIdx + actionIdx are derived in the
// callback by walking parsed.Flow.Steps; see router.go.
func (a *AttemptCounter) NextFor(key ActionKey) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.counts[key]++
	return a.counts[key]
}

// Snapshot returns a copy of the current count map for diagnostics.
// The returned map is owned by the caller; mutations do not affect
// future NextFor calls.
func (a *AttemptCounter) Snapshot() map[ActionKey]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[ActionKey]int, len(a.counts))
	for k, v := range a.counts {
		out[k] = v
	}
	return out
}
