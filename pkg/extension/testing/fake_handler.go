// Package testing provides reusable test doubles for the extension contract.
// Imported as `extensiontesting` (or any local alias) to avoid collision
// with the standard library's `testing` package.
//
// Layout note: this is a sub-package of pkg/extension, NOT an `internal/`
// directory. Phase 2's pkg/activity tests, Phase 5's E2E harness, and
// Phase 6's example projects all need to import these helpers.
//
// FakeCredentialHandler is the in-memory CredentialHandler used across
// pkg/activity, future Phase 5 E2E harness, and Phase 6 example tests.
package testing

import (
	"context"
	"fmt"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// FakeCredentialHandler is an in-memory CredentialHandler keyed by
// credential ID. Missing IDs return an error wrapping
// extension.ErrUnknownCredential so the activity classifies the failure as
// non-retryable per D2-12.
//
// The Creds map is exposed (not via constructor) so test setup is one-line:
//
//	h := &FakeCredentialHandler{Creds: map[string]extension.Credential{
//	    "admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("ghp_xxx")},
//	}}
//
// Concurrent reads from Resolve are safe because Go's map reads are safe
// when no goroutine writes — tests should populate Creds once and not
// mutate after.
type FakeCredentialHandler struct {
	// Creds is the in-memory ID → Credential map. nil is treated as empty.
	Creds map[string]extension.Credential
}

// Compile-time check that FakeCredentialHandler satisfies the contract.
// Drift in CredentialHandler's signature would break this assignment at
// compile time, surfacing the contract violation immediately.
var _ extension.CredentialHandler = (*FakeCredentialHandler)(nil)

// Resolve returns the credential keyed by id, or an error wrapping
// extension.ErrUnknownCredential when the ID is not present (or Creds is
// nil). The wrap follows the documented handler-author convention:
//
//	fmt.Errorf("%w: %s", extension.ErrUnknownCredential, id)
func (h *FakeCredentialHandler) Resolve(_ context.Context, id string) (extension.Credential, error) {
	if h.Creds == nil {
		return nil, fmt.Errorf("%w: %s", extension.ErrUnknownCredential, id)
	}
	c, ok := h.Creds[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", extension.ErrUnknownCredential, id)
	}
	return c, nil
}
