package parser

import (
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
