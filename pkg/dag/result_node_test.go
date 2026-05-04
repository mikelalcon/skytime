package dag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
)

// stringPtr is a per-test helper for syntax.MakePosition's *string filename
// arg. Lives here (instead of testhelpers_test.go) so package dag tests stay
// self-contained.
func stringPtr(s string) *string { return &s }

// TestResultNode_SealedNode confirms *dag.Result satisfies the sealed Node
// interface with the standard Kind / Position / nodeMarker shape used by
// every other node type (Step, IfCond, Script, ...).
func TestResultNode_SealedNode(t *testing.T) {
	fname := "test.star"
	pos := syntax.MakePosition(stringPtr(fname), 5, 13)
	r := &Result{
		Pos:    pos,
		Keys:   []string{"a", "b"},
		Values: nil,
		Types:  nil,
	}

	// Node interface satisfaction (compile-time + runtime).
	var n Node = r
	assert.Equal(t, "Result", n.Kind())
	assert.Equal(t, pos, n.Position())

	// nodeMarker is unexported; calling it directly proves we are in the
	// same package and the seal is consistent. External packages cannot
	// implement Node thanks to this method.
	r.nodeMarker()
}

// TestResultNode_KeysIsSlice verifies Keys preserves source insertion order
// — D4.2-03's replay-determinism contract (Pitfall 5: iterate Keys, not
// `for k := range Values`).
func TestResultNode_KeysIsSlice(t *testing.T) {
	r := &Result{Keys: []string{"alpha", "beta", "gamma"}}
	require.Len(t, r.Keys, 3)
	assert.Equal(t, "alpha", r.Keys[0])
	assert.Equal(t, "beta", r.Keys[1])
	assert.Equal(t, "gamma", r.Keys[2])
}
