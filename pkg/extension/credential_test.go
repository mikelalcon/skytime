package extension

import (
	"encoding/json"
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
	c := &BearerCredential{ID_: "admin", Token: NewSecret(secret)}

	assert.Equal(t, "<credential:bearer:admin>", c.String())
	assert.Equal(t, "admin", c.ID())

	// Every fmt verb (including %+v / %#v which would bypass String() on
	// the pointer-to-struct) must redact thanks to the Secret field type
	// (D2-08 closes the gap that Phase 1's redacted String() alone left).
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		got := fmt.Sprintf(verb, c)
		assert.NotContains(t, got, secret,
			"verb %q LEAKED bearer token: %s", verb, got)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		got := fmt.Sprintf(verb, *c) // value receiver — a common slog payload
		assert.NotContains(t, got, secret,
			"value-receiver verb %q LEAKED bearer token: %s", verb, got)
	}
}

// TestBasicCredential_RedactedString — same redaction contract for Password.
func TestBasicCredential_RedactedString(t *testing.T) {
	const user = "service-account"
	const pass = "p@ssw0rd-do-not-leak"
	c := &BasicCredential{ID_: "github-oauth", User: user, Password: NewSecret(pass)}

	assert.Equal(t, "<credential:basic:github-oauth>", c.String())
	assert.Equal(t, "github-oauth", c.ID())

	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		got := fmt.Sprintf(verb, c)
		assert.NotContains(t, got, pass,
			"verb %q LEAKED password: %s", verb, got)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		got := fmt.Sprintf(verb, *c)
		assert.NotContains(t, got, pass,
			"value-receiver verb %q LEAKED password: %s", verb, got)
	}
}

// TestAPIKeyCredential_RedactedString — same redaction contract for the Key.
func TestAPIKeyCredential_RedactedString(t *testing.T) {
	const key = "sk_live_do_not_leak_xyz123"
	c := &APIKeyCredential{ID_: "stripe", Key: NewSecret(key), HeaderName: "Authorization"}

	assert.Equal(t, "<credential:apikey:stripe>", c.String())
	assert.Equal(t, "stripe", c.ID())

	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		got := fmt.Sprintf(verb, c)
		assert.NotContains(t, got, key,
			"verb %q LEAKED API key: %s", verb, got)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		got := fmt.Sprintf(verb, *c)
		assert.NotContains(t, got, key,
			"value-receiver verb %q LEAKED API key: %s", verb, got)
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
		{"bearer", &BearerCredential{ID_: "b1", Token: NewSecret("t")}, "b1"},
		{"basic", &BasicCredential{ID_: "b2", User: "u", Password: NewSecret("p")}, "b2"},
		{"apikey", &APIKeyCredential{ID_: "a1", Key: NewSecret("k"), HeaderName: "X-Key"}, "a1"},
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

// ============================================================================
// Phase 2 D2-08: Secret-typed fields verified end-to-end on each kind.
// ============================================================================

// TestBearerCredential_SecretField confirms Token is type Secret at compile
// time AND that .Reveal() round-trips at runtime. The compile-time guarantee
// comes from the constructor below — if Token were still string, the
// assignment `Token: NewSecret(...)` would not compile.
func TestBearerCredential_SecretField(t *testing.T) {
	const secret = "ghp_distinct_xyz_for_test"
	c := &BearerCredential{ID_: "admin", Token: NewSecret(secret)}

	// Type-level check (compile time): the Token field IS a Secret —
	// asserting via the .Reveal() method (only exists on Secret).
	assert.Equal(t, secret, c.Token.Reveal())

	// Format-level check: %+v on the pointer (and value) does NOT leak.
	assert.NotContains(t, fmt.Sprintf("%+v", c), secret)
	assert.NotContains(t, fmt.Sprintf("%+v", *c), secret)
	assert.Contains(t, fmt.Sprintf("%+v", *c), redactedString)
}

// TestBasicCredential_SecretField — same shape for Password.
func TestBasicCredential_SecretField(t *testing.T) {
	const pass = "p@ssw0rd-distinct-for-test"
	c := &BasicCredential{ID_: "svc", User: "alice", Password: NewSecret(pass)}

	assert.Equal(t, pass, c.Password.Reveal())
	assert.NotContains(t, fmt.Sprintf("%+v", c), pass)
	assert.NotContains(t, fmt.Sprintf("%+v", *c), pass)
	assert.Contains(t, fmt.Sprintf("%+v", *c), redactedString)
}

// TestAPIKeyCredential_SecretField — same shape for Key.
func TestAPIKeyCredential_SecretField(t *testing.T) {
	const key = "sk_distinct_xyz_for_test"
	c := &APIKeyCredential{ID_: "stripe", Key: NewSecret(key), HeaderName: "Authorization"}

	assert.Equal(t, key, c.Key.Reveal())
	assert.NotContains(t, fmt.Sprintf("%+v", c), key)
	assert.NotContains(t, fmt.Sprintf("%+v", *c), key)
	assert.Contains(t, fmt.Sprintf("%+v", *c), redactedString)
}

// TestCredentials_RedactedInAllFormats walks each Credential kind and each
// realistic formatting/serialization path that Phase 2 / Phase 3 might
// exercise, asserting NO path reveals the underlying secret. This is the
// integration-level equivalent of secret_test.go's TestSecret_FullRedactionMatrix
// — proves the Secret wrapper protects the surrounding struct, not just
// itself.
//
// Paths covered: %v, %+v, %#v, %s, %q on pointer + value receiver, plus
// json.Marshal of the struct.
func TestCredentials_RedactedInAllFormats(t *testing.T) {
	const distinctive = "DISTINCTIVE_TOKEN_VALUE_ABC123"

	cases := []struct {
		name string
		cred any
	}{
		{
			"bearer",
			&BearerCredential{ID_: "admin", Token: NewSecret(distinctive)},
		},
		{
			"basic",
			&BasicCredential{ID_: "svc", User: "u", Password: NewSecret(distinctive)},
		},
		{
			"apikey",
			&APIKeyCredential{ID_: "stripe", Key: NewSecret(distinctive), HeaderName: "X-K"},
		},
	}
	verbs := []string{"%v", "%+v", "%#v", "%s", "%q"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, verb := range verbs {
				got := fmt.Sprintf(verb, tc.cred)
				assert.NotContainsf(t, got, distinctive,
					"%s pointer-receiver via %q LEAKED: %s", tc.name, verb, got)
			}
			// Also check value-receiver formats — slog and many error
			// wrappers dereference automatically.
			vr := dereferenceForFormat(tc.cred)
			for _, verb := range verbs {
				got := fmt.Sprintf(verb, vr)
				assert.NotContainsf(t, got, distinctive,
					"%s value-receiver via %q LEAKED: %s", tc.name, verb, got)
			}

			// JSON path — the Temporal default DataConverter.
			b, err := json.Marshal(tc.cred)
			require.NoError(t, err)
			assert.NotContainsf(t, string(b), distinctive,
				"%s json.Marshal LEAKED: %s", tc.name, string(b))
		})
	}
}

// dereferenceForFormat returns the value behind a *T pointer. Used so the
// table test above can exercise value-receiver formatting verbs that
// downstream callers (slog, errors) commonly trigger.
func dereferenceForFormat(c any) any {
	switch v := c.(type) {
	case *BearerCredential:
		return *v
	case *BasicCredential:
		return *v
	case *APIKeyCredential:
		return *v
	}
	return c
}
