package dag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.starlark.net/syntax"
)

// TestFailNode_SealedNode confirms *dag.Fail satisfies the sealed Node
// interface.
func TestFailNode_SealedNode(t *testing.T) {
	fname := "f.star"
	pos := syntax.MakePosition(stringPtr(fname), 7, 1)
	f := &Fail{Pos: pos, Message: "bad"}

	var n Node = f
	assert.Equal(t, "Fail", n.Kind())
	assert.Equal(t, pos, n.Position())

	// Seal access (same-package call ensures the method exists).
	f.nodeMarker()
}

// TestFailNode_MessageFnOptional verifies both the literal-only and
// interpolation-bearing Fail shapes are valid; Message is exposed
// verbatim in both.
func TestFailNode_MessageFnOptional(t *testing.T) {
	fname := "f.star"
	pos := syntax.MakePosition(stringPtr(fname), 1, 1)

	literal := &Fail{Pos: pos, Message: "x"}
	assert.Equal(t, "x", literal.Message)
	assert.Nil(t, literal.MessageFn)

	withLambda := &Fail{
		Pos:       pos,
		Message:   "x ${ctx.y}",
		MessageFn: &CapturedLambda{ID: "deadbeef:1:1"},
	}
	assert.Equal(t, "x ${ctx.y}", withLambda.Message)
	assert.NotNil(t, withLambda.MessageFn)
	assert.Equal(t, "deadbeef:1:1", withLambda.MessageFn.ID)
}
