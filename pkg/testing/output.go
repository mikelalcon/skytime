package testing

import (
	"encoding/json"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// MockOperationOutput wraps an arbitrary mock-returned value so it can
// satisfy dag.OperationOutput and round-trip through Temporal's JSON
// DataConverter. Per Open Question 4 (RESEARCH.md), the JSON wire
// format is the value-map directly — NO wrapper field name — so the
// shape is indistinguishable from a typed extension Output once it
// lands as RawOperationOutput on the activity-result side.
//
// For non-map values (lists, scalars), Plan 02's router wraps the
// value into a single-key map {"value": v} before assembling
// MockOperationOutput so MarshalJSON always emits a JSON object.
//
// D5-C3 + RESEARCH.md Investigation 5.
type MockOperationOutput struct {
	Value map[string]any
}

// IsOperationOutput satisfies the dag.OperationOutput marker.
func (MockOperationOutput) IsOperationOutput() {}

// MarshalJSON emits the value-map directly (no wrapper field).
// Compile-time assertion that we satisfy the dag interface:
//
//	var _ dag.OperationOutput = MockOperationOutput{}
//
// is below the type so build breaks if the marker drifts.
func (m MockOperationOutput) MarshalJSON() ([]byte, error) {
	if m.Value == nil {
		// Empty map — emit {} so the wire shape is always a JSON
		// object (RawOperationOutput.Bytes will hold {}).
		return []byte("{}"), nil
	}
	return json.Marshal(m.Value)
}

var _ dag.OperationOutput = MockOperationOutput{}
