package extension

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
