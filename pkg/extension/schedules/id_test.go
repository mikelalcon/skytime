package schedules

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// pStringPtr returns a pointer to a string, used to construct
// syntax.Position values (its first field is *string, mirroring
// receiver_test.go's helper).
func pStringPtr(s string) *string { return &s }

// TestComposeScheduleID pins the ID shape locked by D-7.2-04:
//
//	"skytime/{flow_name}/{8-char-base64url-sha256(Pos.String())}"
//
// The hash is computed inline (sha256 → base64url → first 8 chars) so any
// drift in the production helper is caught byte-for-byte.
func TestComposeScheduleID(t *testing.T) {
	tr := &dag.Trigger{
		FlowName: "weekly_digest",
		Pos:      syntax.MakePosition(pStringPtr("weekly_digest.star"), 5, 1),
	}
	got := ComposeScheduleID(tr)

	sum := sha256.Sum256([]byte("weekly_digest.star:5:1"))
	wantHash := base64.RawURLEncoding.EncodeToString(sum[:])[:8]
	want := "skytime/weekly_digest/" + wantHash

	require.Equal(t, want, got)
}

// TestComposeScheduleID_Deterministic — two calls with the same input
// produce the same string (Schedule ID is content-addressable via Pos).
func TestComposeScheduleID_Deterministic(t *testing.T) {
	tr := &dag.Trigger{
		FlowName: "f",
		Pos:      syntax.MakePosition(pStringPtr("a.star"), 12, 3),
	}
	a := ComposeScheduleID(tr)
	b := ComposeScheduleID(tr)
	assert.Equal(t, a, b)
}

// TestComposeScheduleID_DifferentPosDifferentHash — two triggers with the
// same FlowName but different Pos line numbers must produce different IDs
// (otherwise two cron triggers in the same flow would collide on Create).
func TestComposeScheduleID_DifferentPosDifferentHash(t *testing.T) {
	t1 := &dag.Trigger{
		FlowName: "shared_flow",
		Pos:      syntax.MakePosition(pStringPtr("file.star"), 5, 1),
	}
	t2 := &dag.Trigger{
		FlowName: "shared_flow",
		Pos:      syntax.MakePosition(pStringPtr("file.star"), 11, 1),
	}
	id1 := ComposeScheduleID(t1)
	id2 := ComposeScheduleID(t2)
	require.NotEqual(t, id1, id2)
}

// TestIsSkytimeManaged pins the isolation primitive — only IDs starting
// with "skytime/" are subject to reconciliation. Edge cases included so
// any future refactor (e.g., trailing-slash sloppiness) breaks loudly.
func TestIsSkytimeManaged(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"skytime/x/y", true},
		{"", false},
		{"sky", false},
		{"skytime", false}, // no trailing slash
		{"skytime/", true}, // boundary
		{"otherprefix/skytime/x", false},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			require.Equal(t, c.want, IsSkytimeManaged(c.id))
		})
	}
}

// TestIDHashParity pins byte-for-byte equality with the algorithm
// pkg/extension/receiver/workflow_id.go::posHash uses (sha256 → base64url
// → first 8 chars of Pos.String()). Re-implementation rather than import
// dependency avoids cross-package coupling; this test is the regression
// coupling.
func TestIDHashParity(t *testing.T) {
	pos := syntax.MakePosition(pStringPtr("x.star"), 5, 1)
	trig := &dag.Trigger{FlowName: "foo", Pos: pos}
	got := posHash(trig)

	// Receiver-side equivalent computed inline. If receiver.posHash or
	// schedules.posHash ever drift, this test breaks AND the receiver
	// package's TestComposeWorkflowID breaks — providing the regression
	// coupling without requiring a test-only export from receiver.
	sum := sha256.Sum256([]byte(pos.String()))
	want := base64.RawURLEncoding.EncodeToString(sum[:])[:8]
	require.Equal(t, want, got)
	require.Len(t, got, 8, "posHash output must be exactly 8 base64url chars")
}
