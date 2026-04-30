package dag

import (
	"encoding/json"
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

// TestFlow_TaskQueueRoundTrip — D3-19: Flow.TaskQueue is the per-flow Temporal
// task queue override, threaded by the parser from `flow(..., task_queue=...)`.
// Empty string means "inherit worker default" and is omitted from JSON.
func TestFlow_TaskQueueRoundTrip(t *testing.T) {
	pos := nodePos(t, 1, 1)
	f := &Flow{
		Pos:       pos,
		Name:      "f",
		Body:      []Node{},
		TaskQueue: "critical",
	}
	assert.Equal(t, "critical", f.TaskQueue)

	// Marshal and confirm the JSON contains the task_queue key.
	b, err := json.Marshal(f)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "critical", got["task_queue"], "task_queue must be present in marshaled Flow JSON")

	// Zero-value TaskQueue is omitted (omitempty).
	zero := &Flow{Pos: pos, Name: "f", Body: []Node{}}
	b2, err := json.Marshal(zero)
	require.NoError(t, err)
	var got2 map[string]any
	require.NoError(t, json.Unmarshal(b2, &got2))
	_, hasTaskQueue := got2["task_queue"]
	assert.False(t, hasTaskQueue, "zero-value TaskQueue must be omitted from JSON via omitempty")
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
