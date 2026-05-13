package testing

import (
	"context"
	"fmt"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// WithCredentialHandler installs a CredentialHandler on the test
// harness (CLI-10). Used by:
//
//   - pkg/cli/test.go to thread the binary's production handler into
//     `skytime test` so the same Resolve() path serves all four
//     subcommands (run / validate / server / test).
//   - Test authors who want to drive a partial-mock test using a real
//     credential handler PLUS testsuite.OnActivity mocks for the
//     network-touching activities. Per D-7.4-11, OnActivity short-circuits
//     before any handler.Resolve() call, so the handler is invoked ONLY
//     by activities that aren't mocked. This lets one test exercise the
//     real credential-resolution path while still mocking the GitHub /
//     Slack / etc. surface.
//
// Per-call scope (same idiom as pkg/testing.WithExtensions and
// pkg/cli.WithCredentialHandler): the option mutates the runConfig for
// a single Run / RunCLI invocation. No globals.
//
// The Fake constructor below (NewFakeCredentialHandler) is the
// recommended shorthand for the common case where tests need a
// credential map literal without writing a custom handler type.
func WithCredentialHandler(h extension.CredentialHandler) Option {
	return func(c *runConfig) error {
		c.credHandler = h
		return nil
	}
}

// NewFakeCredentialHandler returns a CredentialHandler backed by the
// supplied id→credential map (D-7.4-10). The map is COPIED at
// construction; mutations to the caller's map after this call do not
// affect the handler.
//
// Behavior matches credfile.Resolver: unknown IDs return an error that
// wraps extension.ErrUnknownCredential so the activity classifier
// (pkg/activity/classify.go) treats failures as NonRetryable — same
// shape production sees.
//
// Typical usage:
//
//	h := testing.NewFakeCredentialHandler(map[string]extension.Credential{
//	    "gh-token": &extension.BearerCredential{ID_: "gh-token", Token: extension.NewSecret("test-pat")},
//	})
//	testing.RunCLI(dir, testing.WithExtensions(...), testing.WithCredentialHandler(h))
func NewFakeCredentialHandler(creds map[string]extension.Credential) extension.CredentialHandler {
	// Defensive copy: detach the handler from caller-side map mutations.
	cp := make(map[string]extension.Credential, len(creds))
	for k, v := range creds {
		cp[k] = v
	}
	return &fakeCredentialHandler{creds: cp}
}

// fakeCredentialHandler is the map-backed test double returned by
// NewFakeCredentialHandler. Read-only after construction; safe for
// concurrent Resolve() calls without a mutex.
type fakeCredentialHandler struct {
	creds map[string]extension.Credential
}

// Resolve implements extension.CredentialHandler with the same
// unknown-ID semantics as credfile.Resolver: wraps
// extension.ErrUnknownCredential so callers' errors.Is checks succeed.
func (h *fakeCredentialHandler) Resolve(_ context.Context, id string) (extension.Credential, error) {
	cred, ok := h.creds[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s (fake credential handler)", extension.ErrUnknownCredential, id)
	}
	return cred, nil
}

// Compile-time interface check.
var _ extension.CredentialHandler = (*fakeCredentialHandler)(nil)
