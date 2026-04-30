package interpreter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestState_SnapshotIsSortedKeyStable verifies that ten consecutive
// snapshot() calls all observe the same sorted-key iteration order (D3-23).
// Map iteration is performed via sortedKeys, so even though Go's native map
// iteration is random per-iteration, the sorted-keys helper produces a
// deterministic key sequence.
func TestState_SnapshotIsSortedKeyStable(t *testing.T) {
	s := newState(map[string]any{
		"z": 1,
		"a": 2,
		"m": 3,
		"b": 4,
		"y": 5,
	})

	expected := []string{"a", "b", "m", "y", "z"}
	for i := 0; i < 10; i++ {
		out := s.snapshot()
		require.Len(t, out, 5)
		got := sortedKeys(out)
		require.Equal(t, expected, got, "snapshot iteration %d produced unstable key order", i)
	}
}

// TestState_ScopedAddsItemVar verifies that scoped() injects itemVar at the
// requested key and preserves the parent's bindings. The returned state is
// distinct from the receiver (no shared map).
func TestState_ScopedAddsItemVar(t *testing.T) {
	parent := newState(map[string]any{"shared": "p"})
	child := parent.scoped("row", map[string]any{"id": 1})

	require.NotSame(t, parent, child, "scoped must return a fresh *state")

	cs := child.snapshot()
	assert.Equal(t, "p", cs["shared"], "parent binding preserved")
	row, ok := cs["row"].(map[string]any)
	require.True(t, ok, "row item present")
	assert.Equal(t, 1, row["id"])

	// Parent unmodified by scoped().
	ps := parent.snapshot()
	_, hasRow := ps["row"]
	assert.False(t, hasRow, "parent state must NOT carry the scoped itemVar")
}

// TestState_ScopedEmptyItemVarReturnsSame verifies that scoped("", anything)
// is a pass-through — no shadowing, same receiver returned. This matches the
// for_each_parallel walker's behavior when item= kwarg is omitted (the
// branch shouldn't synthesize a key).
func TestState_ScopedEmptyItemVarReturnsSame(t *testing.T) {
	s := newState(map[string]any{"a": 1})
	out := s.scoped("", "ignored")
	require.Same(t, s, out, "scoped with empty itemVar must return the receiver unchanged")
}

// TestState_SetOutput verifies setOutput publishes a key visible in the next
// snapshot. Used by walkScript (plan 03-03) to publish lambda return values
// under their declared alias.
func TestState_SetOutput(t *testing.T) {
	s := newState(nil)
	s.setOutput("k", "v")
	out := s.snapshot()
	assert.Equal(t, "v", out["k"])
}

// TestState_SortedKeysReturnsAlphabetical exercises the sortedKeys helper
// directly. Defense-in-depth: if D3-23's iteration discipline ever regresses,
// this test fails before snapshot/scoped tests do.
func TestState_SortedKeysReturnsAlphabetical(t *testing.T) {
	got := sortedKeys(map[string]any{"z": 1, "a": 2, "m": 3})
	assert.Equal(t, []string{"a", "m", "z"}, got)

	// Empty/nil-safety.
	assert.Empty(t, sortedKeys(nil))
	assert.Empty(t, sortedKeys(map[string]any{}))
}
