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

// TestIfCond_OutputAliasZeroValue: D4.2-01 — zero-value OutputAlias means
// procedural mode (today's behavior); the field is omitted from JSON via
// `omitempty` so existing fixtures keep their current shape.
func TestIfCond_OutputAliasZeroValue(t *testing.T) {
	n := &IfCond{LambdaID: "L1"}
	assert.Equal(t, "", n.OutputAlias, "zero value preserves procedural mode")

	b, err := json.Marshal(n)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	_, has := m["output_alias"]
	assert.False(t, has, "output_alias must be omitted when empty (omitempty)")
}

// TestIfCond_OutputAliasSet: D4.2-01 — a non-empty OutputAlias serializes
// into JSON under the `output_alias` key.
func TestIfCond_OutputAliasSet(t *testing.T) {
	n := &IfCond{LambdaID: "L1", OutputAlias: "X"}
	b, err := json.Marshal(n)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "X", m["output_alias"])
	assert.Equal(t, "IfCond", m["kind"])
	assert.Equal(t, "L1", m["lambda_id"])
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

// TestScript_HasIDFn pins the D4.1-02 / W9 field. Compile-only proof
// that Script.IDFn exists and accepts a *CapturedLambda. Runtime
// evaluation belongs to a future plan; the parser-side desugar surface
// in 04.1-03 needs the field today.
func TestScript_HasIDFn(t *testing.T) {
	s := &Script{IDFn: &CapturedLambda{ID: "x"}}
	require.NotNil(t, s.IDFn)
	assert.Equal(t, "x", s.IDFn.ID)
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

// --- LogStep (Phase 07.2.1-01) -----------------------------------------------

// TestLogStep_NodeInterface pins the sealed Node-interface satisfaction at
// compile time. A failing change to the Node interface or to *LogStep's
// methods would break compilation of this test, not just the runtime check.
func TestLogStep_NodeInterface(t *testing.T) {
	var n Node = (*LogStep)(nil)
	// The nil-pointer-as-interface assertion is intentional: the cast on
	// the LHS exercises the sealed Node interface at compile time. There
	// is no meaningful runtime invariant to assert beyond "compiles".
	_ = n
}

// TestLogStep_Kind: D-7.2.1-15 — kind discriminator is exactly "LogStep".
func TestLogStep_Kind(t *testing.T) {
	assert.Equal(t, "LogStep", (&LogStep{}).Kind())
}

// TestLogStep_Position: Position() round-trips the embedded Pos. Mirrors
// the *Script precedent in pkg/dag/control.go.
func TestLogStep_Position(t *testing.T) {
	pos := nodePos(t, 7, 13)
	assert.Equal(t, pos, (&LogStep{Pos: pos}).Position())
}

// TestLogStep_AllFourLevelsConstructible: D-7.2.1-01 — all four slog
// levels (info/warn/error/debug) are storable. The type-level field is
// a plain string with NO enum validation; the parser is the level gate
// (rejects unknown levels at parse time). This keeps pkg/dag free of
// parser concerns.
func TestLogStep_AllFourLevelsConstructible(t *testing.T) {
	for _, level := range []string{"info", "warn", "error", "debug"} {
		ls := &LogStep{Level: level, Msg: "hello"}
		assert.Equal(t, level, ls.Level)
		assert.Equal(t, "hello", ls.Msg)
	}
}

// TestLogStep_FieldShape: pin the expected field set so a future refactor
// can't quietly add/rename fields without updating tests + JSON shape.
// LogStep is exempt from output_alias enforcement (D-7.2.1-16: side-channel
// step, no value produced) — there is no OutputAlias field on the struct
// (compile-time guarantee).
func TestLogStep_FieldShape(t *testing.T) {
	ls := &LogStep{}
	assert.Equal(t, "", ls.Level)
	assert.Equal(t, "", ls.Msg)
	assert.Nil(t, ls.MsgFn)
	assert.Equal(t, "", ls.AttrsLambdaID)
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
