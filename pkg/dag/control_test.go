package dag

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- IfCond ------------------------------------------------------------------

func TestIfCond_ZeroElseDoesNotCrash(t *testing.T) {
	// IfCond.Else is a slice; nil zero value is safe to len() and range over.
	pos := nodePos(t, 1, 1)
	n := &IfCond{Pos: pos, LambdaID: "deadbeef:1:1", Then: []Node{}}
	assert.Equal(t, 0, len(n.Else), "nil Else has zero length, ranges no times")
	for range n.Else {
		t.Fatal("ranging nil slice should produce zero iterations")
	}
}

// --- Script ------------------------------------------------------------------

func TestScript_FieldsRoundTrip(t *testing.T) {
	pos := nodePos(t, 7, 3)
	s := &Script{Pos: pos, ID: "compute_pr_link", LambdaID: "abcdef01:7:3", OutputAlias: "pr_link"}
	assert.Equal(t, "Script", s.Kind())
	assert.Equal(t, pos, s.Position())
	assert.Equal(t, "compute_pr_link", s.ID)
	assert.Equal(t, "abcdef01:7:3", s.LambdaID)
	assert.Equal(t, "pr_link", s.OutputAlias)
}

// --- ForEachParallel ---------------------------------------------------------

func TestForEachParallel_Validate_LambdaOnly(t *testing.T) {
	n := &ForEachParallel{ItemsLambdaID: "deadbeef:1:1"}
	require.NoError(t, n.Validate())
}

func TestForEachParallel_Validate_LiteralOnly(t *testing.T) {
	n := &ForEachParallel{ItemsLiteral: []any{"a", "b"}}
	require.NoError(t, n.Validate())
}

func TestForEachParallel_Validate_RejectsBoth(t *testing.T) {
	n := &ForEachParallel{ItemsLambdaID: "x", ItemsLiteral: []any{"a"}}
	err := n.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "got both")
}

func TestForEachParallel_Validate_RejectsNeither(t *testing.T) {
	n := &ForEachParallel{}
	err := n.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "got neither")
}

// TestForEachParallel_MaxConcurrencyRoundTrip — D3-13: ForEachParallel.
// MaxConcurrency is the optional fan-out cap. Zero means "use interpreter
// default (10)" per D3-13. The field is JSON-tagged with omitempty so the
// default value is omitted from golden output.
func TestForEachParallel_MaxConcurrencyRoundTrip(t *testing.T) {
	n := &ForEachParallel{
		ItemsLiteral:   []any{"a", "b"},
		ItemVar:        "i",
		Steps:          []Node{},
		MaxConcurrency: 5,
	}
	assert.Equal(t, 5, n.MaxConcurrency)

	b, err := json.Marshal(n)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	// JSON numbers come back as float64.
	assert.Equal(t, float64(5), got["max_concurrency"],
		"max_concurrency must be present in marshaled ForEachParallel JSON")
}

// TestForEachParallel_MaxConcurrencyDefaultZero asserts the zero-value
// MaxConcurrency is 0 (interpreted as "use interpreter default") and is
// omitted from JSON output via the omitempty tag.
func TestForEachParallel_MaxConcurrencyDefaultZero(t *testing.T) {
	n := &ForEachParallel{
		ItemsLiteral: []any{"a"},
		ItemVar:      "i",
		Steps:        []Node{},
	}
	assert.Equal(t, 0, n.MaxConcurrency, "zero-value MaxConcurrency means interpreter default")

	b, err := json.Marshal(n)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	_, hasMaxConc := got["max_concurrency"]
	assert.False(t, hasMaxConc, "zero-value MaxConcurrency must be omitted via omitempty")
}

// --- CallFlow ----------------------------------------------------------------

func TestCallFlow_ResolvedStartsNil(t *testing.T) {
	pos := nodePos(t, 9, 4)
	c := &CallFlow{Pos: pos, Name: "approve_pr"}
	assert.Nil(t, c.Resolved, "Resolved is nil until the parser's cross-flow resolution pass")
}

func TestCallFlow_ResolvedSkippedFromJSON(t *testing.T) {
	// Resolved is tagged json:"-" so a CallFlow that has been resolved does
	// not drag the entire transitive flow graph into golden-file output.
	pos := nodePos(t, 9, 4)
	target := &Flow{Pos: pos, Name: "approve_pr"}
	c := &CallFlow{Pos: pos, Name: "approve_pr", Resolved: target}

	b, err := json.Marshal(c)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	_, hasResolved := m["Resolved"]
	assert.False(t, hasResolved, "Resolved must be omitted from JSON output")
	_, hasResolvedLower := m["resolved"]
	assert.False(t, hasResolvedLower, "Resolved must be omitted regardless of casing")
}
