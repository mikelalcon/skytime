package dag

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
)

// --- Per-type kind discriminator tests ---------------------------------------

func TestFlow_MarshalJSON_HasKindDiscriminator(t *testing.T) {
	f := &Flow{Name: "x", Body: []Node{}}
	b, err := json.Marshal(f)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "Flow", m["kind"])
	assert.Equal(t, "x", m["name"])
	body, ok := m["body"].([]any)
	require.True(t, ok)
	assert.Empty(t, body)
}

func TestStep_MarshalJSON_HasKindDiscriminator(t *testing.T) {
	s := &Step{}
	b, err := json.Marshal(s)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "Step", m["kind"])
	actions, ok := m["actions"].([]any)
	require.True(t, ok, "actions must marshal as an array even when nil")
	assert.Empty(t, actions)
}

func TestIfCond_MarshalJSON_HasKindDiscriminator(t *testing.T) {
	n := &IfCond{LambdaID: "deadbeef:1:1"}
	b, err := json.Marshal(n)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "IfCond", m["kind"])
	assert.Equal(t, "deadbeef:1:1", m["lambda_id"])
	then, ok := m["then"].([]any)
	require.True(t, ok)
	assert.Empty(t, then)
	els, ok := m["else"].([]any)
	require.True(t, ok)
	assert.Empty(t, els)
}

func TestScript_MarshalJSON_HasKindDiscriminator(t *testing.T) {
	n := &Script{ID: "compute_link", LambdaID: "abc:1:2", OutputAlias: "pr_link"}
	b, err := json.Marshal(n)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "Script", m["kind"])
	assert.Equal(t, "compute_link", m["id"])
	assert.Equal(t, "abc:1:2", m["lambda_id"])
	assert.Equal(t, "pr_link", m["output_alias"])
}

func TestForEachParallel_MarshalJSON_HasKindDiscriminator(t *testing.T) {
	n := &ForEachParallel{ItemVar: "i", ItemsLiteral: []any{"a", "b"}}
	b, err := json.Marshal(n)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "ForEachParallel", m["kind"])
	assert.Equal(t, "i", m["item_var"])
	items, ok := m["items_literal"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"a", "b"}, items)
	steps, ok := m["steps"].([]any)
	require.True(t, ok)
	assert.Empty(t, steps)
}

func TestCallFlow_MarshalJSON_HasKindDiscriminator(t *testing.T) {
	n := &CallFlow{Name: "approve", Inputs: map[string]any{"repo": "x"}}
	b, err := json.Marshal(n)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "CallFlow", m["kind"])
	assert.Equal(t, "approve", m["name"])
	inputs, ok := m["inputs"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "x", inputs["repo"])
}

func TestActionRef_MarshalJSON_HasKindAndKwargs(t *testing.T) {
	d := starlark.NewDict(2)
	require.NoError(t, d.SetKey(starlark.String("repo"), starlark.String("foo/bar")))
	require.NoError(t, d.SetKey(starlark.String("count"), starlark.MakeInt(7)))
	a := &ActionRef{Kind_: "github.create_issue", Kwargs: d, CredentialID: "admin"}

	b, err := json.Marshal(a)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "github.create_issue", m["kind"])
	assert.Equal(t, "admin", m["credential_id"])
	kw, ok := m["kwargs"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "foo/bar", kw["repo"])
	// JSON unmarshals integers into float64
	assert.Equal(t, float64(7), kw["count"])
}

func TestActionRef_MarshalJSON_OmitsPos(t *testing.T) {
	// The Pos field must NOT appear — Pos.Filename is machine-absolute and
	// would break cross-machine golden stability.
	a := &ActionRef{Kind_: "x"}
	b, err := json.Marshal(a)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	_, hasPos := m["pos"]
	assert.False(t, hasPos)
	_, hasPosUpper := m["Pos"]
	assert.False(t, hasPosUpper)
}

// --- Heterogeneous body discriminator test -----------------------------------

func TestFlow_MarshalJSON_HeterogeneousBodyEachHasKind(t *testing.T) {
	f := &Flow{
		Name: "x",
		Body: []Node{
			&Step{},
			&IfCond{LambdaID: "a:1:1"},
			&Script{ID: "s", LambdaID: "b:1:1", OutputAlias: "out"},
			&ForEachParallel{ItemVar: "i", ItemsLambdaID: "c:1:1"},
			&CallFlow{Name: "child"},
		},
	}
	b, err := json.Marshal(f)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	body, ok := m["body"].([]any)
	require.True(t, ok)
	require.Len(t, body, 5)
	want := []string{"Step", "IfCond", "Script", "ForEachParallel", "CallFlow"}
	for i, w := range want {
		obj, ok := body[i].(map[string]any)
		require.True(t, ok, "body[%d] is not an object", i)
		assert.Equal(t, w, obj["kind"], "body[%d] kind mismatch", i)
	}
}

// --- Stability tests ---------------------------------------------------------

func TestMarshal_Stable_FlowWithBody(t *testing.T) {
	// Same value marshaled twice must produce byte-identical output.
	f := &Flow{
		Name: "x",
		Body: []Node{
			&Step{},
			&IfCond{LambdaID: "a:1:1"},
		},
	}
	b1, err := json.Marshal(f)
	require.NoError(t, err)
	b2, err := json.Marshal(f)
	require.NoError(t, err)
	assert.Equal(t, string(b1), string(b2))
}

// --- D4.2-01: Result + Fail marshal ------------------------------------------

// TestMarshal_ResultNode: D4.2-04. Result emits {"kind":"Result","keys":[...]};
// Values + Types are deliberately omitted (Pitfall 8: lambda IDs are unstable
// content-hash-suffixed; TypeInfo encoding is deferred).
func TestMarshal_ResultNode(t *testing.T) {
	r := &Result{
		Keys: []string{"a", "b"},
		Values: map[string]*CapturedLambda{
			"a": {ID: "L1"},
			"b": {ID: "L2"},
		},
		Types: map[string]any{"a": struct{ Kind string }{"int"}},
	}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "Result", m["kind"])
	keys, ok := m["keys"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"a", "b"}, keys)
	// Values + Types must be absent from the wire form.
	_, hasValues := m["values"]
	assert.False(t, hasValues, "Values must not marshal in v1 (lambda IDs unstable)")
	_, hasTypes := m["types"]
	assert.False(t, hasTypes, "Types must not marshal in v1 (TypeInfo encoding deferred)")
}

// TestMarshal_FailNode_LiteralOnly: D4.2-01. Fail with no MessageFn emits
// {"kind":"Fail","message":"..."} and omits message_lambda_id.
func TestMarshal_FailNode_LiteralOnly(t *testing.T) {
	f := &Fail{Message: "missing repo"}
	b, err := json.Marshal(f)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "Fail", m["kind"])
	assert.Equal(t, "missing repo", m["message"])
	_, hasFn := m["message_lambda_id"]
	assert.False(t, hasFn, "message_lambda_id must be omitted when MessageFn is nil")
}

// TestMarshal_FailNode_WithLambda: D4.2-01. Fail with MessageFn collapses
// the lambda pointer to its stable ID under message_lambda_id; Message
// retains the verbatim template (with ${...}).
func TestMarshal_FailNode_WithLambda(t *testing.T) {
	f := &Fail{
		Message:   "missing ${ctx.x}",
		MessageFn: &CapturedLambda{ID: "L3"},
	}
	b, err := json.Marshal(f)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "Fail", m["kind"])
	assert.Equal(t, "missing ${ctx.x}", m["message"])
	assert.Equal(t, "L3", m["message_lambda_id"])
}

// TestMarshal_StableAcrossTwoRoundtrips: same value marshals to byte-identical
// output across two invocations. Mirrors Phase 1 plan 02 stability pattern,
// extended with a heterogeneous body containing IfCond (with OutputAlias),
// Result, and Fail.
func TestMarshal_StableAcrossTwoRoundtrips(t *testing.T) {
	f := &Flow{
		Name: "x",
		Body: []Node{
			&IfCond{
				LambdaID:    "L1",
				OutputAlias: "out",
				Then:        []Node{&Result{Keys: []string{"sign"}}},
				Else:        []Node{&Fail{Message: "no"}},
			},
		},
	}
	b1, err := json.Marshal(f)
	require.NoError(t, err)
	b2, err := json.Marshal(f)
	require.NoError(t, err)
	assert.Equal(t, string(b1), string(b2))

	// Sanity: each child still carries its kind discriminator (regression
	// guard in case the per-type MarshalJSON dispatch ever drifts).
	assert.Contains(t, string(b1), `"kind":"Flow"`)
	assert.Contains(t, string(b1), `"kind":"IfCond"`)
	assert.Contains(t, string(b1), `"kind":"Result"`)
	assert.Contains(t, string(b1), `"kind":"Fail"`)
	assert.Contains(t, string(b1), `"output_alias":"out"`)
}

// --- LogStep (Phase 07.2.1-01) -----------------------------------------------

// TestLogStep_MarshalJSON_Literal: D-7.2.1-15 / plan 07.2.1-01 — a LogStep
// with only Level + Msg (no interpolation lambda, no attrs lambda) emits a
// minimal {kind, level, msg} object. The optional msg_lambda_id and
// attrs_lambda_id fields are omitted via `omitempty` so the simple-case
// JSON stays tight (mirrors *Fail's failJSON shape).
func TestLogStep_MarshalJSON_Literal(t *testing.T) {
	n := &LogStep{Level: "info", Msg: "hello"}
	b, err := json.Marshal(n)
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"LogStep","level":"info","msg":"hello"}`, string(b))
}

// TestLogStep_MarshalJSON_WithLambdas: D-7.2.1-15 — when MsgFn is set
// (interpolation desugarer fired) and AttrsLambdaID is non-empty,
// both lambda IDs surface as msg_lambda_id and attrs_lambda_id. The Msg
// template is preserved verbatim (including ${...} markers) so debugging
// tools can show the literal source alongside the resolved-at-runtime
// lambda ID.
func TestLogStep_MarshalJSON_WithLambdas(t *testing.T) {
	n := &LogStep{
		Level:         "warn",
		Msg:           "${ctx.x}",
		MsgFn:         &CapturedLambda{ID: "lam_abc"},
		AttrsLambdaID: "lam_xyz",
	}
	b, err := json.Marshal(n)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"kind":"LogStep","level":"warn","msg":"${ctx.x}","msg_lambda_id":"lam_abc","attrs_lambda_id":"lam_xyz"}`,
		string(b))
}

// TestLogStep_MarshalJSON_NoLambdaSerialized: load-bearing security guard
// — the *starlark.Function inside CapturedLambda must NEVER leak into the
// wire JSON. Only the opaque content-hash ID surfaces via msg_lambda_id.
// This mirrors the Script.IDFn / Fail.MessageFn precedents: lambda
// pointers are in-memory only and rehydrate at workflow start via the
// content-hash-keyed FlowRegistry (Phase 3 lambda-serialization contract).
func TestLogStep_MarshalJSON_NoLambdaSerialized(t *testing.T) {
	n := &LogStep{Level: "info", Msg: "hi", MsgFn: &CapturedLambda{ID: "lam_xyz"}}
	b, err := json.Marshal(n)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "Function",
		"*starlark.Function pointer must not leak into wire JSON")
	assert.NotContains(t, string(b), "*starlark",
		"Starlark internal types must not appear in wire JSON")
	assert.Contains(t, string(b), `"msg_lambda_id":"lam_xyz"`,
		"opaque lambda ID is the only legal surface")
}

// TestLogStep_MarshalJSON_InSlice: verifies LogStep.MarshalJSON dispatches
// when LogStep is an element of a []Node slice. This is the path used by
// Flow.MarshalJSON when a flow body contains a log step — the heterogeneous
// body test in TestFlow_MarshalJSON_HeterogeneousBodyEachHasKind is the
// closest sibling pattern.
func TestLogStep_MarshalJSON_InSlice(t *testing.T) {
	nodes := []Node{
		&LogStep{Level: "info", Msg: "a"},
		&LogStep{Level: "warn", Msg: "b"},
	}
	b, err := json.Marshal(nodes)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"kind":"LogStep"`)
	assert.Contains(t, string(b), `"level":"info"`)
	assert.Contains(t, string(b), `"level":"warn"`)
	assert.Contains(t, string(b), `"msg":"a"`)
	assert.Contains(t, string(b), `"msg":"b"`)
}

func TestMarshal_Stable_ActionRefWithKwargs(t *testing.T) {
	// Even with Starlark-dict-backed kwargs, marshaling must be stable —
	// Go's encoding/json sorts map keys, so converting Kwargs to map[string]any
	// before encoding produces deterministic output.
	d := starlark.NewDict(3)
	require.NoError(t, d.SetKey(starlark.String("z"), starlark.String("1")))
	require.NoError(t, d.SetKey(starlark.String("a"), starlark.String("2")))
	require.NoError(t, d.SetKey(starlark.String("m"), starlark.String("3")))
	a := &ActionRef{Kind_: "ext.op", Kwargs: d}

	b1, err := json.Marshal(a)
	require.NoError(t, err)
	b2, err := json.Marshal(a)
	require.NoError(t, err)
	assert.Equal(t, string(b1), string(b2))

	// Smoke check: keys appear sorted in the JSON
	assert.Contains(t, string(b1), `"kwargs":{"a":"2","m":"3","z":"1"}`)
}
