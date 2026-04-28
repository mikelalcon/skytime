package extension

import (
	"context"
	"errors"
)

// ErrUnknownCredential is the sentinel handlers wrap when a credential ID
// doesn't exist in their backing store. The activity (Phase 2) classifies
// errors satisfying errors.Is(err, ErrUnknownCredential) as NonRetryable
// (configuration bug; retrying won't help) per D2-12. Other handler errors
// are treated as transient and classified as Retryable.
//
// Convention for handler authors:
//
//	return nil, fmt.Errorf("%w: %s", extension.ErrUnknownCredential, id)
//
// Decision reference: D2-12 (locked).
var ErrUnknownCredential = errors.New("unknown credential")

// CredentialHandler is the contract Phase 3 wires when registering the worker.
// Skytime's `worker.Run(client, flowDir, skytime.WithCredentialHandler(...))`
// (Phase 3 land) takes a CredentialHandler option; Phase 1 declares the
// interface so Phase 2's generic activity can call Resolve and Phase 6's
// example handlers can implement it.
//
// LIFECYCLE: Resolve is called JUST IN TIME inside the activity (D-08), never
// at parse time. Workflow state and ActionRef carry only the credential ID
// (string); the secret only exists in the activity's heap for the duration
// of one operation invocation.
//
// THREAD SAFETY: implementations MUST be safe for concurrent calls — Phase 2's
// activity may resolve credentials in parallel within a block.
type CredentialHandler interface {
	// Resolve looks up the credential by ID and returns the typed
	// Credential. Returns an error if the ID is unknown or the lookup
	// fails (e.g., backing store unreachable).
	//
	// The first parameter is context.Context (stdlib), NEVER
	// workflow.Context — this method runs inside an activity, where
	// activity.Context() satisfies context.Context.
	//
	// On unknown IDs, return an error wrapping ErrUnknownCredential so the
	// activity classifies the failure as non-retryable (D2-12).
	Resolve(ctx context.Context, id string) (Credential, error)
}
