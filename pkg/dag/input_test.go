package dag

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Phase 3 (D3-04, D3-05): WorkflowInput is now {FlowName, ContentHash, InitState}.
//
// Phase 1 carried `Flow *Flow` + `Lambdas map[string]*CapturedLambda` and
// needed a custom MarshalJSON to omit *starlark.Function values. Phase 3's
// shape has no embedded Flow and no lambdas — the worker looks up the parsed
// flow + lambda map from its in-memory registry by (FlowName, ContentHash).
// No custom MarshalJSON needed; the default encoding/json output is stable
// because every field is a primitive or a map[string]any (sorted by encoding/json).
// =============================================================================

// TestWorkflowInput_RoundTrip asserts json.Marshal followed by json.Unmarshal
// reconstructs an equal struct — the basic durability contract for the
// payload SkytimeWorkflow receives.
func TestWorkflowInput_RoundTrip(t *testing.T) {
	in := &WorkflowInput{
		FlowName:    "approve_pr",
		ContentHash: "abcd1234",
		InitState: map[string]any{
			"repo": "foo/bar",
			"pr":   float64(42), // JSON numbers come back as float64 — match here for equality
		},
	}

	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out WorkflowInput
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in.FlowName, out.FlowName)
	assert.Equal(t, in.ContentHash, out.ContentHash)
	assert.Equal(t, in.InitState, out.InitState)
}

// TestWorkflowInput_NoFlowField asserts the marshaled JSON has EXACTLY three
// top-level keys: content_hash, flow_name, init_state. The Phase 1 fields
// `flow`, `lambdas`, `lambda_ids` are gone (D3-04, D3-05).
func TestWorkflowInput_NoFlowField(t *testing.T) {
	in := &WorkflowInput{
		FlowName:    "f",
		ContentHash: "deadbeef",
		InitState:   map[string]any{"k": "v"},
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))

	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	assert.Equal(t, []string{"content_hash", "flow_name", "init_state"}, keys,
		"WorkflowInput JSON must have exactly {content_hash, flow_name, init_state}")

	// Phase 1 fields must NOT appear.
	_, hasFlow := got["flow"]
	assert.False(t, hasFlow, "Phase 1 'flow' field must be removed (D3-04)")
	_, hasLambdas := got["lambdas"]
	assert.False(t, hasLambdas, "Phase 1 'lambdas' field must be removed (D3-04)")
	_, hasLambdaIDs := got["lambda_ids"]
	assert.False(t, hasLambdaIDs, "Phase 1 'lambda_ids' field must be removed (D3-04)")
}

// TestWorkflowInput_EmptyInitState confirms a nil InitState marshals to JSON
// `null` (the natural map[string]any(nil) encoding). Documents the behavior
// rather than relying on an "obvious" default.
func TestWorkflowInput_EmptyInitState(t *testing.T) {
	in := &WorkflowInput{
		FlowName:    "f",
		ContentHash: "deadbeef",
		InitState:   nil,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	// nil map marshals to JSON null — encoding/json convention.
	assert.Nil(t, got["init_state"], "nil InitState marshals to JSON null")
}
