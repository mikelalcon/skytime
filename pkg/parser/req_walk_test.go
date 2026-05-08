package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
)

// TestFindFreeVarAccesses_CtxName exercises the generalized signature
// against the existing ctx-style fixture: `lambda ctx: ctx.foo + ctx.bar`.
// Equivalent to TestCtxWalk_* but routed through the new freeVarName
// parameter.
func TestFindFreeVarAccesses_CtxName(t *testing.T) {
	src := []byte("_x = lambda ctx: ctx.foo + ctx.bar\n")
	filename := "ctx_lambda.star"
	opts := defaultFileOptions()
	file, err := opts.Parse(filename, src, 0)
	require.NoError(t, err)

	var lambdaPos syntax.Position
	syntax.Walk(file, func(n syntax.Node) bool {
		if le, ok := n.(*syntax.LambdaExpr); ok {
			lambdaPos = le.Lambda
		}
		return true
	})
	require.True(t, lambdaPos.IsValid(), "lambda position must be valid")

	accesses, err := findFreeVarAccesses(src, filename, lambdaPos, "ctx")
	require.NoError(t, err)
	require.Len(t, accesses, 2)
	names := []string{accesses[0].AttrName, accesses[1].AttrName}
	assert.ElementsMatch(t, []string{"foo", "bar"}, names)
}

// TestFindFreeVarAccesses_ReqName exercises the trigger-style convention:
// `lambda req: req.payload + req.headers["X"]` walked with freeVarName="req".
func TestFindFreeVarAccesses_ReqName(t *testing.T) {
	src := []byte("_x = lambda req: req.payload + req.headers\n")
	filename := "req_lambda.star"
	opts := defaultFileOptions()
	file, err := opts.Parse(filename, src, 0)
	require.NoError(t, err)

	var lambdaPos syntax.Position
	syntax.Walk(file, func(n syntax.Node) bool {
		if le, ok := n.(*syntax.LambdaExpr); ok {
			lambdaPos = le.Lambda
		}
		return true
	})
	require.True(t, lambdaPos.IsValid(), "lambda position must be valid")

	accesses, err := findFreeVarAccesses(src, filename, lambdaPos, "req")
	require.NoError(t, err)
	require.Len(t, accesses, 2)
	names := []string{accesses[0].AttrName, accesses[1].AttrName}
	assert.ElementsMatch(t, []string{"payload", "headers"}, names)
}

// TestFindFreeVarAccesses_WrongName confirms the visitor does NOT enforce
// that the lambda's first param actually IS named freeVarName. Called with
// a name the body does not use, it returns zero accesses (the validator's
// job to reject typos, not the visitor's).
func TestFindFreeVarAccesses_WrongName(t *testing.T) {
	src := []byte("_x = lambda req: req.payload\n")
	filename := "req_lambda.star"
	opts := defaultFileOptions()
	file, err := opts.Parse(filename, src, 0)
	require.NoError(t, err)

	var lambdaPos syntax.Position
	syntax.Walk(file, func(n syntax.Node) bool {
		if le, ok := n.(*syntax.LambdaExpr); ok {
			lambdaPos = le.Lambda
		}
		return true
	})
	require.True(t, lambdaPos.IsValid(), "lambda position must be valid")

	accesses, err := findFreeVarAccesses(src, filename, lambdaPos, "ctx")
	require.NoError(t, err)
	assert.Len(t, accesses, 0,
		"freeVarName mismatch must produce zero accesses (visitor is name-aware, not name-enforcing)")
}

// TestFindFreeVarAccesses_NoMatchingLambda passes a position that points
// inside the file but at no lambda. Defensive: returns (nil, nil), no
// error.
func TestFindFreeVarAccesses_NoMatchingLambda(t *testing.T) {
	src := []byte("_x = 1\n_y = lambda ctx: ctx.foo\n")
	filename := "no_match.star"
	// Synthesize a position that won't match any lambda in the file —
	// line 1 col 1 (which is the `_x = 1` assignment, not a lambda).
	bogus := syntax.MakePosition(&filename, 1, 1)

	accesses, err := findFreeVarAccesses(src, filename, bogus, "ctx")
	require.NoError(t, err)
	assert.Nil(t, accesses, "no matching lambda → (nil, nil)")
}
