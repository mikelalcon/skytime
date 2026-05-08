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

// FlowNames returns a fresh sorted slice of all registered flow names.
// Used by pkg/cli/server.go's startup banner (Phase 7) so the rendered
// list is deterministic across runs. Returns an empty (non-nil) slice
// when no flows are registered.
func (r *FlowRegistry) FlowNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byFlow))
	for name := range r.byFlow {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
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

// =============================================================================
// TriggerRegistry — Phase 7 Plan 04 (TRIG-05, D-07-11)
// =============================================================================

// ErrTriggerRegistryFrozen is returned by TriggerRegistry.Register after
// Freeze has been called. Indicates a worker-boot bug: registration must
// complete before Freeze.
var ErrTriggerRegistryFrozen = errors.New("interpreter: trigger registry is frozen")

// TriggerRegistry stores all *dag.Trigger values registered at boot.
//
// Why this shape diverges from FlowRegistry (§ Pitfall 1): Flows are
// looked up per-workflow-start by (flow_name, content_hash) — that's
// FlowRegistry's primary access pattern. Triggers have a different
// lifecycle: registered once at boot, iterated wholesale once when the
// HTTP router mounts handlers (Phase 7.1) or when the cron scheduler
// reconciles schedules (Phase 7.2). NEVER looked up per-request.
//
// Therefore the primary access shape is a sorted slice plus a per-file
// content_hash secondary index for future hot-reload diagnostics.
//
// Determinism: Freeze() sorts the internal slice by (Source.Kind,
// FlowName, Pos) so All() returns the same order across runs. Plan 05's
// startup banner depends on this sorted order.
//
// Concurrency: same RWMutex + frozen-after-boot model as FlowRegistry —
// Register from worker boot (single goroutine), All from any number of
// readers (HTTP router, cron, dashboard) post-Freeze.
type TriggerRegistry struct {
	mu       sync.RWMutex
	frozen   bool
	triggers []*dag.Trigger
	// byContentHash groups triggers by the content_hash of their owning
	// file. Phase 7 sets but doesn't read this index; future phases (hot
	// reload, per-file diagnostics) consume it.
	byContentHash map[string][]*dag.Trigger
}

// NewTriggerRegistry returns an empty, unfrozen TriggerRegistry. The
// worker's boot step (pkg/worker/boot.go) fills it via Register() then
// calls Freeze() before NewWorker returns.
func NewTriggerRegistry() *TriggerRegistry {
	return &TriggerRegistry{byContentHash: map[string][]*dag.Trigger{}}
}

// Register adds a trigger to the registry, indexed by the content_hash
// of its owning file. Returns ErrTriggerRegistryFrozen post-Freeze. Safe
// for concurrent calls during boot — but boot is single-goroutine in
// practice, so contention is theoretical.
//
// Unlike FlowRegistry.Register, there is NO duplicate detection at the
// (flow, hash) layer — D-07-13 explicitly allows multiple triggers per
// (flow, source-kind) pair. The parser's warnDuplicateTriggers pass
// (Plan 03) emits a warning for byte-identical duplicates; the registry
// stores both.
func (r *TriggerRegistry) Register(contentHash string, t *dag.Trigger) error {
	if t == nil {
		return errors.New("TriggerRegistry.Register: trigger required")
	}
	if contentHash == "" {
		return errors.New("TriggerRegistry.Register: contentHash required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return ErrTriggerRegistryFrozen
	}
	r.triggers = append(r.triggers, t)
	r.byContentHash[contentHash] = append(r.byContentHash[contentHash], t)
	return nil
}

// Freeze marks the registry as immutable AND sorts the internal slice by
// (Source.Kind, FlowName, Pos) for deterministic All() output.
// Idempotent: calling Freeze on an already-frozen registry is a no-op.
func (r *TriggerRegistry) Freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return
	}
	sort.SliceStable(r.triggers, func(i, j int) bool {
		a, b := r.triggers[i], r.triggers[j]
		if a.Source == nil || b.Source == nil {
			// Defensive: nil sources sort last (shouldn't happen post-parse).
			return a.Source != nil
		}
		if a.Source.Kind() != b.Source.Kind() {
			return a.Source.Kind() < b.Source.Kind()
		}
		if a.FlowName != b.FlowName {
			return a.FlowName < b.FlowName
		}
		// Tiebreaker: file:line:col — Pos.String() formats stably.
		return a.Pos.String() < b.Pos.String()
	})
	r.frozen = true
}

// All returns a fresh slice of triggers in sorted order (by Source.Kind,
// then FlowName, then Pos). Plan 05's startup banner reads this. Phase
// 7.1's HTTP router groups by Source.Kind() for handler mounting. Phase
// 7.2's cron reconciler filters by Source type-switch.
//
// Returns an empty (non-nil) slice when no triggers are registered.
// Safe to call before Freeze (returns the slice in registration order
// in that case — only post-Freeze guarantees sorted order).
func (r *TriggerRegistry) All() []*dag.Trigger {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*dag.Trigger, len(r.triggers))
	copy(out, r.triggers)
	return out
}

// ByContentHash returns the triggers declared in the file with the given
// content_hash. Returns nil if no triggers were registered for that hash.
// Used by future hot-reload diagnostics (Phase 7+); Phase 7 sets but
// doesn't consume.
func (r *TriggerRegistry) ByContentHash(hash string) []*dag.Trigger {
	r.mu.RLock()
	defer r.mu.RUnlock()
	triggers, ok := r.byContentHash[hash]
	if !ok {
		return nil
	}
	out := make([]*dag.Trigger, len(triggers))
	copy(out, triggers)
	return out
}
