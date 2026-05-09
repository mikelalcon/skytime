package receiver

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
)

// pStringPtr returns a pointer to a string, used to construct
// syntax.Position values (its first field is *string).
func pStringPtr(s string) *string { return &s }

func TestComposeWorkflowID(t *testing.T) {
	tr := &dag.Trigger{
		FlowName: "webhook_demo",
		Pos:      syntax.MakePosition(pStringPtr("webhook_demo.star"), 5, 1),
	}
	got := composeWorkflowID(tr, "abc-delivery")

	sum := sha256.Sum256([]byte("webhook_demo.star:5:1"))
	wantHash := base64.RawURLEncoding.EncodeToString(sum[:])[:8]
	want := "webhook_demo/" + wantHash + "/abc-delivery"

	assert.Equal(t, want, got)
}

func TestComposeWorkflowID_Stable(t *testing.T) {
	tr := &dag.Trigger{
		FlowName: "f",
		Pos:      syntax.MakePosition(pStringPtr("a.star"), 12, 3),
	}
	a := composeWorkflowID(tr, "k")
	b := composeWorkflowID(tr, "k")
	assert.Equal(t, a, b, "composeWorkflowID must be deterministic for the same inputs")
}

func TestComposeWorkflowID_FanOutDifferentIDs(t *testing.T) {
	t1 := &dag.Trigger{
		FlowName: "shared_flow",
		Pos:      syntax.MakePosition(pStringPtr("file.star"), 5, 1),
	}
	t2 := &dag.Trigger{
		FlowName: "shared_flow",
		Pos:      syntax.MakePosition(pStringPtr("file.star"), 11, 1),
	}
	id1 := composeWorkflowID(t1, "same-key")
	id2 := composeWorkflowID(t2, "same-key")
	require.NotEqual(t, id1, id2, "two triggers at different positions must produce different WorkflowIDs even with the same FlowName + userKey (D-7.1-08 fan-out disambiguation)")
}

func TestComposeWorkflowID_PosLengthEightChars(t *testing.T) {
	tr := &dag.Trigger{
		FlowName: "f",
		Pos:      syntax.MakePosition(pStringPtr("a.star"), 1, 1),
	}
	got := posHash(tr)
	assert.Len(t, got, 8, "trigger_pos_hash must be exactly 8 base64url chars")
}
