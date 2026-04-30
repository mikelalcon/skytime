package interpreter

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// ErrRegistryFrozen is returned by Register after Freeze has been called.
// Indicates a worker-boot bug: registration must complete before Freeze.
var ErrRegistryFrozen = errors.New("interpreter: flow registry is frozen")

// ErrDuplicateFlow is returned by Register when the same (name, hash) pair
// is registered twice. Single (name, hash) pairs are unique by construction
// (sha256 of file bytes); a duplicate signals a boot-loop bug.
var ErrDuplicateFlow = errors.New("interpreter: duplicate (flow_name, content_hash)")

// ParsedFlow is the in-memory artifact of one .star file's flow declaration.
// The worker's boot step (pkg/worker plan 03-04) builds these and registers
// them. The interpreter never constructs ParsedFlow — only consumes them.
//
// Lambdas is the D-18 ID → CapturedLambda map; bridge.CallLambda receives
// CapturedLambda by ID lookup at every IfCond / Script / for_each_parallel
// lambda evaluation site (plan 03-03 walkers).
type ParsedFlow struct {
	Flow    *dag.Flow
	Lambdas map[string]*dag.CapturedLambda
}

// FlowRegistry maps (flow_name, content_hash) to ParsedFlow.
//
// Multi-version shape (D3-07 + RESEARCH discretion): the inner map allows
// multiple content_hashes per flow_name. In practice during a v1 deployment
// a worker hosts exactly one version of each flow (Build IDs handle drain).
// Keeping the multi-version shape costs nothing and supports test fixtures
// that register the same flow twice with different bytes.
//
// Concurrency: Register is called only from worker boot (single-goroutine).
// Lookup is called from any number of workflow goroutines after Freeze.
// An RWMutex protects both — Lookup uses RLock so concurrent reads do not
// block one another. Post-Freeze the byFlow map is read-only by contract,
// so the lock is for boot-time correctness only.
type FlowRegistry struct {
	mu     sync.RWMutex
	frozen bool
	// byFlow maps flow_name → content_hash → ParsedFlow. Outer + inner
	// both populated at boot, never mutated after Freeze.
	byFlow map[string]map[string]*ParsedFlow
}

// NewRegistry returns an empty, unfrozen FlowRegistry. The worker's boot
// step (plan 03-04) fills it via Register() then calls Freeze() before
// starting the SDK worker.
func NewRegistry() *FlowRegistry {
	return &FlowRegistry{byFlow: map[string]map[string]*ParsedFlow{}}
}

// Register adds a parsed flow to the registry. Returns ErrRegistryFrozen
// post-Freeze; ErrDuplicateFlow on (name, hash) collision. Safe for
// concurrent calls during boot.
func (r *FlowRegistry) Register(flowName, contentHash string, parsed *ParsedFlow) error {
	if flowName == "" || contentHash == "" || parsed == nil {
		return fmt.Errorf("Register: flowName, contentHash, parsed all required (got name=%q hash=%q parsed=%v)",
			flowName, contentHash, parsed)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return ErrRegistryFrozen
	}
	inner, ok := r.byFlow[flowName]
	if !ok {
		inner = map[string]*ParsedFlow{}
		r.byFlow[flowName] = inner
	}
	if _, dup := inner[contentHash]; dup {
		return fmt.Errorf("%w: %s@%s", ErrDuplicateFlow, flowName, contentHash)
	}
	inner[contentHash] = parsed
	return nil
}

// Freeze marks the registry as immutable. Subsequent Register calls return
// ErrRegistryFrozen. Idempotent — calling Freeze on an already-frozen
// registry is a no-op.
func (r *FlowRegistry) Freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
}

// Lookup returns the ParsedFlow for (flowName, contentHash). Returns
// (nil, false) on miss. Safe for concurrent calls — read-only post-Freeze
// means no lock contention on the hot path; the RLock is for boot-time
// safety only.
func (r *FlowRegistry) Lookup(flowName, contentHash string) (*ParsedFlow, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inner, ok := r.byFlow[flowName]
	if !ok {
		return nil, false
	}
	parsed, ok := inner[contentHash]
	return parsed, ok
}

// ContentHashFor returns the unique content_hash for a flow_name when
// exactly one version is registered. Used by the call_flow walker (plan
// 03-03) to construct the child WorkflowInput. Returns ("", false) if
// zero or multiple versions are registered for that name.
//
// Multi-version case rationale: if a worker happens to host two versions
// of "greet" (test fixture, mid-drain corner case), call_flow can't pick
// deterministically. Returning ("", false) forces a clean error path in
// the walker.
//
// Determinism: when there is exactly one inner-map entry, sortedHashKeys
// is overkill but kept defensively — call_flow walks this from inside
// workflow code and workflowcheck would flag any unsorted iteration.
func (r *FlowRegistry) ContentHashFor(flowName string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inner, ok := r.byFlow[flowName]
	if !ok || len(inner) != 1 {
		return "", false
	}
	for _, hash := range sortedHashKeys(inner) {
		return hash, true
	}
	return "", false
}

// sortedHashKeys returns inner-map keys in sorted order. Defense-in-depth
// helper for workflowcheck: ContentHashFor is called from call_flow walker
// (plan 03-03) which IS workflow code; unsorted map iteration would be
// flagged.
func sortedHashKeys(m map[string]*ParsedFlow) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
