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
