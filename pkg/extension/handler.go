package extension

import "context"

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
	Resolve(ctx context.Context, id string) (Credential, error)
}
