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

// TestFail_TopLevel_NoArg: fail() with no positional argument is
// rejected. The error must point users at the correct shape.
func TestFail_TopLevel_NoArg(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[fail()])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fail")
}

// TestFail_TopLevel_TooManyArgs: fail("a", "b") rejects — only one
// positional message argument is accepted.
func TestFail_TopLevel_TooManyArgs(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[fail("a", "b")])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
}

// TestFail_TopLevel_NonStringArg: fail(42) rejects — the message must
// be a string. Starlark's UnpackPositionalArgs accepts the value as
// starlark.Value; our type-check rejects with a clean error.
func TestFail_TopLevel_NonStringArg(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[fail(42)])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "string")
}

// TestProceduralFailGuard: an if_cond WITHOUT output_alias (procedural
// mode) where one branch consists of a single fail() must parse cleanly.
// This is the procedural-guard pattern (D4.2-07): top-level fail() is
// allowed anywhere body nodes are accepted.
func TestProceduralFailGuard(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={"r": "string"}, steps=[
    if_cond(
        cond=lambda ctx: ctx.r == "",
        then=[fail("repo required")],
        else_=[script(id="ok", fn=lambda ctx: {"ok": True}, output_alias="o")],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "procedural-mode if_cond with fail()/script() branches must parse: %v", err)
}
