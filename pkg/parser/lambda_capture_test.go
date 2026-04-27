package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// TestLambdaCapture_StableID pins PARSE-04 / D-18: parsing the same source
// twice (in distinct parser sessions) yields byte-identical lambda IDs.
func TestLambdaCapture_StableID(t *testing.T) {
	src := []byte(`flow(name="x", inputs={}, steps=[
    script(id="s", fn=lambda ctx: ctx, output_alias="r"),
])`)

	p1, err := NewParser(WithExtensions(&fakeExtension{}))
	require.NoError(t, err)
	flows1, err := p1.ParseSource("test.star", src)
	require.NoError(t, err)
	scr1 := flows1["x"].Body[0].(*dag.Script)

	p2, err := NewParser(WithExtensions(&fakeExtension{}))
	require.NoError(t, err)
	flows2, err := p2.ParseSource("test.star", src)
	require.NoError(t, err)
	scr2 := flows2["x"].Body[0].(*dag.Script)

	assert.Equal(t, scr1.LambdaID, scr2.LambdaID,
		"D-18: same fileBytes + same position → same LambdaID across parser sessions")
}

// TestLambdaCapture_ContentSensitive: changing the file content (even a
// comment) flips the hash prefix.
func TestLambdaCapture_ContentSensitive(t *testing.T) {
	src1 := []byte(`flow(name="x", inputs={}, steps=[script(id="s", fn=lambda ctx: ctx, output_alias="r")])`)
	src2 := []byte(`# extra comment
flow(name="x", inputs={}, steps=[script(id="s", fn=lambda ctx: ctx, output_alias="r")])`)

	p1, _ := NewParser(WithExtensions(&fakeExtension{}))
	flows1, err := p1.ParseSource("test.star", src1)
	require.NoError(t, err)

	p2, _ := NewParser(WithExtensions(&fakeExtension{}))
	flows2, err := p2.ParseSource("test.star", src2)
	require.NoError(t, err)

	id1 := flows1["x"].Body[0].(*dag.Script).LambdaID
	id2 := flows2["x"].Body[0].(*dag.Script).LambdaID

	assert.NotEqual(t, id1, id2,
		"D-18 says cosmetic edits change ID — comment addition must flip the hash prefix")
}

// TestLambdaCapture_PositionMatchesDef: the captured lambda's Pos points
// at the lambda keyword, not the surrounding builtin call site.
func TestLambdaCapture_PositionMatchesDef(t *testing.T) {
	src := []byte(`flow(name="x", inputs={}, steps=[
    script(
        id="s",
        fn=lambda ctx: ctx,
        output_alias="r",
    ),
])`)
	p, _ := NewParser(WithExtensions(&fakeExtension{}))
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	scr := flows["x"].Body[0].(*dag.Script)
	captured, ok := p.lambdas[scr.LambdaID]
	require.True(t, ok)

	// "fn=lambda" lives on line 4, column starts at the `lambda` keyword.
	assert.Equal(t, "test.star", captured.Pos.Filename())
	assert.Equal(t, int32(4), captured.Pos.Line, "captured.Pos.Line must point at the `lambda` keyword line")
}

// TestLambdaCapture_RejectsNonFunctionKwarg: passing a non-function value
// to a kwarg expecting a lambda surfaces a clear error.
func TestLambdaCapture_RejectsNonFunctionKwarg(t *testing.T) {
	src := []byte(`flow(name="x", inputs={}, steps=[
    if_cond(cond="not a lambda", then=[step(action=fake_ext.echo(msg="t"))]),
])`)
	p, _ := NewParser(WithExtensions(&fakeExtension{}))
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a lambda or function")
}
