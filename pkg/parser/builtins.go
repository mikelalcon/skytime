package parser

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
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

// skytime:doc summary="Declares a workflow — the top-level unit of orchestration."
// skytime:doc summary="Each .star file declares one or more flows; flow names must be unique within a parser session."
// skytime:doc returns="None (registers a *dag.Flow as a parse-time side effect)."
// skytime:doc since="phase-01"
// skytime:doc example="flow(\n    name = \"check_user\",\n    inputs = {\"user_id\": \"string\"},\n    steps = [\n        step(action = api.fetch_user(id = \"${ctx.user_id}\")),\n    ],\n)"
// skytime:doc see="step, script, if_cond, for_each_parallel, call_flow"
// skytime:doc param_name="string"
// skytime:doc desc_name="Unique flow identifier; supports ${ctx.expr} interpolation."
// skytime:doc param_inputs="dict[string,string]"
// skytime:doc desc_inputs="Type-hint map (e.g. {\"repo\":\"string\"}); seeds the parse-time state schema."
// skytime:doc param_steps="list[Node]"
// skytime:doc desc_steps="Body — list of step/if_cond/script/for_each_parallel/call_flow nodes."
// skytime:doc param_task_queue="string"
// skytime:doc desc_task_queue="Optional Temporal task queue override (D3-19); empty inherits worker default."
// skytime:doc param_description="string"
// skytime:doc desc_description="Optional free-form description shown by `skytime info`."
// builtinFlow constructs a *dag.Flow from kwargs, registers it in the
// parser session's flow map (D-15: error on duplicate names), and returns
// starlark.None. The flow is captured by name as a side effect — the .star
// author writes `flow(...)` as a top-level statement, not for its return
// value.
func (p *Parser) builtinFlow(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name        string
		inputs      *starlark.Dict
		stepsLst    *starlark.List
		taskQueue   string // D3-19 — empty string == "inherit worker default"
		description string // Quick 260504-k9c — optional free-form prose
	)
	if err := starlark.UnpackArgs("flow", args, kwargs,
		"name", &name,
		"inputs?", &inputs,
		"steps", &stepsLst,
		"task_queue?", &taskQueue,
		"description?", &description,
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
		Pos:         pos,
		Name:        name,
		NameFn:      flowNameFn,
		Inputs:      inputsMap,
		Description: description, // Quick 260504-k9c
		Body:        body,
		TaskQueue:   taskQueue,
	}
	p.flows[name] = f
	// Quick 260504-k9c: append name to flowOrder AFTER the duplicate-name
	// guard above (which returns early). Each successful registration
	// appends exactly once, preserving source-declaration order for
	// FlowsInOrder() / `skytime info`.
	p.flowOrder = append(p.flowOrder, name)
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

// skytime:doc summary="Declares a sequential workflow step that dispatches one or more extension actions."
// skytime:doc summary="Exactly ONE of action / block / action_fn / block_fn must be provided (D4.1-06 4-way mutual exclusion)."
// skytime:doc returns="A *dag.Step node consumed by flow(steps=[...])."
// skytime:doc since="phase-01"
// skytime:doc example="step(\n    name = \"Fetch ${ctx.repo}\",\n    action = http.get(path = \"/repos/${ctx.repo}\"),\n)"
// skytime:doc see="flow, if_cond, for_each_parallel"
// skytime:doc param_action="ActionRef"
// skytime:doc desc_action="Single static action (mutually exclusive with block/action_fn/block_fn)."
// skytime:doc param_block="list[ActionRef]"
// skytime:doc desc_block="Static batch of actions (homogeneous idempotency required, D2-05)."
// skytime:doc param_action_fn="lambda(ctx) -> ActionRef"
// skytime:doc desc_action_fn="Dynamic single-action lambda (D4.1-06); evaluated inside the workflow."
// skytime:doc param_block_fn="lambda(ctx) -> list[ActionRef]"
// skytime:doc desc_block_fn="Dynamic batch lambda (D4.1-07); empty list short-circuits without dispatch."
// skytime:doc param_name="string"
// skytime:doc desc_name="Display name; supports ${ctx.expr} interpolation (D4.1-15)."
// skytime:doc param_retry="RetryPolicy"
// skytime:doc desc_retry="Optional Temporal RetryPolicy (DSL-08)."
// skytime:doc param_timeout="Timeout"
// skytime:doc desc_timeout="Optional Temporal Timeout (DSL-08)."
// skytime:doc param_task_queue="string"
// skytime:doc desc_task_queue="Optional Temporal task queue override (D3-19); precedence: step > flow > worker."
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
// builtinIfCond — if_cond(cond=lambda ctx: ..., then=[...], else_=[...], output_alias?=...)
// =============================================================================

// skytime:doc summary="Conditional branch evaluated INSIDE the workflow (zero Temporal history events for the branch decision)."
// skytime:doc summary="Procedural mode (no output_alias): branches contain procedural nodes; today's behavior."
// skytime:doc summary="Expression mode (output_alias=\"X\"): both branches must end in result(value={...}) or fail(...); the bound value lands at ctx.X (DSL-14, D4.2-09)."
// skytime:doc returns="A *dag.IfCond node."
// skytime:doc since="phase-01 (procedural); phase-04.2 (expression mode)"
// skytime:doc example="if_cond(\n    output_alias = \"user\",\n    cond = lambda ctx: ctx.user_id != \"\",\n    then = [result(value = {\"id\": ctx.user_id, \"ok\": True})],\n    else_ = [fail(\"invalid user_id: '${ctx.user_id}'\")],\n)"
// skytime:doc see="result, fail, script"
// skytime:doc param_cond="lambda(ctx) -> bool"
// skytime:doc desc_cond="Predicate evaluated inside the workflow; lambda-time globals only (no time, no random, no I/O)."
// skytime:doc param_then="list[Node]"
// skytime:doc desc_then="Body executed when cond is truthy."
// skytime:doc param_else_="list[Node]"
// skytime:doc desc_else_="Body executed when cond is falsy."
// skytime:doc param_output_alias="string"
// skytime:doc desc_output_alias="When non-empty, switches to expression mode and binds branch result to ctx.<alias>."
func (p *Parser) builtinIfCond(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		cond        starlark.Value
		thenLst     *starlark.List
		elseLst     *starlark.List
		outputAlias string // D4.2-01 — empty string == procedural mode (today's behavior)
	)
	if err := starlark.UnpackArgs("if_cond", args, kwargs,
		"cond", &cond,
		"then", &thenLst,
		"else_?", &elseLst,
		"output_alias?", &outputAlias,
	); err != nil {
		return nil, p.wrapBuiltinError("if_cond", thread, err)
	}
	pos := callerPosition(thread)

	// D4.2-01 / D3-19 idiom: distinguish "kwarg omitted" (procedural
	// mode preserved) from "kwarg supplied as empty string" (parse
	// error). hasKwarg lets us tell which happened.
	if hasKwarg(kwargs, "output_alias") && outputAlias == "" {
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: "if_cond: output_alias must be non-empty if supplied",
		}
	}

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
		Pos:         pos,
		LambdaID:    captured.ID,
		OutputAlias: outputAlias,
		Then:        thenBody,
		Else:        elseBody,
	}), nil
}

// =============================================================================
// builtinFail — fail("msg") → *dag.Fail (D4.2-05/06)
// =============================================================================

// skytime:doc summary="Top-level builtin that emits a failure node — runtime raises a NonRetryableApplicationError at the .star callsite."
// skytime:doc summary="Top-level fail() (parse-time builtin) is distinct from lambda-time fail() (starlark.Universe builtin); they share the name across two predeclared environments (D4.2-05)."
// skytime:doc summary="Supports ${ctx.expr} interpolation in the message (D4.1-01)."
// skytime:doc returns="A *dag.Fail node."
// skytime:doc since="phase-04.2"
// skytime:doc example="if_cond(\n    cond = lambda ctx: ctx.user_id != \"\",\n    then = [step(action = api.fetch_user(id = ctx.user_id))],\n    else_ = [fail(\"user_id is required\")],\n)"
// skytime:doc see="result, if_cond"
// skytime:doc param_message="string"
// skytime:doc desc_message="Single positional argument; runtime renders the message at the .star callsite, with ${ctx.expr} interpolation evaluated inside the workflow."
// builtinFail emits a *dag.Fail node with literal Message and (when
// ${...} interpolation markers are present) MessageFn populated via
// the D4.1-01 desugarInterpolation path. Positional-only signature
// (kwargs rejected) — `fail("oops")` mirrors Python/Starlark's bare
// fail() shape so consultants don't have to remember a `msg=` kwarg.
//
// Dual-name semantics: this top-level fail() is registered in the
// PARSE-TIME globals (newParseTimeGlobals); the LAMBDA-TIME fail
// continues to live in pkg/bridge/lambda_globals.go via
// starlark.Universe["fail"]. The two predeclared environments are
// mutually exclusive (Starlark resolves names per the active env), so
// re-using the name is safe by design and produces the same observable
// surface — a NonRetryableErr with the user's message at the .star
// callsite. See pkg/parser/doc.go for full documentation.
//
// D4.2-07: fail() is allowed anywhere a body accepts nodes —
// procedural-mode if_cond branches (the procedural guard pattern:
// `if_cond(cond=..., then=[fail("invalid")])`) AND as the last node of
// an expression-mode branch.
func (p *Parser) builtinFail(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) > 0 {
		return nil, p.wrapBuiltinError("fail", thread,
			fmt.Errorf("positional-only signature; use fail(\"message\")"))
	}
	var msgVal starlark.Value
	if err := starlark.UnpackPositionalArgs("fail", args, nil, 1, &msgVal); err != nil {
		return nil, p.wrapBuiltinError("fail", thread, err)
	}
	msgStr, ok := msgVal.(starlark.String)
	if !ok {
		return nil, p.wrapBuiltinError("fail", thread,
			fmt.Errorf("message must be a string, got %s", msgVal.Type()))
	}
	msg := string(msgStr)
	callPos := callerPosition(thread)

	// D4.1-01 interpolation reuse: when ${...} markers are present in
	// the message, desugar into a *CapturedLambda. Empty marker (${})
	// or unterminated ${ are rejected by the desugarer with the
	// existing user-attribution.
	var msgFn *dag.CapturedLambda
	if strings.Contains(msg, "${") {
		desugared, err := p.desugarInterpolation(msg, callPos)
		if err != nil {
			return nil, err
		}
		msgFn = desugared
	}
	return wrapNode(&dag.Fail{
		Pos:       callPos,
		Message:   msg,
		MessageFn: msgFn,
	}), nil
}

// =============================================================================
// builtinResult — result(value={...}) → *dag.Result (D4.2-02..04)
// =============================================================================

// skytime:doc summary="Branch terminator that binds a typed dict-literal value to ctx.<output_alias> in the enclosing if_cond expression-mode."
// skytime:doc summary="Only legal as the LAST node of an expression-mode if_cond branch (i.e., if_cond(output_alias=\"X\", ...))."
// skytime:doc summary="Each value is captured as a per-key parse-time lambda, so ctx-references inside the dict are evaluated inside the workflow at branch-execute time."
// skytime:doc returns="A *dag.Result node (LEAF — no body, no children)."
// skytime:doc since="phase-04.2"
// skytime:doc example="if_cond(\n    output_alias = \"classification\",\n    cond = lambda ctx: ctx.size_bytes > 1000000,\n    then = [result(value = {\"tier\": \"large\", \"label\": \"needs sharding\"})],\n    else_ = [result(value = {\"tier\": \"small\", \"label\": \"single host fine\"})],\n)"
// skytime:doc see="if_cond, fail"
// skytime:doc param_value="dict (string-literal keys)"
// skytime:doc desc_value="Dict-literal at the call site; keys must be string literals; values are captured per-key as parse-time lambdas."
// builtinResult parses a `result(value={...})` call: kwarg-only signature,
// AST-level dict-literal validation, per-key value lambda synthesis, and
// per-key TypeInfo population via inferType. Emits *dag.Result with
// insertion-order Keys (replay determinism per D3-23 + Pitfall 5).
//
// D4.2-02..04: The value= argument MUST be a Starlark dict-literal at
// the call site (variable, lambda, function call all rejected). Every
// dict KEY must be a STRING literal at parse time (computed keys
// rejected). Each VALUE is captured as a per-key *dag.CapturedLambda
// via the synthesized-source path mirroring D4.1-01 interpolation.
//
// Note: builtinResult does NOT reject `result()` outside an
// expression-mode if_cond branch — that placement check is plan
// 04.2-03's job (in validateIfCondExpressionShape, alongside the
// branch-equality validator). For now, builtinResult always emits a
// *dag.Result; downstream finalize passes catch misuse.
func (p *Parser) builtinResult(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return nil, p.wrapBuiltinError("result", thread, fmt.Errorf("kwarg-only signature; use result(value={...})"))
	}
	// We accept the value= kwarg but ignore the (sentinel) value — the
	// real per-key lambdas + types are pre-built in preExecBuildResults
	// and looked up by callPos. UnpackArgs still runs to enforce the
	// presence of value= and reject unknown kwargs.
	var valueArg starlark.Value
	if err := starlark.UnpackArgs("result", args, kwargs, "value", &valueArg); err != nil {
		return nil, p.wrapBuiltinError("result", thread, err)
	}
	callPos := callerPosition(thread)

	// Look up the pre-built result by call position. The pre-exec scan
	// (preExecBuildResults) populates this BEFORE Starlark evaluates
	// any code, so by the time builtinResult runs the entry MUST be
	// present for any well-formed call site. A miss indicates either
	// (a) the user constructed result() programmatically through some
	// non-standard path, or (b) an internal bug — both are rejected
	// with a clear diagnostic.
	pre, ok := p.preBuiltResults[posKey(callPos)]
	if !ok {
		return nil, &dag.ParseError{
			Pos: callPos,
			Msg: "result: pre-build cache miss; result() must appear directly in source (no dynamic construction)",
		}
	}
	return wrapNode(pre), nil
}

// preExecBuildResults pre-scans fileBytes for `result(value=...)`
// CallExprs, validates the AST shape, builds *dag.Result objects
// upfront from the cached source bytes, and returns a REWRITTEN copy
// of fileBytes where each `result(value={...})` call has its value=
// dict-literal replaced with a length-preserving `0` sentinel. The
// rewritten copy is what Starlark sees at exec time — the original
// fileBytes (already cached at p.fileBytes[filename]) remain untouched
// for AST re-parse paths (D4-02 ctx-walk, D4.1-01 interpolation).
//
// Why pre-build + rewrite instead of doing the work in builtinResult
// at exec time:
//
//  1. The user's value= dict literal often references `ctx.<name>` at
//     top-level (e.g., `result(value={"x": ctx.helper})`). At top-level
//     `ctx` is not bound to anything; Starlark would error with
//     "undefined: ctx" before our builtin runs.
//
//  2. The plan's design (RESEARCH §Pattern 4) treats each value
//     expression as a per-key synthesized lambda — meaning the value
//     should NEVER be evaluated at parse time. Source rewriting is the
//     mechanism that makes that contract real: Starlark evaluates the
//     sentinel `0`, our builtin retrieves the pre-built *dag.Result.
//
//  3. Computed keys (`{ctx.k: 1}`) need a clean "dict keys must be
//     string literals" error at parse time. The pre-exec pass detects
//     these via AST inspection before Starlark exec runs and surfaces
//     the cleaner error.
//
// The rewrite is byte-equivalent in length: the dict-literal range
// (Lbrace through Rbrace inclusive) is replaced byte-by-byte; the
// first byte becomes `0`, every other non-newline byte becomes a space,
// every newline byte stays as a newline. Token positions of every
// other expression in the file remain stable, so lambda IDs (D-18) and
// error attribution remain unaffected.
//
// Returns the rewritten source. On AST validation errors (computed
// keys, non-dict-literal value=), returns (nil, *dag.ParseError).
func (p *Parser) preExecBuildResults(filename string, fileBytes []byte) ([]byte, error) {
	file, err := defaultFileOptions().Parse(filename, fileBytes, 0)
	if err != nil {
		// Defensive: if pre-parse fails, let starlark.ExecFile produce
		// the user-visible error (it will hit the same parse error
		// with identical attribution).
		return fileBytes, nil
	}

	// Walk for `result(...)` calls. Collect (call, dictExpr) pairs.
	type resultCall struct {
		call *syntax.CallExpr
		dict *syntax.DictExpr
	}
	var calls []resultCall
	var firstErr error
	syntax.Walk(file, func(n syntax.Node) bool {
		if firstErr != nil {
			return false
		}
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fn.(*syntax.Ident)
		if !ok || id.Name != "result" {
			return true
		}
		// Find value= kwarg.
		var valueExpr syntax.Expr
		var hasValue bool
		hasOtherKwargs := false
		hasPositional := false
		for _, arg := range call.Args {
			bin, ok := arg.(*syntax.BinaryExpr)
			if !ok || bin.Op != syntax.EQ {
				hasPositional = true
				continue
			}
			kid, ok := bin.X.(*syntax.Ident)
			if !ok {
				continue
			}
			if kid.Name == "value" {
				valueExpr = bin.Y
				hasValue = true
			} else {
				hasOtherKwargs = true
			}
		}
		_ = hasOtherKwargs // builtinResult validates at exec time
		if hasPositional {
			firstErr = &dag.ParseError{
				Pos: call.Lparen,
				Msg: "result: kwarg-only signature; use result(value={...})",
			}
			return false
		}
		if !hasValue {
			// Let builtinResult error via UnpackArgs missing-required.
			return true
		}
		dictExpr, ok := valueExpr.(*syntax.DictExpr)
		if !ok {
			firstErr = &dag.ParseError{
				Pos: call.Lparen,
				Msg: "result.value must be a dict literal",
			}
			return false
		}
		// Every key must be a STRING literal at parse time.
		for _, ent := range dictExpr.List {
			de, ok := ent.(*syntax.DictEntry)
			if !ok {
				continue
			}
			keyLit, isLit := de.Key.(*syntax.Literal)
			if !isLit || keyLit.Token != syntax.STRING {
				firstErr = &dag.ParseError{
					Pos: call.Lparen,
					Msg: "result.value: dict keys must be string literals",
				}
				return false
			}
		}
		calls = append(calls, resultCall{call: call, dict: dictExpr})
		return true
	})
	if firstErr != nil {
		return nil, firstErr
	}

	// Build *dag.Result for each collected call BEFORE source rewrite,
	// because per-key value lambda capture reads the original fileBytes
	// to slice value-expression source text.
	for _, rc := range calls {
		if err := p.buildPreExecResult(rc.call, rc.dict, fileBytes); err != nil {
			return nil, err
		}
	}

	// Rewrite the source: replace each dict-literal range with a
	// length-preserving `0`-sentinel + spaces (newlines preserved).
	rewritten := make([]byte, len(fileBytes))
	copy(rewritten, fileBytes)
	for _, rc := range calls {
		startOff, ok := lineColToByteOffset(fileBytes, rc.dict.Lbrace.Line, rc.dict.Lbrace.Col)
		if !ok {
			return nil, fmt.Errorf("internal: cannot map dict Lbrace %s into bytes", rc.dict.Lbrace)
		}
		endOff, ok := lineColToByteOffset(fileBytes, rc.dict.Rbrace.Line, rc.dict.Rbrace.Col)
		if !ok {
			return nil, fmt.Errorf("internal: cannot map dict Rbrace %s into bytes", rc.dict.Rbrace)
		}
		// Rbrace.Col points at the `}` character itself; include it.
		endOff++
		if endOff > len(rewritten) || startOff < 0 || endOff < startOff {
			return nil, fmt.Errorf("internal: invalid dict byte range [%d,%d) in %q", startOff, endOff, filename)
		}
		// First byte = '0' (so Starlark sees value=0 — a valid expr);
		// remaining non-newline bytes = ' '; preserve any '\n'.
		for i := startOff; i < endOff; i++ {
			if rewritten[i] == '\n' {
				continue
			}
			if i == startOff {
				rewritten[i] = '0'
			} else {
				rewritten[i] = ' '
			}
		}
	}
	return rewritten, nil
}

// buildPreExecResult constructs a *dag.Result from a result(...) call
// whose value= arg is a *syntax.DictExpr (already validated by the
// pre-exec scan). Per-key value lambdas are captured via
// captureResultValueLambda; per-key TypeInfo via inferType against an
// empty schema (Wave-1 — plan 04.2-03 will re-run inference against
// the proper branch-fork schema). The result is registered in
// p.preBuiltResults keyed by the call's Lparen position; builtinResult
// looks it up at exec time.
func (p *Parser) buildPreExecResult(call *syntax.CallExpr, dictExpr *syntax.DictExpr, fileBytes []byte) error {
	callPos := call.Lparen
	keys := make([]string, 0, len(dictExpr.List))
	values := make(map[string]*dag.CapturedLambda, len(dictExpr.List))
	types := make(map[string]any, len(dictExpr.List))
	for _, entry := range dictExpr.List {
		de, ok := entry.(*syntax.DictEntry)
		if !ok {
			return &dag.ParseError{Pos: callPos, Msg: "result.value: internal — dict entry has unexpected AST shape"}
		}
		keyLit, ok := de.Key.(*syntax.Literal)
		if !ok || keyLit.Token != syntax.STRING {
			// pre-exec walker should have caught this, defensive.
			return &dag.ParseError{Pos: callPos, Msg: "result.value: dict keys must be string literals"}
		}
		keyStr, ok := keyLit.Value.(string)
		if !ok {
			return &dag.ParseError{Pos: callPos, Msg: "result.value: internal — string-literal key has non-string value"}
		}
		captured, err := p.captureResultValueLambda(de.Value, callPos, keyStr, fileBytes)
		if err != nil {
			return err
		}
		keys = append(keys, keyStr)
		values[keyStr] = captured
		types[keyStr] = inferType(de.Value, newStateSchema(), "ctx")
	}
	res := &dag.Result{
		Pos:    callPos,
		Keys:   keys,
		Values: values,
		Types:  types,
	}
	p.preBuiltResults[posKey(callPos)] = res
	return nil
}

// posKey serializes a syntax.Position into a stable string key for
// the preBuiltResults map. Includes filename + line + col so multiple
// result() calls on the same line (different col) don't collide.
func posKey(pos syntax.Position) string {
	return fmt.Sprintf("%s:%d:%d", pos.Filename(), pos.Line, pos.Col)
}

// validateResultPlacement enforces D4.2-04: every *dag.Result MUST be
// the LAST node of an if_cond branch (then or else_) whose OutputAlias
// is non-empty. Anywhere else — top-level body, mid-branch, inside
// for_each_parallel — surfaces a *dag.ParseError pointing the user at
// the missing output_alias.
//
// Note on responsibility split: the BRANCH-EQUALITY validator (plan
// 04.2-03) enforces D4.2-09 (both-branches-present, last-node-Result/
// Fail, at-least-one-Result, key/type equality). validateResultPlacement
// is the narrower "result() can't appear here at all" gate that lands
// in plan 02 so a top-level `result()` produces a clean error before
// the branch-equality validator even runs.
//
// Rule 2 deviation (Wave-1): TestResult_RejectedOutsideExpressionMode
// (a plan-02 RED test) requires this check; tests/fixtures/
// result_outside_ifcond.star is the canonical fixture.
func (p *Parser) validateResultPlacement() error {
	for _, flow := range p.flows {
		if err := p.walkValidateResultPlacement(flow.Body, false); err != nil {
			return err
		}
	}
	return nil
}

// walkValidateResultPlacement is the recursive helper. inExprBranch is
// TRUE when the body being walked is the then OR else_ of an if_cond
// whose OutputAlias is set; inside such bodies, *dag.Result IS allowed
// at the LAST position (only). Any *dag.Result found in a body where
// inExprBranch is false rejects with the output_alias hint.
func (p *Parser) walkValidateResultPlacement(body []dag.Node, inExprBranch bool) error {
	for i, node := range body {
		switch n := node.(type) {
		case *dag.Result:
			isLast := i == len(body)-1
			if !inExprBranch || !isLast {
				return &dag.ParseError{
					Pos: n.Pos,
					Msg: "result(value=...) is only legal as the last node of an if_cond branch with output_alias set; remove the result() or wrap it in if_cond(output_alias=\"X\", ...)",
				}
			}
		case *dag.IfCond:
			childExprBranch := n.OutputAlias != ""
			if err := p.walkValidateResultPlacement(n.Then, childExprBranch); err != nil {
				return err
			}
			if err := p.walkValidateResultPlacement(n.Else, childExprBranch); err != nil {
				return err
			}
		case *dag.ForEachParallel:
			// Result inside a for_each body is also disallowed (the
			// loop body has no output_alias). Walk with inExprBranch
			// false so any *dag.Result in n.Steps rejects.
			if err := p.walkValidateResultPlacement(n.Steps, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// findResultValueArg re-parses fileBytes and locates the value= kwarg
// argument of the result(...) call whose Lparen matches callPos.
// Returns the AST expression node (NOT evaluated); caller asserts
// *syntax.DictExpr to enforce the dict-literal contract.
//
// Mirrors the re-parse approach in pkg/parser/block_fn_lint.go::
// classifyBlockFn — the cached file bytes are walked once via
// (*syntax.FileOptions).Parse + syntax.Walk; the matching CallExpr is
// found by Lparen position equality.
//
// callPos is `thread.CallFrame(1).Pos` — points at the call's Lparen.
// (Verified: starlark builtin invocation reports Lparen as the caller
// position; matches the `pos := callerPosition(thread)` use across
// every other builtin in this file.)
func findResultValueArg(fileBytes []byte, filename string, callPos syntax.Position) (syntax.Expr, error) {
	file, err := defaultFileOptions().Parse(filename, fileBytes, 0)
	if err != nil {
		return nil, fmt.Errorf("re-parse to recover result.value AST: %w", err)
	}
	var found *syntax.CallExpr
	syntax.Walk(file, func(n syntax.Node) bool {
		if found != nil {
			return false
		}
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return true
		}
		if positionsEqual(call.Lparen, callPos) {
			found = call
			return false
		}
		return true
	})
	if found == nil {
		return nil, fmt.Errorf("internal: cannot locate result(...) CallExpr at %s", callPos)
	}
	// Find the kwarg arg whose name is "value". CallExpr.Args is a
	// slice where named args are *BinaryExpr{Op:EQ, X:*Ident, Y:expr}.
	for _, arg := range found.Args {
		bin, ok := arg.(*syntax.BinaryExpr)
		if !ok || bin.Op != syntax.EQ {
			continue
		}
		id, ok := bin.X.(*syntax.Ident)
		if !ok || id.Name != "value" {
			continue
		}
		return bin.Y, nil
	}
	return nil, fmt.Errorf("internal: result(...) call has no value= kwarg in AST")
}

// =============================================================================
// builtinScript — script(id=..., fn=lambda ctx: ..., output_alias=...)
// =============================================================================

// skytime:doc summary="Pure state transformation evaluated INSIDE the workflow (zero Temporal history events)."
// skytime:doc summary="The fn lambda receives ctx and returns a dict that is bound to ctx.<output_alias> for downstream nodes."
// skytime:doc returns="A *dag.Script node."
// skytime:doc since="phase-01"
// skytime:doc example="script(\n    id = \"validate_${ctx.repo}\",\n    fn = lambda ctx: {\"valid\": ctx.repo != \"\"},\n    output_alias = \"validation\",\n)"
// skytime:doc see="if_cond, flow"
// skytime:doc param_id="string"
// skytime:doc desc_id="Logical identifier; supports ${ctx.expr} interpolation (D4.1-02)."
// skytime:doc param_fn="lambda(ctx) -> dict"
// skytime:doc desc_fn="Pure transformation; lambda-time globals only."
// skytime:doc param_output_alias="string"
// skytime:doc desc_output_alias="ctx attribute that receives the returned dict for downstream consumers."
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

// skytime:doc summary="Parallel fan-out with bounded concurrency (default 10; configurable via max_concurrency)."
// skytime:doc summary="items can be a static list literal OR a lambda producer that returns a list at workflow-execute time."
// skytime:doc returns="A *dag.ForEachParallel node."
// skytime:doc since="phase-01"
// skytime:doc example="for_each_parallel(\n    items = lambda ctx: ctx.repos,\n    item = \"repo\",\n    max_concurrency = 5,\n    steps = [step(action = http.get(path = \"/repos/${ctx.repo}\"))],\n)"
// skytime:doc see="step, call_flow, flow"
// skytime:doc param_items="list | lambda(ctx) -> list"
// skytime:doc desc_items="Static list literal or lambda producer; lambda evaluated once inside the workflow."
// skytime:doc param_item="string"
// skytime:doc desc_item="Per-iteration variable name added to ctx for the inner steps body."
// skytime:doc param_steps="list[Node]"
// skytime:doc desc_steps="Body executed once per item with ctx.<item> bound."
// skytime:doc param_retry="RetryPolicy"
// skytime:doc desc_retry="Optional inherited Temporal RetryPolicy."
// skytime:doc param_timeout="Timeout"
// skytime:doc desc_timeout="Optional inherited Temporal Timeout."
// skytime:doc param_max_concurrency="int"
// skytime:doc desc_max_concurrency="Concurrent worker.Go fan-out cap (D3-13); 0 = interpreter default 10."
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

// skytime:doc summary="Invoke a sub-flow as a Temporal child workflow (isolates sub-history)."
// skytime:doc returns="A *dag.CallFlow node."
// skytime:doc since="phase-01"
// skytime:doc example="call_flow(\n    name = \"audit_repo\",\n    inputs = {\"repo\": ctx.repo},\n    child_options = {\"workflow_id\": \"audit-${ctx.repo}\"},\n)"
// skytime:doc see="flow, step"
// skytime:doc param_name="string"
// skytime:doc desc_name="Target flow name; resolved at parse finalize against the parser session's flow map (D-16)."
// skytime:doc param_inputs="dict[string,any]"
// skytime:doc desc_inputs="Inputs passed to the child flow; merged with the child's declared inputs."
// skytime:doc param_child_options="dict[string,any]"
// skytime:doc desc_child_options="Pass-through Temporal child workflow options (workflow_id, retry policy, etc.)."
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

// =============================================================================
// builtinTrigger — trigger(flow=, source=, map=, idempotency_key=, credential=) → None
// =============================================================================

// skytime:doc summary="Declares a top-level trigger — binds a TriggerSource to a flow with map/idempotency_key lambdas."
// skytime:doc summary="Trigger lambdas run ONCE at HTTP ingress (Phase 7.1+), NOT in workflow replay; non-determinism (time.now, json) is observably safe."
// skytime:doc returns="None (registers a *dag.Trigger as a parse-time side effect)."
// skytime:doc since="phase-07"
// skytime:doc example="trigger(\n    flow=\"check_user\",\n    source=github.webhook(events=[\"push\"]),\n    map=lambda req: {\"repo\": req.payload.repository.name},\n    idempotency_key=lambda req: req.headers[\"X-GitHub-Delivery\"],\n    credential=\"github-app-prod\",\n)"
// skytime:doc see="flow"
// skytime:doc param_flow="string"
// skytime:doc desc_flow="Target flow name (must resolve in the same parser session — cross-file allowed; resolved at parse-finalize)."
// skytime:doc param_source="TriggerSource"
// skytime:doc desc_source="Sealed extension.TriggerSource value (e.g. github.webhook(...)). Phase 7 has no shipped factories; first one in 7.1."
// skytime:doc param_map="lambda req"
// skytime:doc desc_map="Single-positional lambda. Returns a dict — the workflow input. Free vars rejected (D-19); arity != 1 rejected; req.<field> typos surface valid-list."
// skytime:doc param_idempotency_key="lambda req"
// skytime:doc desc_idempotency_key="Single-positional lambda. Returns a string used as the idempotency key for ExecuteWorkflow's WorkflowID dedup."
// skytime:doc param_credential="string"
// skytime:doc desc_credential="Optional credential ID string. Resolved JIT inside the receiver (Phase 7.1+); never serialized to DAG JSON, never logged."
//
// builtinTrigger constructs a *dag.Trigger from kwargs, type-asserts source
// to extension.TriggerSource, captures map/idempotency_key lambdas with
// arity-1 enforcement, and registers the trigger in p.triggers keyed by
// position. Returns starlark.None — trigger() is a top-level statement,
// like flow(), captured by side effect.
//
// Cross-file FlowName resolution and req-attribute walks happen in
// finalize (D-07-12, D-07-05) — at builtin-call time the FlowName is
// stored as a literal string. Per-source ReqSchema() is queried at
// finalize, not here, because the source value's ReqSchema may depend on
// kwargs not yet validated at builtin time.
func (p *Parser) builtinTrigger(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		flowName       string
		sourceVal      starlark.Value
		mapVal         starlark.Value
		idempotencyVal starlark.Value
		credentialID   string
	)
	if err := starlark.UnpackArgs("trigger", args, kwargs,
		"flow", &flowName,
		"source", &sourceVal,
		"map", &mapVal,
		"idempotency_key", &idempotencyVal,
		"credential?", &credentialID,
	); err != nil {
		return nil, p.wrapBuiltinError("trigger", thread, err)
	}
	pos := callerPosition(thread)

	// Type-check Source. The dag-local TriggerSource is the minimal seal
	// (Kind + MarshalJSON); extension.TriggerSource adds ReqSchema and the
	// unexported triggerSourceMarker(). We assert against extension.TriggerSource
	// because the req-walker (finalize) needs ReqSchema().
	src, ok := sourceVal.(extension.TriggerSource)
	if !ok {
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: fmt.Sprintf("trigger.source: expected TriggerSource, got %s", sourceVal.Type()),
		}
	}

	// Capture lambdas with arity-1 enforcement (D-07-05 layer 2).
	mapLambda, err := p.captureLambdaWithArity(thread, "map", mapVal, 1)
	if err != nil {
		return nil, err
	}
	idempLambda, err := p.captureLambdaWithArity(thread, "idempotency_key", idempotencyVal, 1)
	if err != nil {
		return nil, err
	}

	trig := &dag.Trigger{
		Pos:               pos,
		FlowName:          flowName,
		Source:            src,
		MapLambda:         mapLambda,
		IdempotencyLambda: idempLambda,
		CredentialID:      credentialID,
	}

	// Register in parser session — keyed by Pos (unique per call site).
	// Reuses the existing posKey helper (originally defined for
	// preBuiltResults). Two triggers at the SAME position would be a
	// parser bug (lambda capture would already have collided);
	// builtinTrigger is called once per source-position by Starlark.
	key := posKey(pos)
	p.triggers[key] = trig

	return starlark.None, nil
}
