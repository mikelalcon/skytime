package parser

import (
	"fmt"
	"reflect"

	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// finalize runs after starlark.ExecFileOptions returns. It executes the
// post-exec passes that depend on the full parser session being populated:
//
//  1. resolveCallFlows (D-16): walk every Flow.Body recursively for
//     *dag.CallFlow nodes; look up CallFlow.Name in p.flows; set Resolved
//     or return *dag.ParseError "call_flow target not found".
//  2. lintMixedIdempotency (D2-05): each step(block=[...]) must be
//     homogeneous — either all idempotent OR a single non-idempotent
//     action. Mixed batches surface as *dag.ValidationError at parse time.
//  2.5. lintBlockFnIdempotency (D4.1-11): each step(block_fn=...)
//     lambda's body is walked via syntax.Walk; if all calls are
//     typed-recognized <ext>.<op> with declared Idempotent, the
//     batch's homogeneity is verified at parse time with the same
//     fix-suggestion shape as D2-05. Opaque shapes (helper functions,
//     indirect dispatch) defer to the runtime fallback (D4.1-12 in
//     pkg/activity).
//  3. lintBlockSize (D2-07): step(block=[...]) cannot exceed the parser's
//     maxBlockSize cap (default 50; configurable via WithMaxBlockSize).
//  4. lintEmptyTaskQueue (D3-19, defense in depth): documented stub for
//     direct dag.Flow / dag.Step construction outside the builtin path.
//     The primary rejection lives in builtinFlow / builtinStep.
//  5. validateLambdaCtxAccesses (D4-02): re-parse cached file bytes via
//     findCtxAccesses and reject any ctx.<name> reference inside a
//     captured lambda whose attribute name is not in the lexically-visible
//     state schema at that lambda's position. Stacking rules: flow inputs
//     at entry, += script.OutputAlias after each script, += ItemVar
//     inside for_each_parallel.Steps, if_cond branches see same pre-branch
//     state.
//  6. validateActionRefKwargs (D-11 defense in depth): cross-validate
//     every dag.ActionRef.Kwargs against its registered OperationSpec via
//     extension.DecodeKwargsFromDict. Catches hand-built ActionRefs (test
//     fixtures, future programmatic callers) where the per-call extension
//     factory was bypassed.
//
// finalize returns the FIRST error and stops; tests expect at-most-one
// surfaced error per parse. Phase-2 lints run AFTER call_flow resolution
// so a missing call_flow target surfaces with the more useful "target not
// found" error (rather than getting masked by a downstream lint). D4-02
// runs BEFORE D-11 cross-validate so structural state errors surface
// before kwarg-shape errors.
func (p *Parser) finalize() error {
	if err := p.resolveCallFlows(); err != nil {
		return err
	}
	if err := p.lintMixedIdempotency(); err != nil {
		return err
	}
	if err := p.lintBlockFnIdempotency(); err != nil {
		return err
	}
	if err := p.lintBlockSize(); err != nil {
		return err
	}
	if err := p.lintEmptyTaskQueue(); err != nil {
		return err
	}
	if err := p.validateLambdaCtxAccesses(); err != nil {
		return err
	}
	if err := p.validateResultPlacement(); err != nil {
		return err
	}
	return p.validateActionRefKwargs()
}

// resolveCallFlows walks every flow's body recursively and resolves
// *dag.CallFlow nodes against the parser-session flow map (D-16). A call
// to a flow that wasn't declared anywhere in the parsed session yields a
// *dag.ParseError; this is a parse-time check, never runtime.
func (p *Parser) resolveCallFlows() error {
	for _, flow := range p.flows {
		if err := p.walkResolveCallFlows(flow.Body); err != nil {
			return err
		}
	}
	return nil
}

// walkResolveCallFlows is the recursive helper for resolveCallFlows. It
// descends into IfCond.Then/Else and ForEachParallel.Steps so nested
// call_flow() invocations are resolved.
func (p *Parser) walkResolveCallFlows(body []dag.Node) error {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.CallFlow:
			target, ok := p.flows[n.Name]
			if !ok {
				return &dag.ParseError{
					Pos: n.Pos,
					Msg: fmt.Sprintf("call_flow target not found: %q", n.Name),
				}
			}
			n.Resolved = target
		case *dag.IfCond:
			if err := p.walkResolveCallFlows(n.Then); err != nil {
				return err
			}
			if err := p.walkResolveCallFlows(n.Else); err != nil {
				return err
			}
		case *dag.ForEachParallel:
			if err := p.walkResolveCallFlows(n.Steps); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateActionRefKwargs is the D-11 cross-validate (defense in depth).
// The per-call extension factory already runs UnpackOperationKwargs at
// *starlark.Builtin invocation time; this finalize pass re-validates every
// dag.ActionRef.Kwargs against its registered OperationSpec via
// extension.DecodeKwargsFromDict — catching hand-built ActionRefs (test
// fixtures, future programmatic callers) where the per-call factory was
// bypassed.
//
// First error short-circuits per finalize-pass convention. Errors are
// *dag.ValidationError with Flow + Action populated (Action = "ext.op"
// per D4-04). Pos comes from the enclosing Step.Pos because ActionRef
// itself doesn't carry a parse-time syntax.Position from this code path —
// the extension factory's ActionRef.Pos is the closest analogue but isn't
// always populated when callers hand-build, and the Step is the only
// guaranteed-present syntax-tree node enclosing the ActionRef.
func (p *Parser) validateActionRefKwargs() error {
	for _, flow := range p.flows {
		if err := p.walkValidateActionRefKwargs(flow.Name, flow.Body); err != nil {
			return err
		}
	}
	return nil
}

// walkValidateActionRefKwargs is the recursive helper for
// validateActionRefKwargs. Mirrors walkLintMixedIdempotency /
// walkLintBlockSize / walkBodyForCtxValidation for grep-friendly
// recursion uniformity.
func (p *Parser) walkValidateActionRefKwargs(flowName string, body []dag.Node) error {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.Step:
			for _, ar := range n.Actions {
				if err := p.crossValidateActionRef(flowName, n.Pos, ar); err != nil {
					return err
				}
			}
		case *dag.IfCond:
			if err := p.walkValidateActionRefKwargs(flowName, n.Then); err != nil {
				return err
			}
			if err := p.walkValidateActionRefKwargs(flowName, n.Else); err != nil {
				return err
			}
		case *dag.ForEachParallel:
			if err := p.walkValidateActionRefKwargs(flowName, n.Steps); err != nil {
				return err
			}
		}
	}
	return nil
}

// crossValidateActionRef cross-validates a single ActionRef. Defensive
// skips: unparseable Kind_, unknown extension, unknown operation, nil
// KwargsType — all silently bypassed because earlier passes already
// surface these conditions with better attribution. The point of this
// pass is to catch SCHEMA mismatches that slipped through the per-call
// factory.
func (p *Parser) crossValidateActionRef(flowName string, stepPos syntax.Position, ar *dag.ActionRef) error {
	extName, opName, ok := splitKind(ar.Kind_)
	if !ok {
		return nil
	}
	ext, ok := p.registry.Get(extName)
	if !ok {
		return nil
	}
	spec, ok := ext.Operations()[opName]
	if !ok || spec == nil || spec.KwargsType == nil {
		return nil
	}
	// reflect.New returns a *T pointing at a zero T; DecodeKwargsFromDict
	// requires a non-nil pointer to struct.
	target := reflect.New(spec.KwargsType).Interface()
	if err := extension.DecodeKwargsFromDict(opName, ar.Kwargs, target); err != nil {
		return &dag.ValidationError{
			Pos:     stepPos,
			Flow:    flowName,
			Action:  ar.Kind_,
			Msg:     fmt.Sprintf("kwarg cross-validate: %v", err),
			Wrapped: err,
		}
	}
	return nil
}
