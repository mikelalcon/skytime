package interpreter

import "sort"

// state is the workflow-local state map. Lambdas read it via
// bridge.ToStarlarkStruct (which itself sorts keys); writes happen via
// Script.OutputAlias updates (plan 03-03's walkScript) and
// ForEachParallel item scoping (plan 03-03's walkForEach).
//
// Why a wrapper around map[string]any: callers MUST always go through
// snapshot()/scoped()/setOutput() so that map iteration is centralized
// and sorted (D3-23). Direct map access bypasses the determinism
// discipline. workflowcheck flags `for k := range m` patterns; routing
// every read through snapshot() keeps the workflow code clean.
type state struct {
	vars map[string]any
}

// newState constructs a fresh state seeded from `initial`. The initial
// map is copied (sorted-key) so the caller's map is not aliased and
// downstream mutations don't leak across the package boundary. nil
// initial is treated as an empty map.
func newState(initial map[string]any) *state {
	v := make(map[string]any, len(initial))
	for _, k := range sortedKeys(initial) {
		v[k] = initial[k]
	}
	return &state{vars: v}
}

// snapshot returns a deep-enough copy for handing off to bridge.CallLambda.
// The bridge's ToStarlarkStruct itself sorts keys before iteration; this
// method exists for semantic clarity (every workflow-side state read goes
// through one place) and for the future case where snapshot needs to
// deep-copy nested maps.
//
// Determinism: map iteration here is via sortedKeys, so the returned map
// is populated in alphabetical-key order. Go map iteration order is not
// defined, but the act of iterating sorted keys ensures every observable
// downstream behavior (e.g., bridge.ToStarlarkStruct) sees the same
// values; the returned map identity itself doesn't carry order information.
func (s *state) snapshot() map[string]any {
	out := make(map[string]any, len(s.vars))
	for _, k := range sortedKeys(s.vars) {
		out[k] = s.vars[k]
	}
	return out
}

// setOutput is used by Script walkers (plan 03-03) to publish lambda
// output under an alias. Direct write — no copy — because state is
// always single-threaded inside the workflow goroutine.
func (s *state) setOutput(alias string, v any) {
	s.vars[alias] = v
}

// scoped returns a NEW state with `itemVar` set to `item` on top of the
// parent state. Used by ForEachParallel walker (plan 03-03) to inject
// per-branch items via ctx.<itemVar> dot-access. When itemVar is empty
// the parent state is returned unchanged (no shadowing semantics needed).
//
// scoped does NOT mutate the receiver; the parent state stays clean for
// the next branch. The returned state is independent.
func (s *state) scoped(itemVar string, item any) *state {
	if itemVar == "" {
		return s
	}
	out := make(map[string]any, len(s.vars)+1)
	for _, k := range sortedKeys(s.vars) {
		out[k] = s.vars[k]
	}
	out[itemVar] = item
	return &state{vars: out}
}

// sortedKeys is the workflow-safe map-iteration helper (D3-23).
// workflowcheck would flag `for k := range m`; routing through this
// function makes every iteration explicit and audit-friendly.
//
// Defensive: nil maps return an empty slice rather than nil so callers
// can range-loop without an extra nil check.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
