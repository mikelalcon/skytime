package extension

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// opTestOutput is a test-local OperationOutput-implementing type used by
// the OperationFunc signature tests below. The marker method
// IsOperationOutput is exported (see pkg/dag/output.go SEAL PROPERTY) so
// types in non-pkg/dag packages CAN satisfy the marker — this is precisely
// what extension authors do in pkg/examples/* (Phase 6).
type opTestOutput struct{}

func (opTestOutput) IsOperationOutput() {}

// TestOperationSpec_IdempotentIsPointerToBool verifies — via reflection — that
// OperationSpec.Idempotent has Go type *bool. This is the D-12 enforcement
// surface: a *bool field forces extension authors to write either
// extension.Ptr(true) or extension.Ptr(false) at the registration site; the
// nil case ("forgot to declare") is then detectable at Registry.Register
// time. A bare `bool` (zero-value false) would silently default and the
// registry could not distinguish "not set" from "explicitly false".
func TestOperationSpec_IdempotentIsPointerToBool(t *testing.T) {
	tp := reflect.TypeOf(OperationSpec{})
	field, ok := tp.FieldByName("Idempotent")
	require.True(t, ok, "OperationSpec must have an Idempotent field")
	assert.Equal(t, reflect.Ptr, field.Type.Kind(), "Idempotent must be a pointer type")
	assert.Equal(t, reflect.Bool, field.Type.Elem().Kind(), "Idempotent must be *bool, got *%s", field.Type.Elem().Kind())
}

// TestOperationSpec_ZeroValueIdempotentIsNil verifies a freshly constructed
// OperationSpec (zero value) has nil Idempotent. This is the "author forgot
// to declare" case the registry must reject — see registry_test.go.
func TestOperationSpec_ZeroValueIdempotentIsNil(t *testing.T) {
	var spec OperationSpec
	assert.Nil(t, spec.Idempotent, "zero-value OperationSpec must have nil Idempotent so the registry can detect missing declarations")
}

// TestPtr_BoolRoundtrip verifies the Ptr helper produces a *bool with the
// expected dereferenced value. The helper exists so registration sites can
// write `Idempotent: extension.Ptr(true)` ergonomically.
func TestPtr_BoolRoundtrip(t *testing.T) {
	tr := Ptr(true)
	require.NotNil(t, tr)
	assert.True(t, *tr)

	fl := Ptr(false)
	require.NotNil(t, fl)
	assert.False(t, *fl)
}

// TestPtr_GenericOverString verifies Ptr is generic over T (not bool-only).
// Useful for any future *T field on OperationSpec or downstream types.
func TestPtr_GenericOverString(t *testing.T) {
	s := Ptr[string]("hello")
	require.NotNil(t, s)
	assert.Equal(t, "hello", *s)
}

// TestOperationFunc_ReturnsOperationOutput verifies the D2-04 narrowing of
// OperationFunc's return type from `any` to `dag.OperationOutput`. The
// positive path is exercised by the assignment + invocation below: a
// function returning a typed Output that implements isOperationOutput()
// satisfies the OperationFunc type. The negative path (a function returning
// `(map[string]int{}, nil)`) is documented as a compile-time check that
// MUST NOT compile if the seal is intact:
//
//	// Intentionally won't compile if uncommented — proves the narrowing:
//	//   var bad OperationFunc = func(ctx context.Context, args any, cred Credential) (any, error) {
//	//       return map[string]int{}, nil
//	//   }
//
// The compile failure surface is the type system; this test simply asserts
// the positive path works at runtime.
func TestOperationFunc_ReturnsOperationOutput(t *testing.T) {
	var fn OperationFunc = func(ctx context.Context, args any, cred Credential) (dag.OperationOutput, error) {
		return opTestOutput{}, nil
	}
	require.NotNil(t, fn)

	out, err := fn(context.Background(), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, out)
	_, ok := out.(opTestOutput)
	assert.True(t, ok, "OperationFunc must return a dag.OperationOutput-implementing type")

	// Returning nil output is also legal — the nil interface value
	// satisfies any interface.
	var nilFn OperationFunc = func(ctx context.Context, args any, cred Credential) (dag.OperationOutput, error) {
		return nil, nil
	}
	out, err = nilFn(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Nil(t, out, "OperationFunc may return nil output for void operations")
}

// TestOperationSpec_DefaultTimeoutZeroIsNoTimeout verifies the D2-15
// DefaultTimeout field's zero-value semantics: 0 means "no per-action
// timeout enforced." The field type is time.Duration; an unset zero value
// composes cleanly into a default-constructed OperationSpec.
func TestOperationSpec_DefaultTimeoutZeroIsNoTimeout(t *testing.T) {
	tp := reflect.TypeOf(OperationSpec{})
	field, ok := tp.FieldByName("DefaultTimeout")
	require.True(t, ok, "OperationSpec must have a DefaultTimeout field")
	assert.Equal(t, reflect.TypeOf(time.Duration(0)), field.Type,
		"DefaultTimeout must be time.Duration")

	// Construction with explicit and zero timeout values both succeed.
	spec := OperationSpec{
		Name:           "x",
		Idempotent:     Ptr(true),
		Func:           func(ctx context.Context, args any, cred Credential) (dag.OperationOutput, error) { return nil, nil },
		KwargsType:     reflect.TypeOf(struct{}{}),
		DefaultTimeout: 30 * time.Second,
	}
	assert.Equal(t, 30*time.Second, spec.DefaultTimeout)

	zeroSpec := OperationSpec{
		Name:       "z",
		Idempotent: Ptr(false),
		Func:       func(ctx context.Context, args any, cred Credential) (dag.OperationOutput, error) { return nil, nil },
		KwargsType: reflect.TypeOf(struct{}{}),
		// DefaultTimeout intentionally omitted — zero value documented
		// to mean "no per-action timeout enforced" (D2-15).
	}
	assert.Equal(t, time.Duration(0), zeroSpec.DefaultTimeout,
		"zero DefaultTimeout means no per-action enforcement (D2-15)")

	// Both specs must register cleanly through the existing Registry.Register
	// path so the new field doesn't break Phase 1's registration tests.
	r := NewRegistry()
	require.NoError(t, r.Register(makeFakeExt("e1", map[string]*OperationSpec{"x": &spec})))
	require.NoError(t, r.Register(makeFakeExt("e2", map[string]*OperationSpec{"z": &zeroSpec})))
}
