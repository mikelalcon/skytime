package parser

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// TestFinalize_NestedCallFlow verifies that resolveCallFlows walks recursively
// through IfCond.Then/Else and ForEachParallel.Steps to surface unresolved
// nested call_flow targets.
func TestFinalize_NestedCallFlow(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="other", inputs={}, steps=[step(action=fake_ext.echo(msg="o"))])
flow(name="parent", inputs={}, steps=[
    if_cond(
        cond = lambda ctx: True,
        then = [call_flow(name="other", inputs={})],
        else_ = [call_flow(name="missing_target", inputs={})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call_flow target not found")
	assert.Contains(t, err.Error(), "missing_target")
}

// TestFinalize_NestedForEachParallel verifies recursion into ForEachParallel.Steps.
func TestFinalize_NestedForEachParallel(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="parent", inputs={}, steps=[
    for_each_parallel(items=[1, 2], item="x", steps=[
        call_flow(name="missing_in_loop", inputs={}),
    ]),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing_in_loop")
}

// TestFinalize_DeepNestedResolution verifies positive case for nested flows.
func TestFinalize_DeepNestedResolution(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="leaf", inputs={}, steps=[step(action=fake_ext.echo(msg="leaf"))])
flow(name="parent", inputs={}, steps=[
    if_cond(
        cond = lambda ctx: True,
        then = [call_flow(name="leaf", inputs={})],
    ),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	parent := flows["parent"]
	ifc := parent.Body[0].(*dag.IfCond)
	cf := ifc.Then[0].(*dag.CallFlow)
	require.NotNil(t, cf.Resolved)
	assert.Equal(t, flows["leaf"], cf.Resolved)
}

// TestFinalize_LintOrder_CallFlowResolutionShortCircuits verifies the
// finalize() ordering: resolveCallFlows runs BEFORE the new Phase-2 lints,
// so a flow that has both an unresolved call_flow AND a mixed-idempotency
// block surfaces the call_flow error first (more useful to the consultant
// than a downstream lint they cannot fix until the call_flow target is
// added).
func TestFinalize_LintOrder_CallFlowResolutionShortCircuits(t *testing.T) {
	p := newTestParser(t)
	// "missing_target" doesn't exist; the same flow ALSO contains a
	// mixed-idempotency block. resolveCallFlows must report the
	// missing target first.
	src := []byte(`flow(name="bad", inputs={}, steps=[
    call_flow(name="missing_target", inputs={}),
    step(block=[
        fake_ext.echo(msg="ok"),
        fake_ext.post(payload="x"),
    ]),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	// The ParseError from resolveCallFlows wins; it does NOT carry the
	// mixed-idempotency phrase.
	assert.Contains(t, err.Error(), "call_flow target not found")
	assert.Contains(t, err.Error(), "missing_target")
	assert.NotContains(t, err.Error(), "cannot mix idempotent",
		"resolveCallFlows must short-circuit ahead of lintMixedIdempotency")
}

// TestValidateIfCondExpressionShape_FinalizeOrdering pins the D4.2-09
// ordering rule: validateLambdaCtxAccesses runs BEFORE
// validateIfCondExpressionShape so a ctx-typo error surfaces FIRST when
// a flow contains BOTH a typo AND an expression-mode shape error. This
// guarantees ctx-visibility errors (the more localized fix) win over
// branch-shape errors (the broader structural fix).
func TestValidateIfCondExpressionShape_FinalizeOrdering(t *testing.T) {
	p := newTestParser(t)
	// The else_ branch references `ctx.tyop` (typo of `tyop` — flow
	// declares no such input). Independently, the then-branch ends in a
	// step (not result/fail) which violates D4.2-09 case 2. The walker
	// must surface the ctx-typo error, NOT the branch-shape error.
	src := []byte(`flow(name="ord", inputs={"flag": "bool"}, steps=[
    if_cond(
        output_alias="X",
        cond=lambda ctx: ctx.flag,
        then=[step(action=fake_ext.echo(msg="x"))],
        else_=[result(value={"x": ctx.tyop})],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)

	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	assert.Contains(t, ve.Msg, "ctx.tyop",
		"validateLambdaCtxAccesses must run BEFORE validateIfCondExpressionShape")
	assert.Contains(t, ve.Msg, "not in declared state")
}

// TestProceduralFailGuard_StaysGreen pins the Pitfall 7 regression net:
// procedural-mode if_cond (no output_alias) using fail() in a branch is
// the canonical D4.2-07 procedural-guard pattern. The new
// validateIfCondExpressionShape pass MUST skip procedural-mode if_conds
// entirely. Parses the canonical fixture verbatim.
func TestProceduralFailGuard_StaysGreen(t *testing.T) {
	p := newTestParser(t)
	_, err := p.ParseFile("../../tests/fixtures/procedural_fail_guard.star")
	require.NoError(t, err, "procedural_fail_guard.star must parse cleanly: %v", err)
}

// =============================================================================
// Phase 07.2.1 Plan 02 Task 2: validateLogStepPlacement finalize pass
//
// First-of-its-kind module-scope orphan-node check. Mirrors
// validateResultPlacement (D4.2-04) but the placement contract is "must be a
// step inside flow(...)" rather than "must be the last node of an
// expression-mode if_cond branch".
// =============================================================================

// TestValidateLogStepPlacement_RejectsModuleScope verifies D-7.2.1-18:
// log.<level>(...) at module scope (i.e., not inside any flow body) produces a
// position-aware *dag.ParseError. Symmetric across all four levels.
func TestValidateLogStepPlacement_RejectsModuleScope(t *testing.T) {
	levels := []string{"info", "warn", "error", "debug"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			p := newTestParser(t)
			src := []byte(fmt.Sprintf(`log.%s("at module scope")`, level))
			_, err := p.ParseSource("test.star", src)
			require.Error(t, err)
			var pe *dag.ParseError
			require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
			assert.Contains(t, pe.Msg, fmt.Sprintf("log.%s: only valid as a step inside flow(...)", level))
			assert.True(t, pe.Pos.IsValid(), "ParseError must carry a valid position")
		})
	}
}

// TestValidateLogStepPlacement_AcceptsInsideFlow sanity-checks that a log call
// directly inside a flow body parses cleanly (no false positive from the
// orphan check).
func TestValidateLogStepPlacement_AcceptsInsideFlow(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[log.info("hi")])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
}

// TestValidateLogStepPlacement_AcceptsInsideIfCondBranch verifies recursive
// claim-walking: log calls inside if_cond Then/Else branches are reached by
// walkBodyForLogSteps and not flagged as orphans.
func TestValidateLogStepPlacement_AcceptsInsideIfCondBranch(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={"y": "bool"}, steps=[
  if_cond(
    cond=lambda ctx: ctx.y,
    then=[log.info("yes")],
    else_=[log.warn("no")],
  ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
}

// TestValidateLogStepPlacement_AcceptsInsideForEachParallel verifies recursion
// into ForEachParallel.Steps — a log inside a fan-out body is reached and not
// flagged.
func TestValidateLogStepPlacement_AcceptsInsideForEachParallel(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
  for_each_parallel(items=["a","b"], item="it", steps=[
    log.info("processing"),
  ]),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
}
