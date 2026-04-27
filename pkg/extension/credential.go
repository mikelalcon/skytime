package extension

// Credential is a sealed interface — the unexported isCredential() method
// prevents downstream packages from declaring new credential kinds. Adding a
// kind requires editing this file (a deliberate API-evolution gate).
//
// Phase 1 plan 03 task 2 expands this file with three concrete kinds:
// BearerCredential, BasicCredential, APIKeyCredential. Each kind has a
// redacted String() that NEVER includes the secret material — Pitfall #6
// defense in depth.
type Credential interface {
	// ID returns the credential identifier (e.g. "admin"). Workflow state
	// holds only this ID, never the resolved secret (PROJECT.md security
	// constraint).
	ID() string

	// isCredential is the unexported seal. Downstream packages cannot
	// satisfy Credential; new kinds must be added here.
	isCredential()
}
