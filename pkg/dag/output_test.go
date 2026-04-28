package dag

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeOutput is a test-local type that implements OperationOutput. The
// `IsOperationOutput()` method is the marker — only types that explicitly
// declare it satisfy the interface.
type fakeOutput struct {
	Field string
}

func (fakeOutput) IsOperationOutput() {}

// Compile-time assertion: fakeOutput satisfies OperationOutput. If
// OperationOutput's method set drifts, this assignment fails to compile.
var _ OperationOutput = fakeOutput{}

// TestOperationOutput_TypeImplementingMarkerSatisfiesInterface — happy path:
// a concrete type with IsOperationOutput() satisfies OperationOutput at
// runtime as well as compile time.
func TestOperationOutput_TypeImplementingMarkerSatisfiesInterface(t *testing.T) {
	var o OperationOutput = fakeOutput{Field: "x"}
	assert.NotNil(t, o)

	// Type switch must recognize the concrete type — Phase 3's interpreter
	// pattern.
	switch v := o.(type) {
	case fakeOutput:
		assert.Equal(t, "x", v.Field)
	default:
		t.Fatalf("unexpected concrete type %T", o)
	}
}

// TestOperationOutput_MarkerEnforcesExplicitOptIn documents the marker
// property at the test level. The exported IsOperationOutput method on
// the interface means any type that wants to satisfy OperationOutput must
// explicitly declare the method — this is a deliberate opt-in code change
// reviewers can spot.
//
// COMPILE-TIME ASSERTION (intentionally commented; un-comment to verify
// the marker still rejects naive types):
//
//	type unmarkedFoo struct{}
//	var _ OperationOutput = unmarkedFoo{}  // <-- must NOT compile
//
// If the line above compiled, the marker would be broken. The compiler
// error is "cannot use unmarkedFoo{} ... missing method IsOperationOutput".
func TestOperationOutput_MarkerEnforcesExplicitOptIn(t *testing.T) {
	// Positive runtime check paired with the documented compile-time
	// assertion above. Confirms the marker works for a type that DOES
	// implement it.
	var o OperationOutput = fakeOutput{}
	assert.Implements(t, (*OperationOutput)(nil), o)
}
