package parser

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// nodeValue is a private wrapper that lets *dag.Node values flow through
// the Starlark value system without making each dag type implement the
// full starlark.Value contract.
//
// Why this exists: builtinStep returns a value that ends up in flow()'s
// `steps=` list (a *starlark.List). Starlark requires every list entry to
// satisfy starlark.Value. We could (a) make every Node a starlark.Value, or
// (b) wrap them in this thin shim. Option (b) keeps pkg/dag pure data and
// confines Starlark coupling to pkg/parser, where it belongs.
//
// Hash() returns an error — node values are not hashable (no canonical
// equality across reparses). Freeze() is a no-op because dag types are
// immutable once constructed (the parser builds them in one shot).
type nodeValue struct {
	Node dag.Node
}

var _ starlark.Value = (*nodeValue)(nil)

// String returns "<Kind>(<position>)" for debug output.
func (n *nodeValue) String() string {
	return fmt.Sprintf("%s(%s)", n.Node.Kind(), n.Node.Position())
}

// Type returns Node.Kind() so Starlark callers see "Step", "IfCond", etc.
func (n *nodeValue) Type() string { return n.Node.Kind() }

// Truth marks every node value as truthy.
func (n *nodeValue) Truth() starlark.Bool { return starlark.True }

// Hash refuses hashability — nodes aren't keys.
func (n *nodeValue) Hash() (uint32, error) {
	return 0, fmt.Errorf("%s is not hashable", n.Node.Kind())
}

// Freeze is a no-op: dag types are constructed once and treated as immutable.
func (n *nodeValue) Freeze() {}

// wrapNode boxes a dag.Node into a starlark.Value for return from a builtin.
func wrapNode(n dag.Node) starlark.Value { return &nodeValue{Node: n} }

// unwrapNode unboxes a list entry back to a dag.Node, returning a *dag.ParseError
// when the list contains non-node values (e.g. a stray ActionRef in `steps=`).
func unwrapNode(v starlark.Value, callPos syntax.Position, listKwarg string) (dag.Node, error) {
	nv, ok := v.(*nodeValue)
	if !ok {
		return nil, &dag.ParseError{
			Pos: callPos,
			Msg: fmt.Sprintf("%s: expected a flow node (step/if_cond/script/for_each_parallel/call_flow), got %s", listKwarg, v.Type()),
		}
	}
	return nv.Node, nil
}

// callerPosition extracts the .star call-site position. Pitfall #3: use
// thread.CallFrame(1).Pos, NOT fn.Position() — the latter is the def site
// of the builtin, useless for D-04 attribution.
func callerPosition(thread *starlark.Thread) syntax.Position {
	if thread.CallStackDepth() < 2 {
		return syntax.Position{}
	}
	return thread.CallFrame(1).Pos
}

// wrapBuiltinError tags an error with the call-site position. Used when
// starlark.UnpackArgs or our own validation rejects a builtin's args.
func (p *Parser) wrapBuiltinError(opName string, thread *starlark.Thread, err error) error {
	// Already typed — don't double-wrap.
	if _, ok := err.(*dag.ParseError); ok {
		return err
	}
	if _, ok := err.(*dag.ValidationError); ok {
		return err
	}
	return &dag.ParseError{
		Pos:     callerPosition(thread),
		Msg:     fmt.Sprintf("%s: %v", opName, err),
		Wrapped: err,
	}
}

// =============================================================================
// builtinFlow — flow(name=..., inputs=..., steps=[...]) → *dag.Flow
// =============================================================================

// builtinFlow constructs a *dag.Flow from kwargs, registers it in the
// parser session's flow map (D-15: error on duplicate names), and returns
// starlark.None. The flow is captured by name as a side effect — the .star
// author writes `flow(...)` as a top-level statement, not for its return
// value.
func (p *Parser) builtinFlow(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name      string
		inputs    *starlark.Dict
		stepsLst  *starlark.List
		taskQueue string // D3-19 — empty string == "inherit worker default"
	)
	if err := starlark.UnpackArgs("flow", args, kwargs,
		"name", &name,
		"inputs?", &inputs,
		"steps", &stepsLst,
		"task_queue?", &taskQueue,
	); err != nil {
		return nil, p.wrapBuiltinError("flow", thread, err)
	}
	pos := callerPosition(thread)

	// D3-19: distinguish "kwarg omitted" (allowed; defaults to "") from
	// "kwarg supplied as empty string" (rejected). Walk kwargs to detect.
	if hasKwarg(kwargs, "task_queue") && taskQueue == "" {
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: "flow: task_queue must be non-empty when provided",
		}
	}

	if existing, dup := p.flows[name]; dup {
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: fmt.Sprintf("duplicate flow name %q (also defined at %s)", name, existing.Pos),
		}
	}

	inputsMap, err := convertInputsDict(inputs, pos)
	if err != nil {
		return nil, err
	}
	body, err := convertNodeList(stepsLst, pos, "flow.steps")
	if err != nil {
		return nil, err
	}

	// D4.1-16: optional ${...} interpolation in the flow name. The
	// authoritative duplicate-detection key remains the LITERAL name —
	// two flows with different templates that resolve to the same string
	// at runtime do NOT collide here (documented v1 limitation; the
	// duplicate check above already keyed on the literal name).
	var flowNameFn *dag.CapturedLambda
	if strings.Contains(name, "${") {
		desugared, derr := p.desugarInterpolation(name, pos)
		if derr != nil {
			return nil, derr
		}
		flowNameFn = desugared
	}

	f := &dag.Flow{
		Pos:       pos,
		Name:      name,
		NameFn:    flowNameFn,
		Inputs:    inputsMap,
		Body:      body,
		TaskQueue: taskQueue,
	}
	p.flows[name] = f
	return starlark.None, nil
}

// hasKwarg returns true when the named kwarg appears in the kwargs slice.
// Used to distinguish "absent" from "supplied as zero-value" — UnpackArgs
// can't tell us which happened. Mirrors the inline retry/timeout presence
// detection used in builtinStep / builtinForEachParallel.
func hasKwarg(kwargs []starlark.Tuple, name string) bool {
	for _, kv := range kwargs {
		if k, ok := kv[0].(starlark.String); ok && string(k) == name {
			return true
		}
	}
	return false
}

// convertInputsDict turns the optional `inputs={"name": "type"}` kwarg into a
// Go map. Phase 1 only stores type-hint strings (used by Phase 4's static
// validator); the parser does not interpret them.
func convertInputsDict(d *starlark.Dict, callPos syntax.Position) (map[string]string, error) {
	if d == nil || d.Len() == 0 {
		return nil, nil
	}
	out := make(map[string]string, d.Len())
	for _, item := range d.Items() {
		k, ok := item[0].(starlark.String)
		if !ok {
			return nil, &dag.ParseError{
				Pos: callPos,
				Msg: fmt.Sprintf("flow.inputs: keys must be string, got %s", item[0].Type()),
			}
		}
		v, ok := item[1].(starlark.String)
		if !ok {
			return nil, &dag.ParseError{
				Pos: callPos,
				Msg: fmt.Sprintf("flow.inputs[%q]: value must be string type-hint, got %s", string(k), item[1].Type()),
			}
		}
		out[string(k)] = string(v)
	}
	return out, nil
}

// convertNodeList unwraps every entry of a Starlark list back to dag.Node.
// Used by flow.steps, if_cond.then/else_, for_each_parallel.steps.
func convertNodeList(lst *starlark.List, callPos syntax.Position, kwargName string) ([]dag.Node, error) {
	if lst == nil {
		return nil, nil
	}
	out := make([]dag.Node, 0, lst.Len())
	iter := lst.Iterate()
	defer iter.Done()
	var v starlark.Value
	for iter.Next(&v) {
		n, err := unwrapNode(v, callPos, kwargName)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// =============================================================================
// builtinStep — step(action=..., block=[...], retry=..., timeout=...)
// =============================================================================

// builtinStep produces a *dag.Step. Exactly one of `action`, `block`,
// `action_fn`, `block_fn` must be provided (D4.1-06 4-way mutual
// exclusion). `retry` and `timeout` are optional DSL-08 kwargs that decode
// through their respective starlark.Unpacker implementations. `name` is
// the optional display name (D4.1-15) and is run through
// desugarInterpolation so `${ctx.x}` markers populate Step.NameFn.
func (p *Parser) builtinStep(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		action      starlark.Value
		block       *starlark.List
		actionFnVal starlark.Value
		blockFnVal  starlark.Value
		nameVal     starlark.Value
		retry       = &dag.RetryPolicy{}
		timeout     = &dag.Timeout{}
		taskQueue   string // D3-19 — empty string == "inherit flow's task queue"
	)
	// `action` is intentionally accepted as a generic starlark.Value so we
	// can return a clean error when consultants pass a non-ActionRef
	// (e.g., a string). Same for block — UnpackArgs gets the list, we
	// unwrap entries below. `action_fn`/`block_fn`/`name` are also generic
	// values so we can run our own type checks and (for name) interpolation
	// desugaring without UnpackArgs collapsing the value to a string first.
	hasRetry := false
	hasTimeout := false
	if err := starlark.UnpackArgs("step", args, kwargs,
		"action?", &action,
		"block?", &block,
		"action_fn?", &actionFnVal,
		"block_fn?", &blockFnVal,
		"name?", &nameVal,
		"retry?", retry,
		"timeout?", timeout,
		"task_queue?", &taskQueue,
	); err != nil {
		return nil, p.wrapBuiltinError("step", thread, err)
	}
	// UnpackArgs cannot tell us whether retry/timeout were provided vs.
	// zero-valued. Walk kwargs once to detect presence.
	for _, kv := range kwargs {
		if k, ok := kv[0].(starlark.String); ok {
			switch string(k) {
			case "retry":
				hasRetry = true
			case "timeout":
				hasTimeout = true
			}
		}
	}

	pos := callerPosition(thread)

	// D3-19: reject step(task_queue="") — empty string supplied is invalid.
	if hasKwarg(kwargs, "task_queue") && taskQueue == "" {
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: "step: task_queue must be non-empty when provided",
		}
	}

	// D4.1-06: 4-way mutual exclusion across action / block / action_fn /
	// block_fn. Exactly one MUST be present. The canonical error message
	// is verbatim across both "got 0" and "got 2+" cases for grep-friendly
	// pinning by downstream tests.
	hasAction := action != nil && action != starlark.None
	hasBlock := block != nil && block.Len() > 0
	hasActionFn := actionFnVal != nil && actionFnVal != starlark.None
	hasBlockFn := blockFnVal != nil && blockFnVal != starlark.None
	count := 0
	for _, b := range []bool{hasAction, hasBlock, hasActionFn, hasBlockFn} {
		if b {
			count++
		}
	}
	switch {
	case count == 0:
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: "step: must provide action, block, action_fn, or block_fn",
		}
	case count > 1:
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: "step: must provide exactly one of action, block, action_fn, or block_fn",
		}
	}

	// D4.1-15: optional name kwarg with ${...} interpolation.
	var stepName string
	var stepNameFn *dag.CapturedLambda
	if nameVal != nil && nameVal != starlark.None {
		nameStr, isStr := nameVal.(starlark.String)
		if !isStr {
			return nil, &dag.ParseError{
				Pos: pos,
				Msg: fmt.Sprintf("step.name: expected string, got %s", nameVal.Type()),
			}
		}
		raw := string(nameStr)
		desugared, err := p.desugarInterpolation(raw, pos)
		if err != nil {
			return nil, err
		}
		if desugared != nil {
			stepNameFn = desugared
		} else {
			stepName = raw
		}
	}

	var (
		actions      []*dag.ActionRef
		stepActionFn *dag.CapturedLambda
		stepBlockFn  *dag.CapturedLambda
	)
	switch {
	case hasAction:
		ar, ok := action.(*dag.ActionRef)
		if !ok {
			return nil, &dag.ParseError{
				Pos: pos,
				Msg: fmt.Sprintf("step.action: expected ActionRef from an extension factory, got %s", action.Type()),
			}
		}
		// D4.1-05: walk the kwargs dict and replace any string values
		// containing `${...}` with *StarlarkLambda wrappers. Must run
		// BEFORE the dict is frozen by downstream finalize passes.
		if err := p.desugarActionRefKwargs(ar); err != nil {
			return nil, err
		}
		actions = []*dag.ActionRef{ar}
	case hasBlock:
		actions = make([]*dag.ActionRef, 0, block.Len())
		iter := block.Iterate()
		defer iter.Done()
		var v starlark.Value
		for iter.Next(&v) {
			ar, ok := v.(*dag.ActionRef)
			if !ok {
				return nil, &dag.ParseError{
					Pos: pos,
					Msg: fmt.Sprintf("step.block: every entry must be an ActionRef, got %s", v.Type()),
				}
			}
			if err := p.desugarActionRefKwargs(ar); err != nil {
				return nil, err
			}
			actions = append(actions, ar)
		}
	case hasActionFn:
		captured, err := p.captureLambda(thread, "action_fn", actionFnVal)
		if err != nil {
			return nil, err
		}
		stepActionFn = captured
	case hasBlockFn:
		captured, err := p.captureLambda(thread, "block_fn", blockFnVal)
		if err != nil {
			return nil, err
		}
		stepBlockFn = captured
	}

	step := &dag.Step{
		Pos:       pos,
		Actions:   actions,
		Name:      stepName,
		NameFn:    stepNameFn,
		ActionFn:  stepActionFn,
		BlockFn:   stepBlockFn,
		TaskQueue: taskQueue,
	}
	if hasRetry {
		step.Retry = retry
	}
	if hasTimeout {
		step.Timeout = timeout
	}
	return wrapNode(step), nil
}

// desugarActionRefKwargs walks the kwargs *Dict on an ActionRef and,
// when any string value contains a `${...}` interpolation marker (or
// only `$$` escapes), rebuilds the entire kwargs *Dict with the lambda
// or de-escaped value substituted. Always-rebuild handles the http
// extension shape where the per-method builtin freezes its output dict
// before returning the ActionRef — mutating in place would surface as
// "cannot insert into frozen hash table". The fresh dict is left
// unfrozen; downstream finalize / activity-side dispatch handle freeze
// when appropriate.
//
// ar.Pos is used as openPos for the desugarer; the scanner's per-${
// position adjustment computes the precise line+col of each marker
// within the string value, so error attribution remains user-faithful.
func (p *Parser) desugarActionRefKwargs(ar *dag.ActionRef) error {
	if ar == nil || ar.Kwargs == nil {
		return nil
	}
	// First pass: detect whether any value needs rewriting. If not,
	// preserve the original (possibly frozen) dict unchanged.
	items := ar.Kwargs.Items()
	needRewrite := false
	for _, item := range items {
		valStr, isValStr := item[1].(starlark.String)
		if !isValStr {
			continue
		}
		if strings.Contains(string(valStr), "$") {
			needRewrite = true
			break
		}
	}
	if !needRewrite {
		return nil
	}

	rebuilt := starlark.NewDict(len(items))
	for _, item := range items {
		key := item[0]
		val := item[1]
		valStr, isValStr := val.(starlark.String)
		if !isValStr || !strings.Contains(string(valStr), "$") {
			if err := rebuilt.SetKey(key, val); err != nil {
				return err
			}
			continue
		}
		raw := string(valStr)
		captured, err := p.desugarInterpolation(raw, ar.Pos)
		if err != nil {
			return err
		}
		if captured != nil {
			if err := rebuilt.SetKey(key, dag.NewStarlarkLambda(captured)); err != nil {
				return err
			}
			continue
		}
		// No interpolation found but the string contained $$ escapes —
		// rebuild the de-escaped literal so the activity sees the
		// unescaped form. scanInterpolation collapses $$ into a single $
		// inside Parts[i].Text; we concatenate text parts to recover the
		// rendered string.
		scanned, scanErr := scanInterpolation(raw, ar.Pos)
		if scanErr != nil {
			return scanErr
		}
		var deEsc strings.Builder
		for _, part := range scanned.Parts {
			if part.Kind == "text" {
				deEsc.WriteString(part.Text)
			}
		}
		if err := rebuilt.SetKey(key, starlark.String(deEsc.String())); err != nil {
			return err
		}
	}
	ar.Kwargs = rebuilt
	return nil
}

// =============================================================================
// builtinIfCond — if_cond(cond=lambda ctx: ..., then=[...], else_=[...])
// =============================================================================

func (p *Parser) builtinIfCond(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		cond     starlark.Value
		thenLst  *starlark.List
		elseLst  *starlark.List
	)
	if err := starlark.UnpackArgs("if_cond", args, kwargs,
		"cond", &cond,
		"then", &thenLst,
		"else_?", &elseLst,
	); err != nil {
		return nil, p.wrapBuiltinError("if_cond", thread, err)
	}
	pos := callerPosition(thread)

	captured, err := p.captureLambda(thread, "cond", cond)
	if err != nil {
		return nil, err
	}
	thenBody, err := convertNodeList(thenLst, pos, "if_cond.then")
	if err != nil {
		return nil, err
	}
	elseBody, err := convertNodeList(elseLst, pos, "if_cond.else_")
	if err != nil {
		return nil, err
	}

	return wrapNode(&dag.IfCond{
		Pos:      pos,
		LambdaID: captured.ID,
		Then:     thenBody,
		Else:     elseBody,
	}), nil
}

// =============================================================================
// builtinScript — script(id=..., fn=lambda ctx: ..., output_alias=...)
// =============================================================================

func (p *Parser) builtinScript(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		idVal starlark.Value
		lamFn starlark.Value
		alias string
	)
	if err := starlark.UnpackArgs("script", args, kwargs,
		"id", &idVal,
		"fn", &lamFn,
		"output_alias", &alias,
	); err != nil {
		return nil, p.wrapBuiltinError("script", thread, err)
	}
	pos := callerPosition(thread)

	idStr, ok := idVal.(starlark.String)
	if !ok {
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: fmt.Sprintf("script.id: expected string, got %s", idVal.Type()),
		}
	}
	id := string(idStr)

	// D4.1-02: optional ${...} interpolation in the script id. Same
	// shape as flow.NameFn / step.NameFn — literal template kept on
	// Script.ID for cross-script keys; IDFn carries the synthesized
	// *CapturedLambda when interpolation is present.
	var scriptIDFn *dag.CapturedLambda
	if strings.Contains(id, "${") {
		desugared, derr := p.desugarInterpolation(id, pos)
		if derr != nil {
			return nil, derr
		}
		scriptIDFn = desugared
	}

	captured, err := p.captureLambda(thread, "fn", lamFn)
	if err != nil {
		return nil, err
	}

	return wrapNode(&dag.Script{
		Pos:         pos,
		ID:          id,
		IDFn:        scriptIDFn,
		LambdaID:    captured.ID,
		OutputAlias: alias,
	}), nil
}

// =============================================================================
// builtinForEachParallel — for_each_parallel(items=..., item=..., steps=[...])
// =============================================================================

// builtinForEachParallel accepts items as either a static list literal OR a
// lambda producer. Type switch determines which: *starlark.List → static
// (ItemsLiteral), *starlark.Function → lambda (ItemsLambdaID). Anything
// else is an error.
func (p *Parser) builtinForEachParallel(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		items    starlark.Value
		itemVar  string
		stepsLst *starlark.List
		retry    = &dag.RetryPolicy{}
		timeout  = &dag.Timeout{}
		maxConc  int // D3-13 — 0 == "interpreter default (10)"; negative rejected below
	)
	if err := starlark.UnpackArgs("for_each_parallel", args, kwargs,
		"items", &items,
		"item", &itemVar,
		"steps", &stepsLst,
		"retry?", retry,
		"timeout?", timeout,
		"max_concurrency?", &maxConc,
	); err != nil {
		return nil, p.wrapBuiltinError("for_each_parallel", thread, err)
	}
	hasRetry := false
	hasTimeout := false
	for _, kv := range kwargs {
		if k, ok := kv[0].(starlark.String); ok {
			switch string(k) {
			case "retry":
				hasRetry = true
			case "timeout":
				hasTimeout = true
			}
		}
	}

	pos := callerPosition(thread)

	// D3-13: reject negative max_concurrency at parse time. Zero is allowed
	// (interpreter applies the documented default). Position-aware error.
	if maxConc < 0 {
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: "for_each_parallel: max_concurrency must be >= 0",
		}
	}

	body, err := convertNodeList(stepsLst, pos, "for_each_parallel.steps")
	if err != nil {
		return nil, err
	}

	node := &dag.ForEachParallel{
		Pos:            pos,
		ItemVar:        itemVar,
		Steps:          body,
		MaxConcurrency: maxConc,
	}
	if hasRetry {
		node.Retry = retry
	}
	if hasTimeout {
		node.Timeout = timeout
	}

	switch v := items.(type) {
	case *starlark.List:
		// Static literal — convert each entry to Go value.
		literal := make([]any, 0, v.Len())
		iter := v.Iterate()
		defer iter.Done()
		var elem starlark.Value
		for iter.Next(&elem) {
			gv, err := starlarkLiteralToGo(elem)
			if err != nil {
				return nil, &dag.ParseError{
					Pos: pos,
					Msg: fmt.Sprintf("for_each_parallel.items literal: %v", err),
				}
			}
			literal = append(literal, gv)
		}
		node.ItemsLiteral = literal
	case *starlark.Function:
		captured, err := p.captureLambda(thread, "items", v)
		if err != nil {
			return nil, err
		}
		node.ItemsLambdaID = captured.ID
	default:
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: fmt.Sprintf("for_each_parallel.items: expected list literal or lambda, got %s", items.Type()),
		}
	}

	if err := node.Validate(); err != nil {
		return nil, &dag.ParseError{Pos: pos, Msg: err.Error()}
	}
	return wrapNode(node), nil
}

// starlarkLiteralToGo converts a literal Starlark value (used in
// for_each_parallel items literals) to a Go any. Phase 1 covers the
// primitive types fixtures need; Phase 3 / Phase 6 will extend.
func starlarkLiteralToGo(v starlark.Value) (any, error) {
	switch x := v.(type) {
	case starlark.String:
		return string(x), nil
	case starlark.Int:
		i, ok := x.Int64()
		if !ok {
			return x.String(), nil // overflow — store as string
		}
		return i, nil
	case starlark.Float:
		return float64(x), nil
	case starlark.Bool:
		return bool(x), nil
	case starlark.NoneType:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported literal type %s", v.Type())
	}
}

// =============================================================================
// builtinCallFlow — call_flow(name=..., inputs=..., child_options=...)
// =============================================================================

// builtinCallFlow records a CallFlow node with Name set; cross-flow
// resolution (matching Name against the parser session's flow map) happens
// at finalize time per D-16. Returns *nodeValue so it can sit inside a
// flow.steps list.
func (p *Parser) builtinCallFlow(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name      string
		inputsD   *starlark.Dict
		childOptD *starlark.Dict
	)
	if err := starlark.UnpackArgs("call_flow", args, kwargs,
		"name", &name,
		"inputs?", &inputsD,
		"child_options?", &childOptD,
	); err != nil {
		return nil, p.wrapBuiltinError("call_flow", thread, err)
	}
	pos := callerPosition(thread)

	inputs, err := convertAnyDict(inputsD, pos, "call_flow.inputs")
	if err != nil {
		return nil, err
	}
	childOptions, err := convertAnyDict(childOptD, pos, "call_flow.child_options")
	if err != nil {
		return nil, err
	}

	return wrapNode(&dag.CallFlow{
		Pos:          pos,
		Name:         name,
		Inputs:       inputs,
		ChildOptions: childOptions,
	}), nil
}

// convertAnyDict turns a Starlark dict into map[string]any using the literal
// converter. Used by call_flow.inputs and call_flow.child_options.
func convertAnyDict(d *starlark.Dict, callPos syntax.Position, kwargName string) (map[string]any, error) {
	if d == nil || d.Len() == 0 {
		return nil, nil
	}
	out := make(map[string]any, d.Len())
	for _, item := range d.Items() {
		k, ok := item[0].(starlark.String)
		if !ok {
			return nil, &dag.ParseError{
				Pos: callPos,
				Msg: fmt.Sprintf("%s: keys must be string, got %s", kwargName, item[0].Type()),
			}
		}
		gv, err := starlarkLiteralToGo(item[1])
		if err != nil {
			return nil, &dag.ParseError{
				Pos: callPos,
				Msg: fmt.Sprintf("%s[%q]: %v", kwargName, string(k), err),
			}
		}
		out[string(k)] = gv
	}
	return out, nil
}
