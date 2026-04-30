package dag

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep_TaskQueueRoundTrip — D3-19: Step.TaskQueue is the optional
// per-step Temporal task queue override, threaded by the parser from
// `step(..., task_queue=...)`. Empty string means "inherit flow's task queue"
// and is omitted from JSON.
func TestStep_TaskQueueRoundTrip(t *testing.T) {
	pos := nodePos(t, 5, 2)
	s := &Step{Pos: pos, Actions: []*ActionRef{{Kind_: "x.y"}}, TaskQueue: "slow_io"}
	assert.Equal(t, "slow_io", s.TaskQueue)

	b, err := json.Marshal(s)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "slow_io", got["task_queue"], "task_queue must be present in marshaled Step JSON")

	// Zero-value TaskQueue omitted via omitempty.
	zero := &Step{Pos: pos}
	b2, err := json.Marshal(zero)
	require.NoError(t, err)
	var got2 map[string]any
	require.NoError(t, json.Unmarshal(b2, &got2))
	_, hasTaskQueue := got2["task_queue"]
	assert.False(t, hasTaskQueue, "zero-value Step.TaskQueue must be omitted from JSON via omitempty")
}

func TestStep_ZeroValue(t *testing.T) {
	pos := nodePos(t, 5, 2)
	s := &Step{Pos: pos}

	assert.Equal(t, "Step", s.Kind())
	assert.Equal(t, pos, s.Position())
	assert.Nil(t, s.Actions, "zero-value Actions is nil; parser populates")
	assert.Nil(t, s.Retry, "zero-value Retry is nil — DSL-08 'no retry= kwarg given'")
	assert.Nil(t, s.Timeout, "zero-value Timeout is nil — DSL-08 'no timeout= kwarg given'")
}

func TestStep_ActionsSlice(t *testing.T) {
	// Actions slice accepts pointers; the size-1 vs size->1 distinction
	// (action= vs block=) is enforced by the parser, not this type.
	pos := nodePos(t, 5, 2)
	s := &Step{
		Pos:     pos,
		Actions: []*ActionRef{{}, {}},
	}
	assert.Len(t, s.Actions, 2)
}
