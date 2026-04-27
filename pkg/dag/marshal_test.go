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
