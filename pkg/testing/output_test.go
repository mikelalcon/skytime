package testing

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// TestOutput_MarshalsAsValueMapDirectly pins the Open Q4 acceptance:
// the JSON wire format of MockOperationOutput is the value-map
// directly, with NO wrapper field name. Lets the wire shape be
// indistinguishable from a real extension Output once round-tripped
// through Temporal's JSON DataConverter into RawOperationOutput.
func TestOutput_MarshalsAsValueMapDirectly(t *testing.T) {
	out := MockOperationOutput{Value: map[string]any{"login": "octocat"}}
	b, err := json.Marshal(out)
	require.NoError(t, err)
	assert.JSONEq(t, `{"login":"octocat"}`, string(b))
}

// TestOutput_RoundTripsThroughRawOperationOutput verifies the bytes
// emitted by MockOperationOutput.MarshalJSON drop into a
// RawOperationOutput.Bytes byte-for-byte (modulo JSON-equivalent
// reordering at the encoder level), preserving the existing OkResult
// JSON wire shape.
func TestOutput_RoundTripsThroughRawOperationOutput(t *testing.T) {
	out := MockOperationOutput{Value: map[string]any{"x": 1}}
	expected, err := json.Marshal(out)
	require.NoError(t, err)

	raw := dag.RawOperationOutput{Bytes: expected}
	assert.JSONEq(t, `{"x":1}`, string(raw.Bytes))
}

// TestOutput_SatisfiesOperationOutputMarker is a compile-time check —
// if MockOperationOutput drifts off the marker, the package fails to
// build. The runtime body is intentionally empty.
func TestOutput_SatisfiesOperationOutputMarker(t *testing.T) {
	var _ dag.OperationOutput = MockOperationOutput{}
}

// TestOutput_NilValueEmitsEmptyObject — defensive: a zero-valued
// MockOperationOutput (no Value field set) marshals as `{}` so the
// wire shape is always a JSON object even for empty mocks.
func TestOutput_NilValueEmitsEmptyObject(t *testing.T) {
	var out MockOperationOutput
	b, err := json.Marshal(out)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(b))
}
