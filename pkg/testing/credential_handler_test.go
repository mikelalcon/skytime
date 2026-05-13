package testing

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// TestWithCredentialHandler_SetsRunConfigField pins the Option closure
// → runConfig.credHandler assignment (CLI-10 surface contract).
func TestWithCredentialHandler_SetsRunConfigField(t *testing.T) {
	h := NewFakeCredentialHandler(map[string]extension.Credential{
		"gh-token": fakeBearer("gh-token", "test-pat"),
	})
	cfg := &runConfig{}
	require.NoError(t, WithCredentialHandler(h)(cfg))
	assert.Equal(t, h, cfg.credHandler,
		"WithCredentialHandler must store the handler verbatim on cfg.credHandler")
}

// TestNewFakeCredentialHandler_KnownIDReturnsCredential pins the
// happy-path lookup contract.
func TestNewFakeCredentialHandler_KnownIDReturnsCredential(t *testing.T) {
	want := fakeBearer("gh-token", "test-pat")
	h := NewFakeCredentialHandler(map[string]extension.Credential{
		"gh-token": want,
	})
	got, err := h.Resolve(context.Background(), "gh-token")
	require.NoError(t, err)
	assert.Equal(t, want, got, "Resolve(known) must return the mapped credential verbatim")
}

// TestNewFakeCredentialHandler_UnknownIDWrapsErrUnknownCredential pins
// the error-shape parity with credfile.Resolver: errors.Is(err,
// extension.ErrUnknownCredential) must be true so the activity
// classifier (pkg/activity/classify.go) treats the failure as
// NonRetryable in BOTH production and test paths.
func TestNewFakeCredentialHandler_UnknownIDWrapsErrUnknownCredential(t *testing.T) {
	h := NewFakeCredentialHandler(map[string]extension.Credential{})
	_, err := h.Resolve(context.Background(), "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, extension.ErrUnknownCredential),
		"unknown ID error must wrap extension.ErrUnknownCredential; got: %v", err)
	assert.Contains(t, err.Error(), "missing", "error message must include the unknown ID")
}

// TestNewFakeCredentialHandler_DefensiveMapCopy pins the construction-
// time copy: caller-side map mutations after the constructor returns
// MUST NOT affect the handler.
func TestNewFakeCredentialHandler_DefensiveMapCopy(t *testing.T) {
	original := fakeBearer("gh-token", "original-pat")
	source := map[string]extension.Credential{"gh-token": original}

	h := NewFakeCredentialHandler(source)

	// Mutate the source map AFTER construction.
	source["gh-token"] = fakeBearer("gh-token", "mutated-pat")
	delete(source, "gh-token")
	source["gh-token"] = nil

	// Handler still returns the original.
	got, err := h.Resolve(context.Background(), "gh-token")
	require.NoError(t, err)
	assert.Equal(t, original, got, "post-construction mutations to the source map must not affect the handler")
}

// TestNewFakeCredentialHandler_ConcurrentResolves pins the read-only
// safety: 100 goroutines hitting Resolve simultaneously must all
// succeed with the right value.
func TestNewFakeCredentialHandler_ConcurrentResolves(t *testing.T) {
	want := fakeBearer("gh-token", "concurrent-pat")
	h := NewFakeCredentialHandler(map[string]extension.Credential{
		"gh-token": want,
	})

	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := h.Resolve(context.Background(), "gh-token")
			if err != nil {
				errs <- err
				return
			}
			if got != want {
				errs <- errors.New("got != want")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Resolve failed: %v", err)
	}
}

// fakeBearer is a test helper for constructing BearerCredential values
// without coupling tests to the Secret constructor's exact API. Adjust
// the constructor body if extension.NewSecret signature evolves.
func fakeBearer(id, token string) *extension.BearerCredential {
	return &extension.BearerCredential{
		ID_:   id,
		Token: extension.NewSecret(token),
	}
}
