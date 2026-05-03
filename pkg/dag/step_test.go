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

// TestStep_HasNameField — D4.1-15: Step.Name carries the literal display name
// from `step(name="...")`. Empty string falls back to auto-derived "<kind>(<label>)".
func TestStep_HasNameField(t *testing.T) {
	s := &Step{Name: "literal"}
	assert.Equal(t, "literal", s.Name)
}

// TestStep_HasNameFn — D4.1-15: Step.NameFn holds the desugared lambda when
// `step(name="...${ctx.x}...")` contains interpolation markers. Mutually
// exclusive with Name (parser enforces).
func TestStep_HasNameFn(t *testing.T) {
	s := &Step{NameFn: &CapturedLambda{ID: "id"}}
	require.NotNil(t, s.NameFn)
	assert.Equal(t, "id", s.NameFn.ID)
}

// TestStep_HasActionFn — D4.1-06: Step.ActionFn holds the lambda variant of
// `step(action_fn=lambda ctx: ext.op(...))`; evaluated inside the workflow
// at dispatch time and must return a single *ActionRef.
func TestStep_HasActionFn(t *testing.T) {
	s := &Step{ActionFn: &CapturedLambda{ID: "afn"}}
	require.NotNil(t, s.ActionFn)
	assert.Equal(t, "afn", s.ActionFn.ID)
}

// TestStep_HasBlockFn — D4.1-06: Step.BlockFn holds the lambda variant of
// `step(block_fn=lambda ctx: [ext.op(...) for ...])`; evaluated inside the
// workflow at dispatch time and must return a Starlark list of *ActionRef.
func TestStep_HasBlockFn(t *testing.T) {
	s := &Step{BlockFn: &CapturedLambda{ID: "bfn"}}
	require.NotNil(t, s.BlockFn)
	assert.Equal(t, "bfn", s.BlockFn.ID)
}

// TestStep_KindUnchanged — sanity check that adding the new optional fields
// does not regress the existing Node interface contract.
func TestStep_KindUnchanged(t *testing.T) {
	assert.Equal(t, "Step", (&Step{}).Kind())
}
