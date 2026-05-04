package parser

// validateIfCondExpressionShape implements D4.2-09 + D4.2-11. It enforces
// the five "expression-mode" rules that gate an if_cond whose
// OutputAlias is non-empty:
//
//  1. Both then AND else_ branches must be present (no single-arm
//     expression-mode if_cond).
//  2. The LAST node of each branch must be *dag.Result OR *dag.Fail.
//  3. At least one branch's last node must be *dag.Result; both-Fail is
//     procedural-guard territory and rejects with "remove output_alias".
//  4. Across branches that end in Result, the dict KEY SETS must match.
//     The error reports both branches' position and the symmetric
//     difference of keys.
//  5. Per-shared-key TypeInfo strict-equality (D4.2-11). Re-runs inferType
//     against the per-branch state schema (each branch sees pre-branch
//     state — script outputs, item-vars, flow inputs typed via
//     typeFromHint). One-side Opaque DEFERS (no error). Both-Opaque
//     DEFERS (no error). Two concrete-but-different types reject with
//     a cast hint mentioning float(x)/int(x)/str(x).
//
// Plus the orphan-Result detector: *dag.Result is illegal anywhere
// except the LAST position of an expression-mode branch (D4.2-04
// invariant — the placement gate landed in plan 04.2-02 covers the
// top-level case; this validator extends the invariant through
// procedural-mode if_cond branches and leading positions in
// expression-mode branches).
//
// Finalize-pass ordering rule (D4.2-09): runs AFTER
// validateLambdaCtxAccesses (so ctx-typo errors surface FIRST) and
// BEFORE validateActionRefKwargs (so structural state errors surface
// before kwarg-shape errors). The placement check
// (validateResultPlacement, plan 02) runs between them; this validator
// runs immediately after placement.

import (
	"fmt"
	"sort"

	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// validateIfCondExpressionShape walks every flow's body recursively. For
// each *dag.IfCond with OutputAlias != "" it enforces D4.2-09 + D4.2-11.
// The orphan-Result detector is folded into the same walk (case
// *dag.Result anywhere except a legal last-position).
func (p *Parser) validateIfCondExpressionShape() error {
	for _, flow := range p.flows {
		schema := p.seedStateSchemaForFlow(flow)
		if err := p.walkValidateIfCondExpression(flow, flow.Body, schema); err != nil {
			return err
		}
	}
	return nil
}

// walkValidateIfCondExpression walks a body. *dag.Result encountered at
// the top of a body (i.e., not via walkBranchSkippingLastResultOrFail's
// last-position skip) is an orphan and rejects. Expression-mode if_cond
// branches descend via walkBranchSkippingLastResultOrFail so the legal
// last-position Result is not flagged. Procedural-mode if_cond branches
// descend via this same walk — Result inside a procedural branch is
// still illegal.
//
// State schema accumulates as the walker proceeds: script outputs +=
// addUntyped (visibility-only — branch-equality re-infers per-branch);
// for_each item-vars += addUntyped inside the loop's clone.
func (p *Parser) walkValidateIfCondExpression(flow *dag.Flow, body []dag.Node, schema stateSchema) error {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.Result:
			// Orphan: reaching this case means the parent walk did NOT
			// strip the legal last-position case. D4.2-04 invariant.
			return &dag.ValidationError{
				Pos:  n.Pos,
				Flow: flow.Name,
				Msg:  "result(...) is only legal as the last node of an expression-mode if_cond branch (output_alias set); did you mean to wrap this in if_cond(output_alias=...)?",
			}
		case *dag.IfCond:
			if n.OutputAlias != "" {
				if err := p.checkIfCondExpression(flow, n, schema); err != nil {
					return err
				}
				// Descend into both branches with cloned schema. Use
				// walkBranchSkippingLastResultOrFail so the legal
				// last-position Result/Fail is not flagged as an orphan.
				if err := p.walkBranchSkippingLastResultOrFail(flow, n.Then, schema.clone()); err != nil {
					return err
				}
				if err := p.walkBranchSkippingLastResultOrFail(flow, n.Else, schema.clone()); err != nil {
					return err
				}
			} else {
				// Procedural-mode if_cond: descend normally. *dag.Result
				// inside a procedural branch is illegal — the
				// *dag.Result case above will catch it.
				if err := p.walkValidateIfCondExpression(flow, n.Then, schema.clone()); err != nil {
					return err
				}
				if err := p.walkValidateIfCondExpression(flow, n.Else, schema.clone()); err != nil {
					return err
				}
			}
		case *dag.ForEachParallel:
			inner := schema.clone()
			if n.ItemVar != "" {
				inner.addUntyped(n.ItemVar)
			}
			if err := p.walkValidateIfCondExpression(flow, n.Steps, inner); err != nil {
				return err
			}
		case *dag.Script:
			// Script output enters schema after the script runs. Mirrors
			// D4-02 walker semantics. Untyped — full inferType on
			// script bodies is out of v1 scope.
			if n.OutputAlias != "" {
				schema.addUntyped(n.OutputAlias)
			}
		}
	}
	return nil
}

// walkBranchSkippingLastResultOrFail recurses into an expression-mode
// branch but does NOT trigger the orphan-Result error if the LAST node
// is *dag.Result or *dag.Fail (those are legal terminators here).
// Leading nodes are walked via the normal walker so a Result in
// position 0..N-2 still rejects.
func (p *Parser) walkBranchSkippingLastResultOrFail(flow *dag.Flow, body []dag.Node, schema stateSchema) error {
	if len(body) == 0 {
		return nil
	}
	last := len(body) - 1
	// Walk leading nodes normally — orphan-Result/script-output-update
	// behavior intact.
	if err := p.walkValidateIfCondExpression(flow, body[:last], schema); err != nil {
		return err
	}
	// For the last node: *dag.Result or *dag.Fail are legal terminators
	// in expression mode; anything else descends recursively (a nested
	// if_cond at the tail, for example).
	switch body[last].(type) {
	case *dag.Result, *dag.Fail:
		return nil
	default:
		return p.walkValidateIfCondExpression(flow, body[last:], schema)
	}
}

// checkIfCondExpression enforces D4.2-09 cases 1-3 (structural rules)
// and delegates cases 4-5 to compareResultBranches.
func (p *Parser) checkIfCondExpression(flow *dag.Flow, n *dag.IfCond, schema stateSchema) error {
	// Case 1: both branches present (else_ MUST be supplied in expression mode).
	if len(n.Then) == 0 || len(n.Else) == 0 {
		return &dag.ValidationError{
			Pos:  n.Pos,
			Flow: flow.Name,
			Msg:  "if_cond expression mode (output_alias set): both then and else_ branches must be present",
		}
	}
	thenLast := n.Then[len(n.Then)-1]
	elseLast := n.Else[len(n.Else)-1]

	// Case 2: each last node is *dag.Result OR *dag.Fail.
	thenRes, thenIsRes := thenLast.(*dag.Result)
	_, thenIsFail := thenLast.(*dag.Fail)
	elseRes, elseIsRes := elseLast.(*dag.Result)
	_, elseIsFail := elseLast.(*dag.Fail)
	if !(thenIsRes || thenIsFail) {
		return &dag.ValidationError{
			Pos:  thenLast.Position(),
			Flow: flow.Name,
			Msg:  fmt.Sprintf("if_cond expression mode: then-branch last node must be result(...) or fail(...), got %s", thenLast.Kind()),
		}
	}
	if !(elseIsRes || elseIsFail) {
		return &dag.ValidationError{
			Pos:  elseLast.Position(),
			Flow: flow.Name,
			Msg:  fmt.Sprintf("if_cond expression mode: else_-branch last node must be result(...) or fail(...), got %s", elseLast.Kind()),
		}
	}

	// Case 3: at least one branch must end in Result; both-Fail rejects.
	if !thenIsRes && !elseIsRes {
		return &dag.ValidationError{
			Pos:  n.Pos,
			Flow: flow.Name,
			Msg:  "if_cond expression mode: at least one branch must end in result(...); both-fail is procedural-guard territory — remove output_alias",
		}
	}

	// Cases 4 + 5: branch-equality (key sets + per-key TypeInfo with
	// Opaque-defers). Only when BOTH branches end in Result; if one
	// side is Fail the surviving branch's keys/types stand alone.
	if thenIsRes && elseIsRes {
		if err := p.compareResultBranches(flow, n, thenRes, elseRes, schema); err != nil {
			return err
		}
	}
	return nil
}

// compareResultBranches enforces D4.2-09 case 4 (key-set equality) +
// D4.2-11 (per-shared-key TypeInfo strict-equality with Opaque-defers).
// Re-runs inferType per branch using the proper state schema — plan
// 04.2-02's builtinResult populated Result.Types with an EMPTY schema
// as a placeholder; THIS pass is authoritative.
func (p *Parser) compareResultBranches(
	flow *dag.Flow,
	n *dag.IfCond,
	then, els *dag.Result,
	schema stateSchema,
) error {
	_ = n // pos comes from then.Pos / els.Pos in the message
	thenTypes := p.reinferResultTypes(then, schema)
	elseTypes := p.reinferResultTypes(els, schema)

	thenKeys := append([]string(nil), then.Keys...)
	sort.Strings(thenKeys)
	elseKeys := append([]string(nil), els.Keys...)
	sort.Strings(elseKeys)

	// Case 4: key-set equality.
	if !equalStringSlices(thenKeys, elseKeys) {
		diff := symmetricDiff(thenKeys, elseKeys)
		return &dag.ValidationError{
			Pos:  then.Pos,
			Flow: flow.Name,
			Msg: fmt.Sprintf(
				"if_cond expression mode: branches disagree on result keys (then=%v, else=%v, symmetric-difference=%v); else-branch result at %s",
				thenKeys, elseKeys, diff, els.Pos),
		}
	}

	// Case 5: per-shared-key strict-equality with Opaque-defers.
	for _, k := range thenKeys {
		tt := thenTypes[k]
		et := elseTypes[k]
		// Defer when EITHER side is Opaque (D4.2-11 Pitfall 6).
		if _, isOpaque := tt.(TypeOpaque); isOpaque {
			continue
		}
		if _, isOpaque := et.(TypeOpaque); isOpaque {
			continue
		}
		if !Equal(tt, et) {
			return &dag.ValidationError{
				Pos:  then.Pos,
				Flow: flow.Name,
				Msg: fmt.Sprintf(
					"if_cond expression mode: branches disagree on key %q: then=%s, else=%s; wrap one side with float(x)/int(x)/str(x) to widen explicitly (else-branch result at %s)",
					k, typeInfoString(tt), typeInfoString(et), els.Pos),
			}
		}
	}
	return nil
}

// reinferResultTypes re-runs inferType per result-value lambda using
// the actual state schema visible at the if_cond. Plan 04.2-02's
// builtinResult populated Result.Types against an EMPTY schema as a
// best-effort placeholder; this pass is authoritative.
//
// The original AST expression is recoverable from the synthesized
// `<result:...>` filename: `result_lam = lambda ctx: <expr>`. Re-parse
// and pluck the lambda body. Defensive fallback to TypeOpaque{} for
// any failure path (missing BodyPos, missing fileBytes entry, parse
// error, no lambda found) so the comparison defers rather than
// surfacing internal-state errors as user errors.
func (p *Parser) reinferResultTypes(r *dag.Result, schema stateSchema) map[string]TypeInfo {
	out := make(map[string]TypeInfo, len(r.Keys))
	for _, k := range r.Keys {
		captured := r.Values[k]
		if captured == nil || !captured.BodyPos.IsValid() {
			out[k] = TypeOpaque{}
			continue
		}
		src, ok := p.fileBytes[captured.BodyPos.Filename()]
		if !ok || src == nil {
			out[k] = TypeOpaque{}
			continue
		}
		file, err := defaultFileOptions().Parse(captured.BodyPos.Filename(), src, 0)
		if err != nil || file == nil {
			out[k] = TypeOpaque{}
			continue
		}
		lam := findFirstLambda(file)
		if lam == nil {
			out[k] = TypeOpaque{}
			continue
		}
		firstParam := "ctx"
		if len(lam.Params) > 0 {
			if id, isId := lam.Params[0].(*syntax.Ident); isId {
				firstParam = id.Name
			}
		}
		out[k] = inferType(lam.Body, schema, firstParam)
	}
	return out
}

// findFirstLambda walks a parsed *syntax.File and returns the first
// *syntax.LambdaExpr it finds. The synthesized `<result:...>` file has
// exactly one lambda: `result_lam = lambda ctx: <expr>`.
func findFirstLambda(file *syntax.File) *syntax.LambdaExpr {
	var found *syntax.LambdaExpr
	syntax.Walk(file, func(node syntax.Node) bool {
		if found != nil {
			return false
		}
		if lam, ok := node.(*syntax.LambdaExpr); ok {
			found = lam
			return false
		}
		return true
	})
	return found
}

// equalStringSlices reports whether a and b have identical contents in
// the same order. Used after sort.Strings to test key-set equality.
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// symmetricDiff returns the keys present in exactly one of a/b (sorted
// for deterministic error messages).
func symmetricDiff(a, b []string) []string {
	m := make(map[string]int)
	for _, k := range a {
		m[k]++
	}
	for _, k := range b {
		m[k]++
	}
	var diff []string
	for k, v := range m {
		if v == 1 {
			diff = append(diff, k)
		}
	}
	sort.Strings(diff)
	return diff
}
