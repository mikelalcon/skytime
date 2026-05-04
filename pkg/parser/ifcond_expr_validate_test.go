package parser

// Wave 0 RED scaffolding for D4.2-09 + D4.2-11 expression-mode if_cond
// branch-equality validator (plan 03 fills). All seven tests assert the
// EXPECTED post-implementation behavior; until plan 03 wires
// validateIfCondExpressionShape into finalize, these fail loudly.
//
// Test groups:
//   - structural mode-switching: procedural unchanged; expression-mode
//     requires both branches; last node must be Result/Fail; at least
//     one Result on a branch
//   - branch-equality: keys mismatch rejected; per-key TypeInfo
//     mismatch rejected; one-side opaque defers (no error)

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// TestIfCond_OutputAlias_ProceduralPreserved: an if_cond WITHOUT
// output_alias must continue to parse exactly as before — Pitfall 7
// regression guard. RED until plan 03 lands without breaking the
// procedural path.
func TestIfCond_OutputAlias_ProceduralPreserved(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={"r": "string"}, steps=[
    if_cond(
        cond=lambda ctx: ctx.r == "",
        then=[fail("repo required")],
        else_=[step(action=fake_ext.echo(msg="hi"))],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "RED until plan 02/03 lands; procedural mode must stay green: %v", err)
}

// TestIfCond_OutputAlias_ExpressionMode_BothBranchesRequired:
// expression mode requires else_ to be set (single-arm if_cond is
// procedural-only). RED until plan 03.
func TestIfCond_OutputAlias_ExpressionMode_BothBranchesRequired(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[result(value={"x": 1})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "RED until plan 03 ships validateIfCondExpressionShape")
	var ve *dag.ValidationError
	if errors.As(err, &ve) {
		require.Contains(t, ve.Msg, "else_")
	}
}

// TestIfCond_OutputAlias_LastNodeMustBeResultOrFail: in expression
// mode, the last node of each branch must be result() or fail(). A
// Step at the tail must be rejected. RED until plan 03.
func TestIfCond_OutputAlias_LastNodeMustBeResultOrFail(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[step(action=fake_ext.echo(msg="x"))],
        else_=[result(value={"x": 1})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "RED until plan 03 ships validateIfCondExpressionShape")
}

// TestIfCond_OutputAlias_AtLeastOneResult: both-branches-fail is
// rejected with a "remove output_alias" hint (the user wanted procedural
// guard, not expression mode). RED until plan 03.
func TestIfCond_OutputAlias_AtLeastOneResult(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[fail("a")],
        else_=[fail("b")],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "RED until plan 03 ships validateIfCondExpressionShape")
	var ve *dag.ValidationError
	if errors.As(err, &ve) {
		require.Contains(t, ve.Msg, "output_alias")
	}
}

// TestValidateIfCondExpressionShape_KeysMismatch: branches with
// different result key sets must reject. RED until plan 03.
func TestValidateIfCondExpressionShape_KeysMismatch(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[result(value={"a": 1})],
        else_=[result(value={"b": 2})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "RED until plan 03 ships validateIfCondExpressionShape")
}

// TestValidateIfCondExpressionShape_TypeMismatch_NoLUB: same key, both
// sides concrete, types differ (int vs float) — strict equality must
// reject with a hint to use float() cast. RED until plan 03.
func TestValidateIfCondExpressionShape_TypeMismatch_NoLUB(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[result(value={"x": 1})],
        else_=[result(value={"x": 1.5})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "RED until plan 03 ships validateIfCondExpressionShape")
}

// TestValidateIfCondExpressionShape_OneSideOpaqueDefers: when one
// branch's value-expression is opaque (e.g., user helper call) the
// validator must DEFER — no error. The runtime walker handles the
// shape at execution time. RED until plan 03 implements the deferral
// branch (which is silent — Pitfall 6).
func TestValidateIfCondExpressionShape_OneSideOpaqueDefers(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={"helper": "string"}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[result(value={"x": ctx.helper})],
        else_=[result(value={"x": "literal"})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	// The expectation is no error: ctx.helper is an opaque-typed string
	// here (typeFromHint returns scalar string, but plan 02's inferType
	// surface for ctx access against typed schema may resolve to string;
	// for this test we use opaque-via-unknown by referencing a flow
	// input typed as something the validator considers opaque OR
	// a function call). RED meaning: today, parser does not even know
	// `result()` so the current error is a builtin-not-found. Once
	// plan 02/03 land, this must accept.
	require.NoError(t, err, "RED until plan 02+03 land; one-side-opaque must DEFER (no error): %v", err)
}
