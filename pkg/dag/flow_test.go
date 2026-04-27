package dag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlow_Construction(t *testing.T) {
	pos := nodePos(t, 1, 1)
	f := &Flow{
		Pos:    pos,
		Name:   "approve_pr",
		Inputs: map[string]string{"repo_name": "string"},
		Body:   []Node{},
	}
	assert.Equal(t, "approve_pr", f.Name)
	assert.Equal(t, "string", f.Inputs["repo_name"])
	assert.Equal(t, "Flow", f.Kind())
	assert.Equal(t, pos, f.Position())
}

func TestFlow_BodyAcceptsHeterogeneousNodes(t *testing.T) {
	pos := nodePos(t, 1, 1)
	f := &Flow{
		Pos:  pos,
		Name: "f",
		Body: []Node{
			&Step{Pos: pos},
			&IfCond{Pos: pos},
			&Script{Pos: pos},
			&ForEachParallel{Pos: pos, ItemsLambdaID: "abc"},
			&CallFlow{Pos: pos, Name: "child"},
		},
	}
	require.Len(t, f.Body, 5)

	// Each entry's Kind() returns its own discriminator — type switch ergonomic check.
	assert.Equal(t, "Step", f.Body[0].Kind())
	assert.Equal(t, "IfCond", f.Body[1].Kind())
	assert.Equal(t, "Script", f.Body[2].Kind())
	assert.Equal(t, "ForEachParallel", f.Body[3].Kind())
	assert.Equal(t, "CallFlow", f.Body[4].Kind())
}
