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

// TestValidateIfCondExpressionShape_BothOpaqueDefers: BOTH branches
// reference opaque values (different unknown helpers). Both per-key
// types are Opaque so the validator defers — no error.
func TestValidateIfCondExpressionShape_BothOpaqueDefers(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`def helper(n):
    return n
def other(m):
    return m
flow(name="f", inputs={}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[result(value={"x": helper(1)})],
        else_=[result(value={"x": other(2)})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "both-opaque must DEFER (no error): %v", err)
}

// TestValidateIfCondExpressionShape_TypeMatch: happy path — both
// branches concrete, same type → no error.
func TestValidateIfCondExpressionShape_TypeMatch(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[result(value={"x": 1})],
        else_=[result(value={"x": 2})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "matching int types must accept: %v", err)
}

// TestValidateIfCondExpressionShape_NestedDictRecursion: nested
// dict literals with identical structure across branches must accept
// (TypeDict.Equal recurses).
func TestValidateIfCondExpressionShape_NestedDictRecursion(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[result(value={"x": {"a": 1, "b": "y"}})],
        else_=[result(value={"x": {"a": 1, "b": "z"}})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "matching nested dict types must accept: %v", err)
}

// TestValidateIfCondExpressionShape_NestedDictMismatch: nested dict
// literals where one inner key's type differs across branches (string
// vs int for "b") must reject with the outer key cited in the error.
func TestValidateIfCondExpressionShape_NestedDictMismatch(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[result(value={"x": {"a": 1, "b": "y"}})],
        else_=[result(value={"x": {"a": 1, "b": 1}})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	require.Contains(t, ve.Msg, `key "x"`,
		"error must cite the outer key x whose nested types differ")
	require.Contains(t, ve.Msg, "branches disagree")
}

// TestValidateIfCondExpressionShape_RespectsBranchSchema: per-branch
// state schema flows from flow.Inputs + script outputs upstream of the
// if_cond. Script output is added via addUntyped → both branches see
// ctx.m as TypeOpaque so per-key inference for `k: ctx.m` is Opaque,
// validator defers → no error.
func TestValidateIfCondExpressionShape_RespectsBranchSchema(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={"n": "int"}, steps=[
    script(id="x2", fn=lambda ctx: ctx.n * 2, output_alias="m"),
    if_cond(
        output_alias="O",
        cond=lambda ctx: ctx.m > 10,
        then=[result(value={"k": ctx.m})],
        else_=[result(value={"k": 0})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	// then-branch: ctx.m is opaque (script output untyped) → defers.
	// else-branch: 0 is int. One-side opaque → no error.
	require.NoError(t, err, "branch schema must include script output (untyped → defer): %v", err)
}

// TestResultOutsideIfCond_Rejected: top-level result() (not wrapped
// in if_cond) must reject with the placement-gate hint. The plan-02
// validateResultPlacement gate fires first; this test verifies the
// orphan path is wired and the message points at output_alias.
func TestResultOutsideIfCond_Rejected(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="orphan", inputs={}, steps=[
    result(value={"x": 1}),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "output_alias",
		"orphan result must hint at output_alias")
}

// TestResultInProceduralBranch_Rejected: result() in a procedural-mode
// (no output_alias) if_cond branch must reject. The placement gate
// from plan 02 catches this; the new validator's walkValidateIfCondExpression
// also rejects via the orphan-Result case when descending procedural
// branches.
func TestResultInProceduralBranch_Rejected(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(
        cond=lambda ctx: True,
        then=[result(value={"x": 1})],
        else_=[step(action=fake_ext.echo(msg="hi"))],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "output_alias",
		"result in procedural branch must hint at output_alias")
}

// TestResultInLeadingBody_Rejected: result() in a leading position
// (not the LAST node) of an expression-mode branch must reject. Only
// the last node may be a result terminator.
func TestResultInLeadingBody_Rejected(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: True,
        then=[result(value={"x": 1}), result(value={"x": 2})],
        else_=[result(value={"x": 3})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "leading-position result must reject")
}

// TestValidateIfCondExpressionShape_FixturesReject parses each of the
// 4 invalid fixtures from plan 01 and asserts each rejects. The fixtures
// are the canonical reject-corpus for the validator.
func TestValidateIfCondExpressionShape_FixturesReject(t *testing.T) {
	cases := []struct {
		name     string
		fixture  string
		mustHave string
	}{
		{
			name:     "invalid_keys",
			fixture:  "../../tests/fixtures/ifcond_expr_invalid_keys.star",
			mustHave: "result keys",
		},
		{
			name:     "invalid_types",
			fixture:  "../../tests/fixtures/ifcond_expr_invalid_types.star",
			mustHave: "branches disagree on key",
		},
		{
			name:     "invalid_lastnode",
			fixture:  "../../tests/fixtures/ifcond_expr_invalid_lastnode.star",
			mustHave: "last node must be result",
		},
		{
			name:     "invalid_bothfail",
			fixture:  "../../tests/fixtures/ifcond_expr_invalid_bothfail.star",
			mustHave: "at least one branch must end in result",
		},
		{
			name:     "result_outside_ifcond",
			fixture:  "../../tests/fixtures/result_outside_ifcond.star",
			mustHave: "output_alias",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newTestParser(t)
			_, err := p.ParseFile(c.fixture)
			require.Error(t, err, "fixture %s must reject", c.fixture)
			require.Contains(t, err.Error(), c.mustHave,
				"fixture %s error must contain %q; got %v", c.fixture, c.mustHave, err)
		})
	}
}

// TestValidateIfCondExpressionShape_FixtureValid: the happy-path
// fixture (matching keys + matching types) parses cleanly.
func TestValidateIfCondExpressionShape_FixtureValid(t *testing.T) {
	p := newTestParser(t)
	flows, err := p.ParseFile("../../tests/fixtures/ifcond_expr_valid.star")
	require.NoError(t, err, "ifcond_expr_valid.star must parse cleanly: %v", err)
	require.Contains(t, flows, "happy")
}

// TestExpressionMode_OutputAliasInPostBranchSchema (D4.2-13): the
// alias bound by an expression-mode if_cond must be visible to
// downstream nodes — a sibling script's lambda referencing
// ctx.<alias> must NOT trigger a "ctx.<alias> not in declared
// state" ValidationError. Pinned by examples/skeleton/expression_if.star's
// classify_repo_size flow, but exercised here in isolation so a
// regression in walkBodyForCtxValidation surfaces at unit-test
// granularity (not only at TestDifferentialCorpus).
func TestExpressionMode_OutputAliasInPostBranchSchema(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={"n": "int"}, steps=[
    if_cond(
        output_alias="cls",
        cond=lambda ctx: ctx.n > 0,
        then=[result(value={"tier": "pos"})],
        else_=[result(value={"tier": "nonpos"})],
    ),
    script(id="audit", fn=lambda ctx: {"out": ctx.cls}, output_alias="logged"),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "downstream lambda must see ctx.cls in post-branch schema: %v", err)
}

// TestExpressionMode_OutputAliasInPostBranchSchema_FailFlowAlsoVisible:
// the alias is added unconditionally after the if_cond — the static
// schema cannot know whether the runtime branch was the result-side
// or the fail-side. A downstream reader of ctx.<alias> is statically
// legal (the fail path raises before the read at runtime).
func TestExpressionMode_OutputAliasInPostBranchSchema_FailFlowAlsoVisible(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={"id": "string"}, steps=[
    if_cond(
        output_alias="user",
        cond=lambda ctx: ctx.id != "",
        then=[result(value={"id": "x"})],
        else_=[fail("id required")],
    ),
    script(id="audit", fn=lambda ctx: {"out": ctx.user}, output_alias="logged"),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "downstream lambda must see ctx.user in post-branch schema even with asymmetric branches: %v", err)
}
