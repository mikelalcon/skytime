package parser

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// =============================================================================
// D-19 (positive): module-level def is allowed (Pitfall #5 closure)
// =============================================================================

func TestFreeVars_ModuleLevelDefAllowed(t *testing.T) {
	src := []byte(`def helper(x):
    return x * 2

flow(name="x", inputs={}, steps=[
    script(id="s", fn=lambda ctx: helper(ctx.v), output_alias="r"),
])`)
	p, _ := NewParser(WithExtensions(&fakeExtension{}))
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err,
		"Pitfall #5: module-level def referenced from a lambda must parse cleanly")
}

// =============================================================================
// D-19 (positive): module-level constant is allowed
// =============================================================================

func TestFreeVars_ModuleConstAllowed(t *testing.T) {
	src := []byte(`MAX = 10

flow(name="x", inputs={}, steps=[
    if_cond(cond=lambda ctx: ctx.v < MAX, then=[step(action=fake_ext.echo(msg="t"))]),
])`)
	p, _ := NewParser(WithExtensions(&fakeExtension{}))
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err,
		"D-19: module-level constants are valid free vars")
}

// =============================================================================
// D-19 (negative): nested-scope capture rejected with position-aware error
// =============================================================================

func TestFreeVars_NestedDefRejected(t *testing.T) {
	// `make_lambda()`'s body declares `counter` as a local, then returns
	// a lambda that closes over it. The captured closure violates D-19:
	// `counter` is bound inside the def body (not at column 1).
	src := []byte(`def make_lambda():
    counter = [0]
    return lambda ctx: counter.append(len(counter)) or counter[-1]

flow(name="bad", inputs={}, steps=[
    script(id="s", fn=make_lambda(), output_alias="v"),
])`)
	p, _ := NewParser(WithExtensions(&fakeExtension{}))
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe))
	assert.Contains(t, pe.Error(), "lambda captures non-module-level variable",
		"D-19 violation must surface a clear error message")
	assert.Contains(t, pe.Error(), `"counter"`,
		"error must name the offending free variable")
}

// =============================================================================
// D-19 (negative): error position points at the binding (not the lambda)
// =============================================================================

func TestFreeVars_PositionPointsAtBinding(t *testing.T) {
	src := []byte(`def f():
    inner = 42
    return lambda ctx: ctx.x + inner

flow(name="x", inputs={}, steps=[script(id="s", fn=f(), output_alias="r")])`)
	p, _ := NewParser(WithExtensions(&fakeExtension{}))
	_, err := p.ParseSource("source.star", src)
	require.Error(t, err)

	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe))
	require.True(t, pe.Pos.IsValid(), "error position must be valid")

	// `inner = 42` is on line 2 (1-based). The error position must point
	// at the binding line, not the lambda line (line 3).
	assert.Equal(t, int32(2), pe.Pos.Line,
		"D-19 error must point at the binding site, not the lambda site")
}
