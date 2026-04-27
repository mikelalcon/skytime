package dag

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
