package bridge

import (
	"testing"

	"go.starlark.net/starlark"
)

// MustFreeze freezes the given Starlark value and verifies that subsequent
// mutation attempts return errors containing "frozen". Test-only helper used
// by plan 05 (parser) when asserting freeze cascades on values constructed at
// parse time.
//
// For *starlark.Dict and *starlark.List, MustFreeze probes the freeze by
// attempting a mutation; any other value type is frozen defensively without a
// post-freeze probe (most other Starlark values are immutable by design).
func MustFreeze(t *testing.T, v starlark.Value) {
	t.Helper()
	v.Freeze()
	switch x := v.(type) {
	case *starlark.Dict:
		if err := x.SetKey(starlark.String("__freeze_probe__"), starlark.None); err == nil {
			t.Fatalf("dict was not frozen: SetKey succeeded")
		}
	case *starlark.List:
		if err := x.Append(starlark.None); err == nil {
			t.Fatalf("list was not frozen: Append succeeded")
		}
	}
}
