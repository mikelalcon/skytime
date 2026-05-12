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
//  1.5. validateTriggerFlowNames (D-07-12, Phase 7 Plan 03): every
//     registered trigger's FlowName must resolve to a known flow.
//     Runs RIGHT AFTER resolveCallFlows (both inspect p.flows) and
//     BEFORE any lint that might mask an unknown-flow error.
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
//  5.25. validateTriggerReqAccesses (D-07-05, Phase 7 Plan 03): for
//     every registered trigger, walk both lambdas (map and
//     idempotency_key) and reject any req.<attr> reference whose
//     attribute name is not in trig.Source.ReqSchema(). Runs AFTER
//     validateLambdaCtxAccesses so workflow-lambda ctx-typo errors
//     surface first per the existing finalize ordering doctrine.
//  5.5. validateResultPlacement (D4.2-04): every *dag.Result MUST be the
//     LAST node of an if_cond branch with OutputAlias set. Top-level
//     result(), mid-branch, inside for_each — all reject with the
//     output_alias hint. Narrow scope; complemented by the next pass.
//  5.6. validateLogStepPlacement (D-7.2.1-18, Phase 07.2.1): every
//     *dag.LogStep emitted by builtinLog* MUST be reachable by walking
//     flow bodies — otherwise the call happened at module scope (or
//     inside some other parse-time construct we don't allow log inside
//     of) and gets rejected with a position-aware ParseError. First-
//     of-its-kind module-scope orphan-node check; modeled on
//     validateResultPlacement above but the placement rule is "claimed
//     by flow walk" rather than "last node of an output_alias branch".
//  6. validateIfCondExpressionShape (D4.2-09 + D4.2-11): for every
//     *dag.IfCond with OutputAlias set, enforce the 5 expression-mode
//     rules — both branches present, last node Result/Fail, at-least-one-
//     Result, key-set equality across Result branches, and per-key
//     TypeInfo strict-equality with Opaque-defers. Re-runs inferType
//     against the proper per-branch state schema (plan 02's builtinResult
//     populates Result.Types against an empty placeholder). Ordering
//     rule: AFTER validateLambdaCtxAccesses (ctx-typo errors surface
//     first) and BEFORE validateActionRefKwargs (structural state errors
//     before kwarg-shape errors).
//  7. validateActionRefKwargs (D-11 defense in depth): cross-validate
//     every dag.ActionRef.Kwargs against its registered OperationSpec via
//     extension.DecodeKwargsFromDict. Catches hand-built ActionRefs (test
//     fixtures, future programmatic callers) where the per-call extension
//     factory was bypassed.
//  8. warnDuplicateTriggers (D-07-13, Phase 7 Plan 03): byte-identical
//     trigger duplicates accumulate a deferred warning on
//     p.triggerWarnings and are accepted. Always returns nil; runs LAST
//     so no real error is masked by warning state.
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
	if err := p.validateTriggerFlowNames(); err != nil { // D-07-12
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
	if err := p.validateTriggerReqAccesses(); err != nil { // D-07-05
		return err
	}
	if err := p.validateResultPlacement(); err != nil {
		return err
	}
	if err := p.validateLogStepPlacement(); err != nil { // D-7.2.1-18
		return err
	}
	if err := p.validateIfCondExpressionShape(); err != nil {
		return err
	}
	if err := p.validateActionRefKwargs(); err != nil {
		return err
	}
	return p.warnDuplicateTriggers() // D-07-13 — always nil
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

// validateLogStepPlacement enforces D-7.2.1-18: log.<level>(...) is only
// legal as a step inside a flow(...) body. The four builtinLog* factories
// stash every emitted *dag.LogStep into p.allLogSteps at construction time.
// Any LogStep that's NOT reached by walking p.flows[*].Body is therefore a
// module-scope orphan (the call happened outside any flow registration) and
// gets surfaced as a position-aware *dag.ParseError.
//
// First-of-its-kind module-scope orphan-node check (per 07.2.1-RESEARCH
// §Pitfall 2). Modeled on validateResultPlacement (D4.2-04) but the
// placement contract is "claimed by flow walk" rather than "last node of
// an output_alias if_cond branch". Both passes execute in the finalize
// chain; this one runs immediately after validateResultPlacement so
// downstream consumers (validateIfCondExpressionShape, etc.) never
// encounter orphan log steps.
//
// Returns the FIRST orphan found (matches the parser's at-most-one-error
// convention). Ordering is stable: walks p.allLogSteps in append order so
// the orphan that came earliest in source-evaluation order wins.
func (p *Parser) validateLogStepPlacement() error {
	if len(p.allLogSteps) == 0 {
		return nil
	}
	claimed := make(map[*dag.LogStep]bool, len(p.allLogSteps))
	for _, flow := range p.flows {
		walkBodyForLogSteps(flow.Body, claimed)
	}
	for _, ls := range p.allLogSteps {
		if !claimed[ls] {
			return &dag.ParseError{
				Pos: ls.Pos,
				Msg: fmt.Sprintf("log.%s: only valid as a step inside flow(...)", ls.Level),
			}
		}
	}
	return nil
}

// walkBodyForLogSteps recursively traverses a flow body, marking any
// *dag.LogStep it encounters as claimed. Recursion shape mirrors
// walkResolveCallFlows / walkValidateActionRefKwargs — descend into
// IfCond.Then/Else and ForEachParallel.Steps; all other node kinds are
// leaf-like for the purpose of log-step containment.
//
// Companion to validateLogStepPlacement above. Kept as a package-level
// function (not a method on *Parser) because it has no parser-state
// dependencies — pure traversal.
func walkBodyForLogSteps(body []dag.Node, claimed map[*dag.LogStep]bool) {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.LogStep:
			claimed[n] = true
		case *dag.IfCond:
			walkBodyForLogSteps(n.Then, claimed)
			walkBodyForLogSteps(n.Else, claimed)
		case *dag.ForEachParallel:
			walkBodyForLogSteps(n.Steps, claimed)
			// Other node types (Step, Script, Result, Fail, CallFlow) cannot
			// transitively contain a LogStep — they're leaf-like for this
			// check. If a future node kind is added that nests bodies (e.g.,
			// try/catch), update this switch and re-verify the orphan check
			// catches the same set of module-scope orphans.
		}
	}
}
