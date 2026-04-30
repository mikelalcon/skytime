package parser

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// validateFreeVars enforces D-19: a lambda may close over free variables
// only when they bind to module-level (top-of-file scope) names in the
// owning file. Module-level def helpers and constants are allowed; locals
// inside an enclosing def body are not.
//
// Returns a frozen StringDict snapshot of the captured free vars on success;
// returns *dag.ParseError pointing at the offending binding on failure.
//
// Note (Pitfall #5): a `def helper(x): ...` declaration at column 1 IS a
// module-level binding even though the function's body is indented; we
// inspect the BINDING position (where the name is bound), not the function's
// body or call sites. starlark.Function.FreeVar returns a starlark.Binding
// whose Pos.Col == 1 for module-level bindings.
func (p *Parser) validateFreeVars(fn *starlark.Function, ownerFile string) (starlark.StringDict, error) {
	out := starlark.StringDict{}
	for i := 0; i < fn.NumFreeVars(); i++ {
		binding, value := fn.FreeVar(i)
		if !isModuleLevelBinding(ownerFile, binding) {
			return nil, &dag.ParseError{
				Pos: binding.Pos,
				Msg: fmt.Sprintf("lambda captures non-module-level variable %q (free vars must reference module-level constants/functions only)", binding.Name),
			}
		}
		out[binding.Name] = value
	}
	out.Freeze()
	return out, nil
}

// isModuleLevelBinding returns true when the binding's source position is
// the top-of-file scope of the owning file. Top-of-file in starlark-go's
// resolver lands at column 1 — locals inside a def body have higher
// indentation (col >= 5 for `    var = ...`).
//
// Cross-file note: a binding loaded via load() points at the original
// definition in the load target. starlark-go's resolver currently exposes
// only the binding's Pos (not the load() callsite), so a load-imported
// helper's free-var attribution would point at the helper file, not the
// importing file. Phase 1's same-file rule keeps this simple — fixtures
// that exercise load() use the imported helper as a whole-builtin (e.g.
// `shared_step()`) rather than capturing it inside a lambda.
func isModuleLevelBinding(filename string, binding starlark.Binding) bool {
	return binding.Pos.Filename() == filename && binding.Pos.Col == 1
}

// =============================================================================
// Phase 2 lint passes (D2-05 mixed idempotency + D2-07 block size cap)
// =============================================================================

// lintMixedIdempotency walks every flow's body and asserts that each Step's
// Actions slice is homogeneous — either all idempotent OR exactly one
// non-idempotent action (D2-05 / D2-06).
//
// The activity (Phase 2 pkg/activity) defensively re-validates, but
// rejecting at parse time gives the consultant a clean position-aware error
// before any workflow runs. D2-06 keeps splitting in the Phase 3 interpreter,
// not the activity layer; this lint is the consultant-facing safety net.
//
// Recursion mirrors finalize.go's walkResolveCallFlows shape — same idiom
// so future readers see one walk pattern.
func (p *Parser) lintMixedIdempotency() error {
	for _, flow := range p.flows {
		if err := p.walkLintMixedIdempotency(flow.Name, flow.Body); err != nil {
			return err
		}
	}
	return nil
}

// walkLintMixedIdempotency is the recursive helper for lintMixedIdempotency.
// Descends into IfCond.Then/Else and ForEachParallel.Steps so a mixed block
// nested inside a conditional or loop body is also rejected.
func (p *Parser) walkLintMixedIdempotency(flowName string, body []dag.Node) error {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.Step:
			if len(n.Actions) <= 1 {
				continue // single-action steps are trivially homogeneous
			}
			var idem, nonIdem []string
			for _, ref := range n.Actions {
				extName, opName, ok := splitKind(ref.Kind_)
				if !ok {
					continue
				}
				ext, ok := p.registry.Get(extName)
				if !ok {
					continue
				}
				spec, ok := ext.Operations()[opName]
				if !ok || spec == nil || spec.Idempotent == nil {
					continue
				}
				if *spec.Idempotent {
					idem = append(idem, ref.Kind_)
				} else {
					nonIdem = append(nonIdem, ref.Kind_)
				}
			}
			if len(idem) > 0 && len(nonIdem) > 0 {
				return &dag.ValidationError{
					Pos:  n.Pos,
					Flow: flowName,
					Msg: fmt.Sprintf(
						"cannot mix idempotent and non-idempotent operations in a block.\n  - %s (idempotent)\n  - %s (NOT idempotent)\nSuggestion: split into separate steps.",
						idem[0], nonIdem[0]),
				}
			}
		case *dag.IfCond:
			if err := p.walkLintMixedIdempotency(flowName, n.Then); err != nil {
				return err
			}
			if err := p.walkLintMixedIdempotency(flowName, n.Else); err != nil {
				return err
			}
		case *dag.ForEachParallel:
			if err := p.walkLintMixedIdempotency(flowName, n.Steps); err != nil {
				return err
			}
		}
	}
	return nil
}

// lintBlockSize rejects step(block=[...]) with > p.maxBlockSize entries
// (D2-07). The activity (Phase 2) defensively re-enforces a runtime cap;
// this parse-time pass is the fast-fail UX surface.
func (p *Parser) lintBlockSize() error {
	for _, flow := range p.flows {
		if err := p.walkLintBlockSize(flow.Name, flow.Body); err != nil {
			return err
		}
	}
	return nil
}

// walkLintBlockSize is the recursive helper for lintBlockSize. Mirrors
// walkLintMixedIdempotency's recursion shape.
func (p *Parser) walkLintBlockSize(flowName string, body []dag.Node) error {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.Step:
			if len(n.Actions) > p.maxBlockSize {
				return &dag.ValidationError{
					Pos:  n.Pos,
					Flow: flowName,
					Msg: fmt.Sprintf(
						"block has %d actions; maximum is %d. Split into multiple steps.",
						len(n.Actions), p.maxBlockSize),
				}
			}
		case *dag.IfCond:
			if err := p.walkLintBlockSize(flowName, n.Then); err != nil {
				return err
			}
			if err := p.walkLintBlockSize(flowName, n.Else); err != nil {
				return err
			}
		case *dag.ForEachParallel:
			if err := p.walkLintBlockSize(flowName, n.Steps); err != nil {
				return err
			}
		}
	}
	return nil
}

// =============================================================================
// Phase 3 lint pass (D3-19 empty task_queue defense in depth)
// =============================================================================

// lintEmptyTaskQueue is a defense-in-depth pass for D3-19. The primary
// rejection of `flow(task_queue="")` and `step(task_queue="")` happens
// inside builtinFlow / builtinStep BEFORE the dag types are constructed —
// at that point we can distinguish "kwarg supplied as empty" (rejected)
// from "kwarg omitted" (default empty, allowed) via the kwargs presence
// detection.
//
// Post-construction, dag.Flow.TaskQueue and dag.Step.TaskQueue carry no
// presence flag — empty string is indistinguishable from "absent". This
// pass walks any directly-constructed *dag.Flow / *dag.Step (e.g. test
// harnesses) but is a NO-OP for the empty-string case: it would have to
// reject every absence-of-override, which is the dominant valid case.
//
// The pass is kept as a stub for two reasons:
//   1. Symmetry with lintMixedIdempotency / lintBlockSize — every Phase
//      2/3 invariant has a matching lint pass that downstream readers can
//      grep for.
//   2. If a future change adds a presence flag (e.g. a TaskQueueSet bool
//      or a *string pointer), this is the place to add the rejection
//      logic without touching the builtin code path.
//
// The function returns nil unconditionally for now. Tests pin this
// behavior so a future tightening (or loosening) is a deliberate decision.
func (p *Parser) lintEmptyTaskQueue() error {
	for _, flow := range p.flows {
		if err := p.walkLintEmptyTaskQueue(flow.Name, flow.Body); err != nil {
			return err
		}
	}
	return nil
}

// walkLintEmptyTaskQueue is the recursive helper for lintEmptyTaskQueue.
// Mirrors walkLintMixedIdempotency / walkLintBlockSize so the recursion
// idiom is uniform across lint passes.
func (p *Parser) walkLintEmptyTaskQueue(flowName string, body []dag.Node) error {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.Step:
			// No-op: see lintEmptyTaskQueue comment for the design rationale.
			_ = n
		case *dag.IfCond:
			if err := p.walkLintEmptyTaskQueue(flowName, n.Then); err != nil {
				return err
			}
			if err := p.walkLintEmptyTaskQueue(flowName, n.Else); err != nil {
				return err
			}
		case *dag.ForEachParallel:
			if err := p.walkLintEmptyTaskQueue(flowName, n.Steps); err != nil {
				return err
			}
		}
	}
	return nil
}

// splitKind splits "github.create_issue" into ("github", "create_issue", true).
// Returns ok=false if the kind doesn't have exactly one dot or has empty
// parts on either side. The kind format is set by extension factories
// (typically `<extName>.<opName>`) — splitKind is the inverse used by lint
// passes that need to look up the OperationSpec via the registry.
func splitKind(kind string) (string, string, bool) {
	i := strings.IndexByte(kind, '.')
	if i < 0 || i == 0 || i == len(kind)-1 {
		return "", "", false
	}
	return kind[:i], kind[i+1:], true
}
