package parser

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// stateSchema is the lexically-visible state-name → TypeInfo map passed
// down the body walk. Visibility-only checks (D4-02) use has() and clone();
// type-aware passes (D4.2 result-branch validator, plan 03) use get().
//
// The struct{}→TypeInfo widening (D4.2-13) preserves every API method
// the D4-02 walker uses by name (has, add, clone, sortedKeys) so the
// existing call sites in walkBodyForCtxValidation continue to compile
// without semantic change. Untyped sources (script outputs before
// inference, item-vars) store TypeOpaque{} via addUntyped(); flow inputs
// map their type-hint string to the appropriate TypeInfo via typeFromHint.
type stateSchema map[string]TypeInfo

// newStateSchema constructs an empty stateSchema.
func newStateSchema() stateSchema { return stateSchema{} }

// add inserts name with the given TypeInfo. Idempotent — re-adding an
// existing name overwrites the previous TypeInfo.
func (s stateSchema) add(name string, t TypeInfo) { s[name] = t }

// addUntyped inserts name with TypeOpaque{} — the "cannot statically
// infer" sentinel. Used for script.OutputAlias before D4.2-13 inference
// lands and for for_each item-vars (whose element type is opaque in v1).
func (s stateSchema) addUntyped(name string) { s[name] = TypeOpaque{} }

// has reports whether name is present in s.
func (s stateSchema) has(name string) bool { _, ok := s[name]; return ok }

// get returns the TypeInfo stored under name, or (nil, false) when
// name is not in scope. Used by plan 03's inferType when resolving
// `ctx.<name>` references.
func (s stateSchema) get(name string) (TypeInfo, bool) {
	t, ok := s[name]
	return t, ok
}

// clone returns a shallow copy of s. D4-02 if_cond branches each receive a
// clone so any future "branch-local" additions do not leak into the parent
// or sibling branch.
func (s stateSchema) clone() stateSchema {
	out := make(stateSchema, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// sortedKeys returns the names in s sorted alphabetically — used for
// deterministic error messages ("visible: [a b c]").
func (s stateSchema) sortedKeys() []string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// typeFromHint maps a flow.Inputs type-hint string to a TypeInfo
// (D4.2-13). Flow declares `inputs={"name": "type"}`; this is the seed
// of the schema. Unknown hints (or empty) collapse to TypeOpaque{} so
// the existing visibility-only D4-02 walker keeps working unchanged
// while the branch-equality validator (plan 03) defers strict checks
// against opaque inputs.
func typeFromHint(hint string) TypeInfo {
	switch hint {
	case "int", "integer":
		return TypeScalar{Kind: "int"}
	case "float", "number":
		return TypeScalar{Kind: "float"}
	case "bool", "boolean":
		return TypeScalar{Kind: "bool"}
	case "string", "str":
		return TypeScalar{Kind: "string"}
	case "list", "array":
		return TypeList{Element: TypeOpaque{}}
	case "dict", "object", "map":
		return TypeDict{Fields: nil}
	}
	return TypeOpaque{}
}

// seedStateSchemaForFlow constructs the initial stateSchema for the given
// flow: every flow.Inputs entry seeded with typeFromHint. Used by every
// finalize-pass walker that needs branch-local typed state — D4-02
// validateLambdaCtxAccesses (visibility-only against the typed schema)
// AND D4.2-09 validateIfCondExpressionShape (branch-equality with full
// type info). Single helper keeps the seed shape consistent across passes.
func (p *Parser) seedStateSchemaForFlow(flow *dag.Flow) stateSchema {
	s := newStateSchema()
	for k, v := range flow.Inputs {
		s.add(k, typeFromHint(v))
	}
	return s
}

// validateLambdaCtxAccesses walks every flow's body sequentially. For each
// captured lambda it computes the state set visible at the lambda's source
// position and rejects any ctx.<name> reference that is not in the set.
//
// D4-02 stacking rules (canonical reference: 04-CONTEXT.md):
//   - At flow entry: state = keys of flow.Inputs (typed via typeFromHint)
//   - Script: validate lambda BEFORE adding OutputAlias; then state +=
//     OutputAlias so subsequent siblings see it (untyped for now;
//     D4.2-13 plan 02 will type via inferType on the script body)
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
		initial := p.seedStateSchemaForFlow(flow)
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
func (p *Parser) walkBodyForCtxValidation(flow *dag.Flow, body []dag.Node, state stateSchema) error {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.Script:
			// Validate this script's lambda BEFORE adding its OutputAlias.
			if err := p.checkLambdaCtx(flow, n.LambdaID, state); err != nil {
				return err
			}
			if n.OutputAlias != "" {
				// D4.2-13: untyped for now (plan 02 will type via
				// inferType on the script's fn body). Visibility-only
				// for D4-02 is preserved.
				state.addUntyped(n.OutputAlias)
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
			// Inside Steps, state += ItemVar (untyped — element-of-items
			// is opaque in v1).
			inner := state.clone()
			if n.ItemVar != "" {
				inner.addUntyped(n.ItemVar)
			}
			if err := p.walkBodyForCtxValidation(flow, n.Steps, inner); err != nil {
				return err
			}
		case *dag.Step:
			// D4.1-09: action_fn / block_fn lambdas (D4.1-06) flow through
			// the same D4-02 ctx.<name> walker as if_cond/script lambdas
			// so a typo in `lambda ctx: gh.get(path=ctx.tyop)` surfaces as
			// *dag.ValidationError at parse time. Step.NameFn (D4.1-15) is
			// a synthesized lambda from interpolation; the walker honors
			// its BodyPos so the desugared body is the source of truth.
			// Step.Actions kwargs may also carry interpolation lambdas
			// (D4.1-05) — validate each.
			if n.ActionFn != nil {
				if err := p.checkLambdaCtx(flow, n.ActionFn.ID, state); err != nil {
					return err
				}
			}
			if n.BlockFn != nil {
				if err := p.checkLambdaCtx(flow, n.BlockFn.ID, state); err != nil {
					return err
				}
			}
			if n.NameFn != nil {
				if err := p.checkLambdaCtx(flow, n.NameFn.ID, state); err != nil {
					return err
				}
			}
			for _, ar := range n.Actions {
				if ar == nil || ar.Kwargs == nil {
					continue
				}
				for _, item := range ar.Kwargs.Items() {
					captured, isLambda := dag.UnwrapStarlarkLambda(item[1])
					if !isLambda {
						continue
					}
					if err := p.checkLambdaCtx(flow, captured.ID, state); err != nil {
						return err
					}
				}
			}
		case *dag.CallFlow:
			// CallFlow inputs forbid lambdas (D-19). Nothing to validate.
			_ = n
		case *dag.Result:
			// D4.2-02: per-key value lambdas were synthesized from
			// user-source value expressions. Each one references
			// ctx.<name> via the same shape as if_cond/script lambdas.
			// Walk in Keys order (replay-deterministic).
			for _, k := range n.Keys {
				captured := n.Values[k]
				if captured == nil {
					continue
				}
				if err := p.checkLambdaCtx(flow, captured.ID, state); err != nil {
					return err
				}
			}
		case *dag.Fail:
			// D4.2-05/06: top-level fail("...${ctx.x}...") may carry a
			// MessageFn synthesized from interpolation. The walker
			// honors its BodyPos so the desugared body is the source
			// of truth (the synthetic <interp:...> file).
			if n.MessageFn != nil {
				if err := p.checkLambdaCtx(flow, n.MessageFn.ID, state); err != nil {
					return err
				}
			}
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
func (p *Parser) checkLambdaCtx(flow *dag.Flow, lambdaID string, state stateSchema) error {
	if lambdaID == "" {
		return nil
	}
	captured, ok := p.lambdas[lambdaID]
	if !ok {
		return nil
	}
	// D4.1-01 BodyPos preference: synthesized lambdas (e.g., from
	// interpolation desugaring) carry the lambda body in a synthetic
	// file (`<interp:...>`), not the user's source. The walker re-parses
	// whichever file actually contains the AST. Hand-written lambdas
	// leave BodyPos zero and the walker uses Pos as before.
	walkPos := captured.Pos
	if captured.BodyPos.IsValid() {
		walkPos = captured.BodyPos
	}
	src, ok := p.fileBytes[walkPos.Filename()]
	if !ok || src == nil {
		return nil
	}
	accesses, err := findCtxAccesses(src, walkPos.Filename(), walkPos)
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
			// D4.1-01 RESEARCH §Pitfall 1 + D4.2-02 Pitfall 3: when the
			// access lives in a synthetic interpolation OR result-value
			// file, remap the error position back to the user's source
			// (captured.Pos — opening ${ for interp, value-expr start
			// for result) so users never see "<interp:..>" or
			// "<result:..>" leaked into ValidationError output.
			errPos := acc.Pos
			if strings.HasPrefix(errPos.Filename(), "<interp:") ||
				strings.HasPrefix(errPos.Filename(), "<result:") {
				errPos = captured.Pos
			}
			return &dag.ValidationError{
				Pos:  errPos,
				Flow: flow.Name,
				Msg:  fmt.Sprintf("ctx.%s not in declared state (visible: %v)", acc.AttrName, state.sortedKeys()),
			}
		}
	}
	return nil
}
