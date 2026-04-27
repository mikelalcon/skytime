package extension

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Compile-time interface assertions — each concrete kind MUST satisfy
// Credential. If these fail to compile, a kind drifted from the contract.
// ============================================================================

var (
	_ Credential = (*BearerCredential)(nil)
	_ Credential = (*BasicCredential)(nil)
	_ Credential = (*APIKeyCredential)(nil)
)

// TestBearerCredential_RedactedString verifies BearerCredential.String() returns
// the EXACT redacted format `<credential:bearer:<id>>` and NEVER includes the
// secret token. Pitfall #6 (credential leakage via logs/error messages) defense.
func TestBearerCredential_RedactedString(t *testing.T) {
	const secret = "ghp_super_secret_do_not_leak"
	c := &BearerCredential{ID_: "admin", Token: secret}

	assert.Equal(t, "<credential:bearer:admin>", c.String())
	assert.Equal(t, "admin", c.ID())

	// %v formatting routes through String() (pointer receiver):
	assert.NotContains(t, fmt.Sprintf("%v", c), secret)
	// %s formatting also routes through String():
	assert.NotContains(t, fmt.Sprintf("%s", c), secret)
	// Quoted formatting:
	assert.NotContains(t, fmt.Sprintf("%q", c), secret)
	// Note: %+v includes struct fields verbatim — that's the Go default and
	// would expose the token. Phase 2's error scrubber catches this if a
	// caller misuses %+v. We document the safe verbs (%s, %v, %q) here.
}

// TestBasicCredential_RedactedString — same redaction contract for User+Password.
func TestBasicCredential_RedactedString(t *testing.T) {
	const user = "service-account"
	const pass = "p@ssw0rd-do-not-leak"
	c := &BasicCredential{ID_: "github-oauth", User: user, Password: pass}

	assert.Equal(t, "<credential:basic:github-oauth>", c.String())
	assert.Equal(t, "github-oauth", c.ID())

	for _, v := range []string{
		fmt.Sprintf("%v", c),
		fmt.Sprintf("%s", c),
		fmt.Sprintf("%q", c),
	} {
		assert.NotContains(t, v, user, "User leaked through %%v/%%s/%%q")
		assert.NotContains(t, v, pass, "Password leaked through %%v/%%s/%%q")
	}
}

// TestAPIKeyCredential_RedactedString — same redaction contract for the Key.
func TestAPIKeyCredential_RedactedString(t *testing.T) {
	const key = "sk_live_do_not_leak_xyz123"
	c := &APIKeyCredential{ID_: "stripe", Key: key, HeaderName: "Authorization"}

	assert.Equal(t, "<credential:apikey:stripe>", c.String())
	assert.Equal(t, "stripe", c.ID())

	for _, v := range []string{
		fmt.Sprintf("%v", c),
		fmt.Sprintf("%s", c),
		fmt.Sprintf("%q", c),
	} {
		assert.NotContains(t, v, key, "Key leaked through %%v/%%s/%%q")
	}
	// HeaderName is not secret — it's a config value — but it should still
	// not appear in the redacted form (defense in depth: future readers
	// should not infer "what's in String() is fine to log").
	for _, v := range []string{
		fmt.Sprintf("%v", c),
		fmt.Sprintf("%s", c),
	} {
		assert.NotContains(t, v, "Authorization")
	}
}

// TestCredential_AllKindsExposeID verifies every concrete kind returns the
// ID_ field via ID(). The Credential interface is the lowest-common-denominator
// contract; ID() is the only piece extensions are guaranteed to read for
// telemetry/correlation.
func TestCredential_AllKindsExposeID(t *testing.T) {
	cases := []struct {
		name string
		cred Credential
		want string
	}{
		{"bearer", &BearerCredential{ID_: "b1", Token: "t"}, "b1"},
		{"basic", &BasicCredential{ID_: "b2", User: "u", Password: "p"}, "b2"},
		{"apikey", &APIKeyCredential{ID_: "a1", Key: "k", HeaderName: "X-Key"}, "a1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.cred.ID())
		})
	}
}

// TestCredential_SealedViaIsCredential is a documentation test: the sealed
// interface is enforced via the unexported isCredential() method. There is no
// runtime test that proves "no other type can satisfy Credential" — that's a
// compile-time guarantee. This test simply asserts the seal method exists on
// each known kind by calling it (the method is exported as a no-op via
// the receiver).
func TestCredential_SealedViaIsCredential(t *testing.T) {
	require.NotPanics(t, func() {
		(&BearerCredential{}).isCredential()
		(&BasicCredential{}).isCredential()
		(&APIKeyCredential{}).isCredential()
	})
}
