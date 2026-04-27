package dag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.starlark.net/syntax"
)

// Compile-time sealed-interface assertions: every concrete node type satisfies
// Node. If a type is added/removed without updating this list the build breaks.
var (
	_ Node = (*Flow)(nil)
	_ Node = (*Step)(nil)
	_ Node = (*IfCond)(nil)
	_ Node = (*Script)(nil)
	_ Node = (*ForEachParallel)(nil)
	_ Node = (*CallFlow)(nil)
)

// The Node interface includes an unexported `nodeMarker()` method, sealing it
// to types declared in pkg/dag. External packages cannot implement Node — this
// prevents Phase 3 from accidentally widening the type set without a
// deliberate change to pkg/dag. To verify: try `type FakeNode struct{}` outside
// pkg/dag with the three Node methods; the compiler rejects with "cannot use
// ... as Node value" because the unexported nodeMarker method is not
// reachable. We document the property here rather than enforce it at runtime.

// nodePos returns a syntax.Position satisfying IsValid(); used across the
// pure-data node tests to seed call-site information.
func nodePos(t *testing.T, line, col int32) syntax.Position {
	t.Helper()
	name := "f.star"
	return syntax.MakePosition(&name, line, col)
}

func TestNode_KindAndPosition(t *testing.T) {
	pos := nodePos(t, 12, 3)

	cases := []struct {
		name    string
		node    Node
		wantStr string
	}{
		{"Flow", &Flow{Pos: pos, Name: "approve_pr"}, "Flow"},
		{"Step", &Step{Pos: pos}, "Step"},
		{"IfCond", &IfCond{Pos: pos}, "IfCond"},
		{"Script", &Script{Pos: pos}, "Script"},
		{"ForEachParallel", &ForEachParallel{Pos: pos}, "ForEachParallel"},
		{"CallFlow", &CallFlow{Pos: pos}, "CallFlow"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.wantStr, c.node.Kind(), "Kind() returns the discriminator")
			assert.Equal(t, pos, c.node.Position(), "Position() round-trips the embedded position")
		})
	}
}
