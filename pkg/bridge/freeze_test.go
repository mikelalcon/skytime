package bridge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
)

// TestMustFreeze_Dict covers Test 12 — verify the helper's freeze cascade
// works on a *starlark.Dict.
func TestMustFreeze_Dict(t *testing.T) {
	d := starlark.NewDict(0)
	require.NoError(t, d.SetKey(starlark.String("k"), starlark.String("v")))

	// Pre-freeze — mutation works.
	require.NoError(t, d.SetKey(starlark.String("a"), starlark.String("b")))

	MustFreeze(t, d)

	// Post-freeze — mutation fails with a "frozen"-tagged error.
	err := d.SetKey(starlark.String("k2"), starlark.String("v2"))
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "frozen")
}

// TestMustFreeze_List verifies the list path through MustFreeze.
func TestMustFreeze_List(t *testing.T) {
	lst := starlark.NewList([]starlark.Value{
		starlark.String("a"),
		starlark.String("b"),
	})

	require.NoError(t, lst.Append(starlark.String("c")))

	MustFreeze(t, lst)

	err := lst.Append(starlark.String("d"))
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "frozen")
}

// TestMustFreeze_OtherType verifies the helper does not panic on a value type
// that has no specific assertion path — it just calls Freeze() defensively.
func TestMustFreeze_OtherType(t *testing.T) {
	// A starlark.String is already immutable; calling Freeze is a no-op.
	// The helper must not panic.
	MustFreeze(t, starlark.String("immutable"))
}
