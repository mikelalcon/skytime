package extension

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Compile-time interface assertion — *fakeHandler MUST satisfy CredentialHandler.
// If this fails to compile, the Resolve signature drifted from the contract.
// ============================================================================

var _ CredentialHandler = (*fakeHandler)(nil)

// fakeHandler is a minimal in-memory CredentialHandler used by tests and
// (later) by the Phase 6 examples until a real handler is wired.
type fakeHandler struct {
	creds map[string]Credential
}

func (h *fakeHandler) Resolve(ctx context.Context, id string) (Credential, error) {
	if c, ok := h.creds[id]; ok {
		return c, nil
	}
	return nil, errCredentialNotFound{id: id}
}

type errCredentialNotFound struct{ id string }

func (e errCredentialNotFound) Error() string { return "credential not found: " + e.id }

func TestCredentialHandler_FakeReturnsKnownCredential(t *testing.T) {
	bearer := &BearerCredential{ID_: "admin", Token: "secret-do-not-leak"}
	h := &fakeHandler{creds: map[string]Credential{"admin": bearer}}

	got, err := h.Resolve(context.Background(), "admin")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "admin", got.ID())
	// Returned credential must round-trip back to the original concrete
	// type (handler does not re-wrap).
	bearerOut, ok := got.(*BearerCredential)
	require.True(t, ok, "expected *BearerCredential, got %T", got)
	assert.Equal(t, "secret-do-not-leak", bearerOut.Token)
}

func TestCredentialHandler_UnknownIDReturnsError(t *testing.T) {
	h := &fakeHandler{creds: map[string]Credential{}}
	got, err := h.Resolve(context.Background(), "unknown")
	require.Error(t, err)
	assert.Nil(t, got)

	var notFound errCredentialNotFound
	require.True(t, errors.As(err, &notFound), "expected errCredentialNotFound, got %T", err)
	assert.Equal(t, "unknown", notFound.id)
}

// TestCredentialHandler_ResolveTakesContext is a compile-time test of the
// CredentialHandler signature. The first parameter MUST be context.Context
// (stdlib), NEVER workflow.Context — Phase 3 wires this from inside an
// activity, where activity.Context() satisfies context.Context but
// workflow.Context does not exist.
func TestCredentialHandler_ResolveTakesContext(t *testing.T) {
	var h CredentialHandler = &fakeHandler{creds: map[string]Credential{}}
	// The literal context.Background() returns a context.Context — if
	// CredentialHandler.Resolve required workflow.Context, this would
	// fail to compile.
	_, _ = h.Resolve(context.Background(), "x")
}
