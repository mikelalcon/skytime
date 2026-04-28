package testing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
	exttesting "github.com/mikelalcon/skytime/pkg/extension/testing"
)

// TestFakeCredentialHandler_ImplementsCredentialHandler is a compile-time
// assertion that *FakeCredentialHandler satisfies extension.CredentialHandler.
// If this stops compiling, FakeCredentialHandler.Resolve has drifted from
// the contract.
func TestFakeCredentialHandler_ImplementsCredentialHandler(t *testing.T) {
	var _ extension.CredentialHandler = (*exttesting.FakeCredentialHandler)(nil)

	// Also exercise the runtime assignment so go vet sees the var as used.
	var h extension.CredentialHandler = &exttesting.FakeCredentialHandler{}
	require.NotNil(t, h)
}

// TestFakeCredentialHandler_Hit verifies Resolve returns the registered
// credential for a known ID without an error.
//
// Construction note: BearerCredential is built via the Phase-1 zero-value
// + ID_ shape so this test compiles against both Task 1 (Token still
// string-typed) and Task 2 (Token becomes Secret). The handler's contract
// is "store and return without re-wrapping" — Token's internal type is
// covered by credential_test.go.
func TestFakeCredentialHandler_Hit(t *testing.T) {
	bearer := &extension.BearerCredential{ID_: "admin"}
	h := &exttesting.FakeCredentialHandler{
		Creds: map[string]extension.Credential{
			"admin": bearer,
		},
	}

	got, err := h.Resolve(context.Background(), "admin")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "admin", got.ID())

	// Round-trip identity: the handler must return the SAME pointer it
	// stored — no copying, no re-wrapping. Pointer equality proves it.
	gotBearer, ok := got.(*extension.BearerCredential)
	require.True(t, ok, "expected *BearerCredential, got %T", got)
	assert.Same(t, bearer, gotBearer,
		"FakeCredentialHandler must return the stored credential by identity")
}

// TestFakeCredentialHandler_Miss verifies Resolve returns an error wrapping
// extension.ErrUnknownCredential when the ID is absent. errors.Is must
// detect the sentinel — D2-12 classification depends on this.
func TestFakeCredentialHandler_Miss(t *testing.T) {
	h := &exttesting.FakeCredentialHandler{
		Creds: map[string]extension.Credential{}, // empty
	}

	got, err := h.Resolve(context.Background(), "absent")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, extension.ErrUnknownCredential),
		"missing ID must wrap ErrUnknownCredential so D2-12 classifies as NonRetryable")
	assert.Contains(t, err.Error(), "absent",
		"error message must mention the missing ID for debuggability")
}

// TestFakeCredentialHandler_NilCredsMap verifies a zero-value
// FakeCredentialHandler{} (no Creds initialization) still produces a
// well-formed ErrUnknownCredential-wrapped error for any lookup. Callers
// that forget to populate Creds get a useful error, not a panic.
func TestFakeCredentialHandler_NilCredsMap(t *testing.T) {
	h := &exttesting.FakeCredentialHandler{} // zero value: Creds is nil

	got, err := h.Resolve(context.Background(), "anything")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, extension.ErrUnknownCredential),
		"nil Creds map must still produce an ErrUnknownCredential-wrapped error")
}
