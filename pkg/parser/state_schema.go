package parser

import (
	"fmt"
	"sort"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// stateSet is the lexically-visible state-name set passed down the body
// walk for D4-02 ctx.<name> validation. Map-of-empty-struct for O(1)
// membership tests; clone before forking branches so add() in one branch
// does not leak into siblings.
type stateSet map[string]struct{}

// newStateSet constructs an empty stateSet.
func newStateSet() stateSet { return stateSet{} }

// add inserts name into s. Idempotent — re-adding an existing name is a
// no-op.
func (s stateSet) add(name string) { s[name] = struct{}{} }

// has reports whether name is present in s.
func (s stateSet) has(name string) bool { _, ok := s[name]; return ok }

// clone returns a shallow copy of s. D4-02 if_cond branches each receive a
// clone so any future "branch-local" additions do not leak into the parent
// or sibling branch.
func (s stateSet) clone() stateSet {
	out := make(stateSet, len(s))
	for k := range s {
		out[k] = struct{}{}
	}
	return out
}

// sortedKeys returns the names in s sorted alphabetically — used for
// deterministic error messages ("visible: [a b c]").
func (s stateSet) sortedKeys() []string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validateLambdaCtxAccesses walks every flow's body sequentially. For each
// captured lambda it computes the state set visible at the lambda's source
// position and rejects any ctx.<name> reference that is not in the set.
//
// D4-02 stacking rules (canonical reference: 04-CONTEXT.md):
//   - At flow entry: state = keys of flow.Inputs
//   - Script: validate lambda BEFORE adding OutputAlias; then state +=
//     OutputAlias so subsequent siblings see it
//   - IfCond: validate cond lambda; walk Then and Else each with a CLONE
//     of pre-branch state — outputs added inside `then` are NOT visible in
//     `else_` and vice versa
//   - ForEachParallel: validate items lambda (if ItemsLambdaID set)
//     against pre-loop state; then walk Steps with a clone += ItemVar
//   - Step: no lambda evaluation in this pass (Phase 4 does not check
//     kwarg expressions; DSL-08 retry/timeout values are pure data)
//   - CallFlow: no lambda evaluation (D-19 forbids lambdas in CallFlow
//     inputs)
//
// Sits in the finalize chain BEFORE validateActionRefKwargs so structural
// state errors surface before kwarg-shape errors (per CONTEXT D4-01).
func (p *Parser) validateLambdaCtxAccesses() error {
	for _, flow := range p.flows {
		initial := newStateSet()
		for k := range flow.Inputs {
			initial.add(k)
		}
		if err := p.walkBodyForCtxValidation(flow, flow.Body, initial); err != nil {
			return err
		}
	}
	return nil
}

// walkBodyForCtxValidation is the recursive helper for
// validateLambdaCtxAccesses. Mirrors walkLintMixedIdempotency /
// walkLintBlockSize / walkResolveCallFlows for grep-friendly recursion
// uniformity.
func (p *Parser) walkBodyForCtxValidation(flow *dag.Flow, body []dag.Node, state stateSet) error {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.Script:
			// Validate this script's lambda BEFORE adding its OutputAlias.
			if err := p.checkLambdaCtx(flow, n.LambdaID, state); err != nil {
				return err
			}
			if n.OutputAlias != "" {
				state.add(n.OutputAlias)
			}
		case *dag.IfCond:
			// Validate the cond lambda first.
			if err := p.checkLambdaCtx(flow, n.LambdaID, state); err != nil {
				return err
			}
			// then/else fork — each branch sees pre-branch state. Clone
			// so a future branch-local add() does not leak.
			if err := p.walkBodyForCtxValidation(flow, n.Then, state.clone()); err != nil {
				return err
			}
			if err := p.walkBodyForCtxValidation(flow, n.Else, state.clone()); err != nil {
				return err
			}
		case *dag.ForEachParallel:
			// The items producer (when items=lambda) sees only PRE-loop
			// state — it cannot reference its own item-var.
			if n.ItemsLambdaID != "" {
				if err := p.checkLambdaCtx(flow, n.ItemsLambdaID, state); err != nil {
					return err
				}
			}
			// Inside Steps, state += ItemVar.
			inner := state.clone()
			if n.ItemVar != "" {
				inner.add(n.ItemVar)
			}
			if err := p.walkBodyForCtxValidation(flow, n.Steps, inner); err != nil {
				return err
			}
		case *dag.Step, *dag.CallFlow:
			// No lambda evaluation: Step kwargs are pure data; CallFlow
			// inputs forbid lambdas (D-19). Nothing to validate here.
			_ = n
		}
	}
	return nil
}

// checkLambdaCtx finds the lambda by ID, reads its source bytes from the
// parser's fileBytes cache, and emits a *dag.ValidationError on the first
// ctx.<name> access not in state.
//
// Empty lambdaID is silently skipped — defensive: well-formed nodes always
// carry an ID after lambda capture, but if a future code path or test
// builds a partial node directly we degrade gracefully rather than
// reject. The earlier finalize pass (resolveCallFlows + lintMixedIdempotency
// + lintBlockSize) would have surfaced gross structural problems first.
//
// Missing lambdas in p.lambdas or missing fileBytes entries are also
// silently skipped — both indicate state that lambda-capture would have
// errored on; we don't want a defensive nil-dereference here.
func (p *Parser) checkLambdaCtx(flow *dag.Flow, lambdaID string, state stateSet) error {
	if lambdaID == "" {
		return nil
	}
	captured, ok := p.lambdas[lambdaID]
	if !ok {
		return nil
	}
	src, ok := p.fileBytes[captured.Pos.Filename()]
	if !ok || src == nil {
		return nil
	}
	accesses, err := findCtxAccesses(src, captured.Pos.Filename(), captured.Pos)
	if err != nil {
		return &dag.ValidationError{
			Pos:     captured.Pos,
			Flow:    flow.Name,
			Msg:     fmt.Sprintf("re-parse for ctx.<name> validation failed: %v", err),
			Wrapped: err,
		}
	}
	for _, acc := range accesses {
		if !state.has(acc.AttrName) {
			return &dag.ValidationError{
				Pos:  acc.Pos,
				Flow: flow.Name,
				Msg:  fmt.Sprintf("ctx.%s not in declared state (visible: %v)", acc.AttrName, state.sortedKeys()),
			}
		}
	}
	return nil
}
