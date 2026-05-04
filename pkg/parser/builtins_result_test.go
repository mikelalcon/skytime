package parser

// Wave 0 RED scaffolding for D4.2-15 result() builtin (plan 02 fills).
// These tests exercise the parser-time surface of the new `result()`
// builtin used inside expression-mode if_cond branches:
//
//   - kwarg-only signature (`result(value={...})`)
//   - dict keys must be string literals at parse time (computed keys reject)
//   - cannot appear outside an if_cond whose output_alias is set
//
// Until plan 02 ships builtinResult these tests fail with "result is not
// in parse-time globals" or analogous wording — that RED state is the
// signal each test will turn GREEN against once plan 02 lands.

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// TestResult_KwargOnlyValueDictLiteral: result(value={"x": 1}) inside an
// expression-mode if_cond branch parses cleanly. RED until plan 02
// registers the result builtin.
func TestResult_KwargOnlyValueDictLiteral(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(output_alias="X", cond=lambda ctx: True,
        then=[result(value={"x": 1})],
        else_=[fail("nope")],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "RED until plan 02 ships builtinResult: %v", err)
}

// TestResult_StringLiteralKeysOnly: result(value={ctx.k: 1}) — computed
// key — must reject at parse time with the "result.value: dict keys must
// be string literals" message. RED until plan 02.
func TestResult_StringLiteralKeysOnly(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={"k": "string"}, steps=[
    if_cond(output_alias="X", cond=lambda ctx: True,
        then=[result(value={ctx.k: 1})],
        else_=[fail("nope")],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "RED until plan 02 ships builtinResult")
	require.Contains(t, err.Error(), "result.value: dict keys must be string literals")
}

// TestResult_RejectedOutsideExpressionMode: a top-level result() (or
// inside a procedural-mode if_cond, i.e., output_alias unset) is rejected
// at parse time. The error must mention `result` and the `output_alias`
// requirement so users know the fix. RED until plan 02.
func TestResult_RejectedOutsideExpressionMode(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[result(value={"x": 1})])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "RED until plan 02 ships builtinResult")
	require.Contains(t, err.Error(), "result")
	require.Contains(t, err.Error(), "output_alias")
}

// TestResult_NotDictLiteral_Variable: result(value=my_var) where my_var
// is a variable reference must reject with the dict-literal hint. The
// AST shape is a *syntax.Ident, not a *syntax.DictExpr.
func TestResult_NotDictLiteral_Variable(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`my_var = {"x": 1}
flow(name="f", inputs={}, steps=[result(value=my_var)])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "result.value must be a dict literal")
}

// TestResult_NotDictLiteral_LambdaArg: result(value=lambda ctx: {})
// must reject — lambdas are explicitly forbidden as the value= shape.
func TestResult_NotDictLiteral_LambdaArg(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[result(value=lambda ctx: {})])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "result.value must be a dict literal")
}

// TestResult_NotDictLiteral_CallArg: result(value=foo()) must reject —
// the value= shape is a CallExpr, not a DictExpr.
func TestResult_NotDictLiteral_CallArg(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`def foo():
    return {"x": 1}
flow(name="f", inputs={}, steps=[result(value=foo())])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "result.value must be a dict literal")
}

// TestResult_PerKeyValueLambdaCaptured: each dict-entry value-expression
// is captured as a per-key *dag.CapturedLambda; BodyPos points at the
// synthetic <result:...> file; Pos points at the user-source value
// expression. Verifies the synthesized-source desugaring path works.
func TestResult_PerKeyValueLambdaCaptured(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={"n": "int"}, steps=[
    if_cond(output_alias="X", cond=lambda ctx: True,
        then=[result(value={"a": ctx.n + 1})],
        else_=[result(value={"a": 0})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	// The branch-equality validator (plan 03) is not yet wired, so the
	// finalize chain accepts this; today it parses cleanly and we can
	// inspect the *dag.Result. RED until plan 02 ships builtinResult.
	require.NoError(t, err, "result(value={...}) should parse: %v", err)

	// Locate the *dag.Result inside the if_cond's then branch.
	flow := p.flows["f"]
	require.NotNil(t, flow)
	ifCond, ok := flow.Body[0].(*dag.IfCond)
	require.True(t, ok)
	require.Len(t, ifCond.Then, 1)
	res, ok := ifCond.Then[0].(*dag.Result)
	require.True(t, ok, "then last node must be *dag.Result")
	require.Equal(t, []string{"a"}, res.Keys)
	require.NotNil(t, res.Values["a"])
	require.NotNil(t, res.Values["a"].Fn,
		"per-key value lambda must have a non-nil *starlark.Function")

	// BodyPos must point at the <result:...> synthetic file (the AST
	// re-parse target for D4-02 validation); Pos must point at the
	// user-source value-expression start.
	captured := res.Values["a"]
	require.True(t, captured.BodyPos.IsValid(),
		"per-key result-value lambda must have BodyPos set")
	require.Contains(t, captured.BodyPos.Filename(), "<result:",
		"BodyPos.Filename must be the synthetic <result:...> file")
	require.Equal(t, "test.star", captured.Pos.Filename(),
		"Pos.Filename must be the USER source file")
}

// TestResult_KeysInsertionOrder: source dict-literal key order is
// preserved on Result.Keys verbatim — replay determinism (D3-23 +
// Pitfall 5). Iterating Result.Values via `for k := range` would be
// non-deterministic; the test asserts the Keys slice carries the order.
func TestResult_KeysInsertionOrder(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(output_alias="X", cond=lambda ctx: True,
        then=[result(value={"c": 1, "a": 2, "b": 3})],
        else_=[result(value={"c": 1, "a": 2, "b": 3})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	flow := p.flows["f"]
	ifCond, _ := flow.Body[0].(*dag.IfCond)
	res, _ := ifCond.Then[0].(*dag.Result)
	require.Equal(t, []string{"c", "a", "b"}, res.Keys,
		"Keys must reflect source insertion order, NOT alphabetical")
}

// TestResult_TypesInferredFromValueExpr: per-key Types map is populated
// from inferType walking the user-source AST expression. Literal
// scalars are typed exactly; opaque expressions collapse to TypeOpaque.
func TestResult_TypesInferredFromValueExpr(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(output_alias="X", cond=lambda ctx: True,
        then=[result(value={"x": 1, "y": "hello", "z": True})],
        else_=[result(value={"x": 1, "y": "hello", "z": True})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	flow := p.flows["f"]
	ifCond, _ := flow.Body[0].(*dag.IfCond)
	res, _ := ifCond.Then[0].(*dag.Result)
	require.Len(t, res.Types, 3)
	require.True(t, Equal(res.Types["x"].(TypeInfo), TypeScalar{Kind: "int"}),
		"x → int, got %v", res.Types["x"])
	require.True(t, Equal(res.Types["y"].(TypeInfo), TypeScalar{Kind: "string"}),
		"y → string, got %v", res.Types["y"])
	require.True(t, Equal(res.Types["z"].(TypeInfo), TypeScalar{Kind: "bool"}),
		"z → bool, got %v", res.Types["z"])
}

// TestResult_OpaqueValueExprDefersToOpaque: an unknown call (user
// helper, <ext>.<op>) collapses to TypeOpaque so the branch-equality
// validator (plan 03) defers cleanly.
func TestResult_OpaqueValueExprDefersToOpaque(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`def helper(x):
    return x
flow(name="f", inputs={"n": "int"}, steps=[
    if_cond(output_alias="X", cond=lambda ctx: True,
        then=[result(value={"x": helper(1)})],
        else_=[result(value={"x": helper(1)})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	flow := p.flows["f"]
	ifCond, _ := flow.Body[0].(*dag.IfCond)
	res, _ := ifCond.Then[0].(*dag.Result)
	require.True(t, Equal(res.Types["x"].(TypeInfo), TypeOpaque{}),
		"unknown helper(...) must collapse to TypeOpaque; got %v", res.Types["x"])
}

// TestCheckLambdaCtx_RemapsResultPrefix: a per-key result-value lambda
// containing `ctx.<typo>` produces a *dag.ValidationError whose Pos
// does NOT leak the synthetic <result:...> filename. The position
// remaps to the user-source value-expression position (Pitfall 3).
func TestCheckLambdaCtx_RemapsResultPrefix(t *testing.T) {
	p := newTestParser(t)
	// `ctx.tyop` is a typo; `n` is declared but `tyop` isn't.
	src := []byte(`flow(name="f", inputs={"n": "int"}, steps=[
    if_cond(output_alias="X", cond=lambda ctx: True,
        then=[result(value={"a": ctx.tyop})],
        else_=[result(value={"a": 0})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "ctx.tyop must reject")
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve),
		"error must be *dag.ValidationError; got %T: %v", err, err)
	require.Equal(t, "test.star", ve.Pos.Filename(),
		"Pos.Filename must remap to user source, NOT <result:...>; got %q", ve.Pos.Filename())
	require.Contains(t, ve.Msg, "tyop",
		"error message must name the offending attribute")
}
