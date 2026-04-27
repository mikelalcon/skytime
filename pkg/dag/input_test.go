package dag

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowInput_MarshalJSON_OmitsLambdaFunctions(t *testing.T) {
	// MarshalJSON must not panic and must NOT serialize *starlark.Function
	// values. Lambda IDs surface as a sorted string list per Pitfall #3
	// (deterministic key order).
	pos := nodePos(t, 1, 1)
	w := &WorkflowInput{
		Flow:      &Flow{Pos: pos, Name: "f"},
		Lambdas:   map[string]*CapturedLambda{"zz:1:1": {}, "aa:2:2": {}},
		InitState: map[string]any{"k": "v"},
	}

	b, err := json.Marshal(w)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))

	// flow + init_state present
	require.Contains(t, got, "flow")
	require.Contains(t, got, "init_state")

	// Lambda IDs are emitted as a sorted slice
	idsAny, ok := got["lambda_ids"]
	require.True(t, ok, "lambda_ids field must be present")
	ids, ok := idsAny.([]any)
	require.True(t, ok)
	require.Len(t, ids, 2)
	assert.Equal(t, "aa:2:2", ids[0], "lambda_ids must be sorted")
	assert.Equal(t, "zz:1:1", ids[1])
}

func TestWorkflowInput_MarshalJSON_EmptyLambdas(t *testing.T) {
	pos := nodePos(t, 1, 1)
	w := &WorkflowInput{Flow: &Flow{Pos: pos, Name: "f"}}

	b, err := json.Marshal(w)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))

	idsAny, ok := got["lambda_ids"]
	require.True(t, ok)
	ids, ok := idsAny.([]any)
	require.True(t, ok)
	assert.Empty(t, ids, "empty Lambdas → empty (not nil/missing) lambda_ids list")
}
