package parser

// Wave 0 RED scaffolding for D4.2-15 top-level fail() builtin (plan 02
// fills). These tests exercise the parser-time surface of the new
// node-level `fail()` builtin (distinct from the lambda-time fail()
// already exposed via lambdaTimeGlobals — the dual-name overload lives
// in two different predeclared environments per pkg/parser/doc.go).

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// TestFail_TopLevel_EmitsDagFail: `fail("oops")` at the body level
// becomes a *dag.Fail{Message:"oops"} node. Today the parser does not
// register a top-level fail builtin; the test fails with "fail is not
// defined" or analogous until plan 02 lands.
func TestFail_TopLevel_EmitsDagFail(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    step(action=fake_ext.echo(msg="hi")),
    fail("oops"),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "RED until plan 02 ships top-level fail builtin: %v", err)

	body := flows["f"].Body
	require.Len(t, body, 2)
	failNode, ok := body[1].(*dag.Fail)
	require.True(t, ok, "second body node must be *dag.Fail; got %T", body[1])
	require.Equal(t, "oops", failNode.Message)
	require.Nil(t, failNode.MessageFn, "no ${...} interpolation present")
}

// TestFail_TopLevel_InterpolationDesugars: `fail("missing ${ctx.r}")`
// captures the literal template in Message AND populates MessageFn
// with a CapturedLambda built by the D4.1-01 desugarer. RED until
// plan 02 wires the desugar call into builtinFail.
func TestFail_TopLevel_InterpolationDesugars(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={"r": "string"}, steps=[
    fail("missing ${ctx.r}"),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "RED until plan 02 wires desugarInterpolation into builtinFail: %v", err)

	body := flows["f"].Body
	require.Len(t, body, 1)
	failNode, ok := body[0].(*dag.Fail)
	require.True(t, ok, "first body node must be *dag.Fail; got %T", body[0])
	require.Equal(t, "missing ${ctx.r}", failNode.Message,
		"literal template preserved verbatim")
	require.NotNil(t, failNode.MessageFn,
		"interpolation must produce a CapturedLambda")
}
