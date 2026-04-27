package bridge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

// TestFromStarlarkValue_BasicTypes covers Test 10 (basic types) — String, Int,
// Float, Bool, *List, *Dict, None.
func TestFromStarlarkValue_None(t *testing.T) {
	g, err := FromStarlarkValue(starlark.None)
	require.NoError(t, err)
	assert.Nil(t, g)
}

func TestFromStarlarkValue_String(t *testing.T) {
	g, err := FromStarlarkValue(starlark.String("hello"))
	require.NoError(t, err)
	assert.Equal(t, "hello", g)
}

func TestFromStarlarkValue_Int(t *testing.T) {
	g, err := FromStarlarkValue(starlark.MakeInt(42))
	require.NoError(t, err)
	assert.Equal(t, int64(42), g)
}

func TestFromStarlarkValue_Float(t *testing.T) {
	g, err := FromStarlarkValue(starlark.Float(3.14))
	require.NoError(t, err)
	assert.Equal(t, 3.14, g)
}

func TestFromStarlarkValue_Bool(t *testing.T) {
	g, err := FromStarlarkValue(starlark.True)
	require.NoError(t, err)
	assert.Equal(t, true, g)
	g, err = FromStarlarkValue(starlark.False)
	require.NoError(t, err)
	assert.Equal(t, false, g)
}

func TestFromStarlarkValue_List(t *testing.T) {
	lst := starlark.NewList([]starlark.Value{
		starlark.String("a"),
		starlark.MakeInt(7),
		starlark.True,
	})
	g, err := FromStarlarkValue(lst)
	require.NoError(t, err)
	assert.Equal(t, []any{"a", int64(7), true}, g)
}

func TestFromStarlarkValue_Dict(t *testing.T) {
	d := starlark.NewDict(2)
	require.NoError(t, d.SetKey(starlark.String("x"), starlark.MakeInt(1)))
	require.NoError(t, d.SetKey(starlark.String("y"), starlark.String("z")))

	g, err := FromStarlarkValue(d)
	require.NoError(t, err)
	m, ok := g.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, int64(1), m["x"])
	assert.Equal(t, "z", m["y"])
}

func TestFromStarlarkValue_Struct(t *testing.T) {
	// Test 11 (FromStarlarkValue): nested struct converts to map.
	sd := starlark.StringDict{
		"a": starlark.MakeInt(1),
		"b": starlark.String("hi"),
	}
	st := starlarkstruct.FromStringDict(starlarkstruct.Default, sd)

	g, err := FromStarlarkValue(st)
	require.NoError(t, err)
	m, ok := g.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, int64(1), m["a"])
	assert.Equal(t, "hi", m["b"])
}

func TestFromStarlarkValue_NestedStruct(t *testing.T) {
	inner := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"repo_name": starlark.String("acme/widget"),
	})
	outer := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"req": inner,
	})

	g, err := FromStarlarkValue(outer)
	require.NoError(t, err)
	m, ok := g.(map[string]any)
	require.True(t, ok)
	reqMap, ok := m["req"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "acme/widget", reqMap["repo_name"])
}

func TestFromStarlarkValue_UnsupportedType(t *testing.T) {
	// A *starlark.Function is the canonical unsupported type — the bridge
	// returns lambdas to Phase 3 via *CapturedLambda, never as raw values.
	thread := &starlark.Thread{Name: "test"}
	opts := &syntax.FileOptions{}
	globals, err := starlark.ExecFileOptions(opts, thread, "test.star",
		`f = lambda x: x`, nil)
	require.NoError(t, err)
	fn, ok := globals["f"].(*starlark.Function)
	require.True(t, ok)

	_, err = FromStarlarkValue(fn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FromStarlarkValue: unsupported type")
}

func TestFromStarlarkValue_DictNonStringKey(t *testing.T) {
	d := starlark.NewDict(1)
	require.NoError(t, d.SetKey(starlark.MakeInt(1), starlark.String("v")))

	_, err := FromStarlarkValue(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dict key must be string")
}
