package bridge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// TestToStarlarkStruct_BasicTypes covers Test 1 — basic round-trip of primitive
// types (string, int, bool) into a *starlarkstruct.Struct.
func TestToStarlarkStruct_BasicTypes(t *testing.T) {
	s, err := ToStarlarkStruct(map[string]any{
		"a": 1,
		"b": "two",
		"c": true,
	})
	require.NoError(t, err)
	require.NotNil(t, s)

	a, err := s.Attr("a")
	require.NoError(t, err)
	aInt, ok := a.(starlark.Int)
	require.True(t, ok)
	got, ok := aInt.Int64()
	require.True(t, ok)
	assert.Equal(t, int64(1), got)

	b, err := s.Attr("b")
	require.NoError(t, err)
	assert.Equal(t, starlark.String("two"), b)

	c, err := s.Attr("c")
	require.NoError(t, err)
	assert.Equal(t, starlark.True, c)
}

// TestToStarlarkStruct_DotAccess_DSL09 covers Test 2 — nested map access via
// dot notation: ctx.req.repo_name. This is the load-bearing DSL-09 test.
func TestToStarlarkStruct_DotAccess_DSL09(t *testing.T) {
	s, err := ToStarlarkStruct(map[string]any{
		"req": map[string]any{"repo_name": "acme/widget"},
	})
	require.NoError(t, err)

	req, err := s.Attr("req")
	require.NoError(t, err)
	reqStruct, ok := req.(*starlarkstruct.Struct)
	require.True(t, ok, "expected nested *starlarkstruct.Struct, got %T", req)

	repoName, err := reqStruct.Attr("repo_name")
	require.NoError(t, err)
	assert.Equal(t, starlark.String("acme/widget"), repoName)
}

// TestToStarlarkStruct_Deterministic covers Test 3 — the iter-determinism
// property test. ROADMAP.md success criterion #4 / DSL-09 success criterion.
// Converting the SAME map twice must produce identical AttrNames() and
// String() output.
func TestToStarlarkStruct_Deterministic(t *testing.T) {
	m := map[string]any{"x": 1, "a": "hi", "z": true, "b": 2.5}

	s1, err := ToStarlarkStruct(m)
	require.NoError(t, err)
	s2, err := ToStarlarkStruct(m)
	require.NoError(t, err)

	// AttrNames must be identical (same order — proves sorted insertion).
	assert.Equal(t, s1.AttrNames(), s2.AttrNames(), "AttrNames must be deterministic")

	// String() must be identical (proves underlying StringDict insertion order matches).
	assert.Equal(t, s1.String(), s2.String(), "String() must be deterministic")
}

// TestToStarlarkStruct_LargeMap covers Test 4 — a heterogeneous map with 100
// keys. Larger maps are more likely to surface map-iteration randomization.
func TestToStarlarkStruct_LargeMap(t *testing.T) {
	m := make(map[string]any, 100)
	for i := 0; i < 100; i++ {
		// Mix names that hash to similar buckets to stress map iteration.
		m[string(rune('a'+(i%26)))+string(rune('0'+(i/10)))+string(rune('0'+(i%10)))] = i
	}

	s1, err := ToStarlarkStruct(m)
	require.NoError(t, err)
	s2, err := ToStarlarkStruct(m)
	require.NoError(t, err)

	assert.Equal(t, s1.AttrNames(), s2.AttrNames(),
		"AttrNames must be deterministic for 100-key map")
	assert.Equal(t, s1.String(), s2.String(),
		"String() must be deterministic for 100-key map")
}

// TestToStarlarkStruct_ListValues covers Test 5 — list values are converted to
// frozen *starlark.List instances.
func TestToStarlarkStruct_ListValues(t *testing.T) {
	s, err := ToStarlarkStruct(map[string]any{
		"items": []any{"a", "b", "c"},
	})
	require.NoError(t, err)

	items, err := s.Attr("items")
	require.NoError(t, err)
	lst, ok := items.(*starlark.List)
	require.True(t, ok, "expected *starlark.List, got %T", items)

	// Length matches.
	assert.Equal(t, 3, lst.Len())

	// Frozen — Append must error.
	err = lst.Append(starlark.String("d"))
	require.Error(t, err, "list must be frozen — Append should fail")
	assert.Contains(t, strings.ToLower(err.Error()), "frozen")
}

// TestToStarlarkStruct_UnsupportedType covers Test 6 — an unsupported Go type
// (e.g., chan int) returns an error containing "unsupported type".
func TestToStarlarkStruct_UnsupportedType(t *testing.T) {
	_, err := ToStarlarkStruct(map[string]any{
		"ch": make(chan int),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported type")
}

// TestToStarlarkStruct_NilValue covers Test 7 — nil values become starlark.None.
func TestToStarlarkStruct_NilValue(t *testing.T) {
	s, err := ToStarlarkStruct(map[string]any{
		"x": nil,
	})
	require.NoError(t, err)

	x, err := s.Attr("x")
	require.NoError(t, err)
	assert.Equal(t, starlark.None, x)
}

// TestToStarlarkStruct_IntegerSupport covers Test 8 — int and int64 both work,
// including large int64 values. starlark.Int holds an unexported impl pointer,
// so we compare via Int64() rather than struct equality.
func TestToStarlarkStruct_IntegerSupport(t *testing.T) {
	s, err := ToStarlarkStruct(map[string]any{
		"small":   int(42),
		"large64": int64(1<<60 + 7),
	})
	require.NoError(t, err)

	small, err := s.Attr("small")
	require.NoError(t, err)
	smallInt, ok := small.(starlark.Int)
	require.True(t, ok)
	gotSmall, ok := smallInt.Int64()
	require.True(t, ok)
	assert.Equal(t, int64(42), gotSmall)

	large, err := s.Attr("large64")
	require.NoError(t, err)
	largeInt, ok := large.(starlark.Int)
	require.True(t, ok)
	gotLarge, ok := largeInt.Int64()
	require.True(t, ok)
	assert.Equal(t, int64(1<<60+7), gotLarge)
}

// TestToStarlarkStruct_Float64 covers Test 9 — float64 round-trips correctly.
func TestToStarlarkStruct_Float64(t *testing.T) {
	s, err := ToStarlarkStruct(map[string]any{"pi": 3.14})
	require.NoError(t, err)

	pi, err := s.Attr("pi")
	require.NoError(t, err)
	assert.Equal(t, starlark.Float(3.14), pi)
}

// TestToStarlarkStruct_NestedListDeterminism — nested list inside map; check
// that a slice of maps round-trips with deterministic key order on each inner
// struct.
func TestToStarlarkStruct_NestedListDeterminism(t *testing.T) {
	m := map[string]any{
		"items": []any{
			map[string]any{"id": 1, "name": "a"},
			map[string]any{"id": 2, "name": "b"},
		},
	}

	s1, err := ToStarlarkStruct(m)
	require.NoError(t, err)
	s2, err := ToStarlarkStruct(m)
	require.NoError(t, err)

	assert.Equal(t, s1.String(), s2.String())
}
