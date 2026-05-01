package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
)

// TestCtxWalk_FindsAttrAccesses covers the common shapes: a def with multiple
// ctx.<attr> accesses, and a lambda with one. The visitor must find both
// regardless of whether the matched node is a *syntax.LambdaExpr or
// *syntax.DefStmt.
func TestCtxWalk_FindsAttrAccesses(t *testing.T) {
	src := []byte(`
def helper(ctx):
    return ctx.alpha + ctx.beta

_x = lambda ctx: ctx.gamma
`)
	filename := "test.star"
	opts := defaultFileOptions()
	file, err := opts.Parse(filename, src, 0)
	require.NoError(t, err)

	// Walk to discover the def-position and lambda-position so the test does
	// not hard-code line/col coordinates that would shift with whitespace.
	var defPos, lambdaPos syntax.Position
	syntax.Walk(file, func(n syntax.Node) bool {
		switch fn := n.(type) {
		case *syntax.DefStmt:
			defPos = fn.Def
		case *syntax.LambdaExpr:
			lambdaPos = fn.Lambda
		}
		return true
	})
	require.True(t, defPos.IsValid(), "def position must be valid")
	require.True(t, lambdaPos.IsValid(), "lambda position must be valid")

	// The def has two ctx.<attr> accesses (alpha, beta).
	defAccesses, err := findCtxAccesses(src, filename, defPos)
	require.NoError(t, err)
	require.Len(t, defAccesses, 2)
	names := []string{defAccesses[0].AttrName, defAccesses[1].AttrName}
	require.ElementsMatch(t, []string{"alpha", "beta"}, names)

	// The lambda has one (gamma).
	lamAccesses, err := findCtxAccesses(src, filename, lambdaPos)
	require.NoError(t, err)
	require.Len(t, lamAccesses, 1)
	require.Equal(t, "gamma", lamAccesses[0].AttrName)
}

// TestCtxWalk_TwoLambdasSameLine covers Pitfall #1: two lambdas at the same
// line but different columns must be distinguishable. The position match
// uses (Filename, Line, Col) so the two lambdas key off different Col values.
func TestCtxWalk_TwoLambdasSameLine(t *testing.T) {
	src := []byte("_x = lambda ctx: ctx.left if True else (lambda c: c.right)(0)\n")
	filename := "twolambdas.star"
	opts := defaultFileOptions()
	file, err := opts.Parse(filename, src, 0)
	require.NoError(t, err)

	var positions []syntax.Position
	syntax.Walk(file, func(n syntax.Node) bool {
		if le, ok := n.(*syntax.LambdaExpr); ok {
			positions = append(positions, le.Lambda)
		}
		return true
	})
	require.Len(t, positions, 2, "expected two LambdaExpr nodes on the same line")
	require.Equal(t, positions[0].Line, positions[1].Line, "both lambdas must share a line")
	require.NotEqual(t, positions[0].Col, positions[1].Col, "the two lambdas must differ by column")

	// First lambda's first param is `ctx` and accesses `.left`.
	firstAccesses, err := findCtxAccesses(src, filename, positions[0])
	require.NoError(t, err)
	require.Len(t, firstAccesses, 1)
	require.Equal(t, "left", firstAccesses[0].AttrName)

	// Second lambda's first param is `c` and accesses `.right`. The visitor
	// reads the first-param name dynamically so the match works regardless
	// of the conventional `ctx` name.
	secondAccesses, err := findCtxAccesses(src, filename, positions[1])
	require.NoError(t, err)
	require.Len(t, secondAccesses, 1)
	require.Equal(t, "right", secondAccesses[0].AttrName)
}

// TestCtxWalk_NestedAttrCollectsTopOnly covers nested attribute access:
// `ctx.req.repo_name.length` should yield only `req` because state-schema
// names are top-level keys per D4-02. Deeper attrs (`repo_name`, `length`)
// are the lambda's data-shape concern, not the validator's.
func TestCtxWalk_NestedAttrCollectsTopOnly(t *testing.T) {
	src := []byte("_y = lambda ctx: ctx.req.repo_name.length\n")
	filename := "nested.star"
	opts := defaultFileOptions()
	file, err := opts.Parse(filename, src, 0)
	require.NoError(t, err)

	var pos syntax.Position
	syntax.Walk(file, func(n syntax.Node) bool {
		if le, ok := n.(*syntax.LambdaExpr); ok {
			pos = le.Lambda
		}
		return true
	})
	require.True(t, pos.IsValid())

	accesses, err := findCtxAccesses(src, filename, pos)
	require.NoError(t, err)
	// ONLY "req" is collected — "repo_name" and "length" are deeper attrs
	// on the chain (D4-02 only checks the top-level state-schema name).
	require.Len(t, accesses, 1)
	require.Equal(t, "req", accesses[0].AttrName)
}

// TestCtxWalk_LambdaNotFound returns empty + nil when lambdaPos does not
// match anything in the file. Defensive: callers treat absent lambdas as
// "nothing to check" — a missing lambda would have errored earlier in the
// finalize chain via lambda capture.
func TestCtxWalk_LambdaNotFound(t *testing.T) {
	src := []byte("_z = lambda ctx: ctx.foo\n")
	bogus := syntax.MakePosition(stringPtr("test.star"), 99, 99)
	accesses, err := findCtxAccesses(src, "test.star", bogus)
	require.NoError(t, err)
	require.Empty(t, accesses)
}
