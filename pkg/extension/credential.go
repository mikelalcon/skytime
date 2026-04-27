package extension

import "fmt"

// Credential is a sealed interface — the unexported isCredential() method
// prevents downstream packages from declaring new credential kinds. Adding a
// kind requires editing this file (a deliberate API-evolution gate).
//
// Each concrete kind has a redacted String() method that NEVER includes the
// secret material. Pitfall #6 (credential leakage via logs/error messages)
// is defended at the type level: %v, %s, %q formatting verbs all route
// through String() when invoked on a *T receiver.
//
// Note: %+v formatting bypasses String() and shows struct fields verbatim —
// callers MUST NOT log credentials with %+v. Phase 2's error scrubber
// catches this if a caller misuses %+v in an error path.
type Credential interface {
	// ID returns the credential identifier (e.g. "admin"). Workflow state
	// holds only this ID, never the resolved secret (PROJECT.md security
	// constraint).
	ID() string

	// isCredential is the unexported seal. Downstream packages cannot
	// satisfy Credential; new kinds must be added in this file.
	isCredential()
}

// ----------------------------------------------------------------------------
// BearerCredential — `Authorization: Bearer <Token>` style auth.
// ----------------------------------------------------------------------------

// BearerCredential carries a bearer token (e.g. GitHub PAT, OAuth access token).
// The Token field is the secret; String() never includes it.
type BearerCredential struct {
	// ID_ is the credential identifier as Starlark sees it (e.g. "admin").
	// Suffixed with `_` to leave the more-natural ID() method name free.
	ID_ string
	// Token is the bearer secret. NEVER log this directly.
	Token string
}

// ID returns the credential identifier.
func (b *BearerCredential) ID() string { return b.ID_ }

// String returns the redacted form `<credential:bearer:<id>>`. NEVER the token.
func (b *BearerCredential) String() string {
	return fmt.Sprintf("<credential:bearer:%s>", b.ID_)
}

// isCredential is the seal — see Credential interface doc.
func (*BearerCredential) isCredential() {}

// ----------------------------------------------------------------------------
// BasicCredential — `Authorization: Basic <base64(User:Password)>` style.
// ----------------------------------------------------------------------------

// BasicCredential carries a username + password pair (e.g. HTTP Basic auth).
// The User and Password fields are both treated as secret; String() never
// includes either.
type BasicCredential struct {
	// ID_ is the credential identifier as Starlark sees it.
	ID_ string
	// User is the basic-auth username. Treated as secret in redaction.
	User string
	// Password is the basic-auth password. NEVER log this directly.
	Password string
}

// ID returns the credential identifier.
func (b *BasicCredential) ID() string { return b.ID_ }

// String returns the redacted form `<credential:basic:<id>>`. NEVER the
// User or Password.
func (b *BasicCredential) String() string {
	return fmt.Sprintf("<credential:basic:%s>", b.ID_)
}

// isCredential is the seal — see Credential interface doc.
func (*BasicCredential) isCredential() {}

// ----------------------------------------------------------------------------
// APIKeyCredential — custom-header API-key auth (e.g. `X-API-Key: <Key>`).
// ----------------------------------------------------------------------------

// APIKeyCredential carries an API key + the header name it should be sent
// under. Both Key and HeaderName are treated as redaction-eligible — defense
// in depth; future readers should not infer "what's in String() is fine to
// log" from a partial redaction.
type APIKeyCredential struct {
	// ID_ is the credential identifier as Starlark sees it.
	ID_ string
	// Key is the API key value. NEVER log this directly.
	Key string
	// HeaderName is the HTTP header the key is sent under
	// (e.g. "X-API-Key", "Authorization"). Not strictly secret but
	// redacted from String() anyway.
	HeaderName string
}

// ID returns the credential identifier.
func (a *APIKeyCredential) ID() string { return a.ID_ }

// String returns the redacted form `<credential:apikey:<id>>`. NEVER the
// Key or HeaderName.
func (a *APIKeyCredential) String() string {
	return fmt.Sprintf("<credential:apikey:%s>", a.ID_)
}

// isCredential is the seal — see Credential interface doc.
func (*APIKeyCredential) isCredential() {}
