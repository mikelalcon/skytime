package extension

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// distinctiveSecret is the canary string used across every Secret formatter
// test. Distinctive enough that a leak through ANY format verb shows up
// clearly in failure output.
const distinctiveSecret = "super-secret-token-abc123"

// TestSecret_FullRedactionMatrix walks every fmt verb that could plausibly
// leak the underlying value and asserts:
//  1. each output contains the redacted sentinel "<redacted>", and
//  2. NONE of the outputs contain the raw secret bytes.
//
// %+v and %#v are the load-bearing cases (D2-08): without Secret.Format and
// GoString, %+v / %#v bypass String() and print struct fields verbatim.
func TestSecret_FullRedactionMatrix(t *testing.T) {
	s := NewSecret(distinctiveSecret)

	cases := []struct {
		name   string
		format string
	}{
		{"percent_v", "%v"},
		{"percent_plus_v", "%+v"},
		{"percent_hash_v", "%#v"},
		{"percent_s", "%s"},
		{"percent_q", "%q"},
		{"percent_d", "%d"},
		{"percent_x", "%x"},
		{"sprint", ""}, // fmt.Sprint, not Sprintf
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			if tc.format == "" {
				got = fmt.Sprint(s)
			} else {
				got = fmt.Sprintf(tc.format, s)
			}
			assert.Contains(t, got, redactedString,
				"verb %q must produce a redacted-marker output", tc.format)
			assert.NotContains(t, got, distinctiveSecret,
				"verb %q LEAKED the raw secret: %s", tc.format, got)
		})
	}

	// Embedded in a surrounding struct — the most common slog/log scenario.
	// %+v on a struct holding a Secret must redact via the field's Format
	// implementation, not via String() (which %+v would skip).
	type wrapper struct {
		ID    string
		Token Secret
	}
	w := wrapper{ID: "user", Token: s}
	for _, fmtVerb := range []string{"%v", "%+v", "%#v", "%s"} {
		got := fmt.Sprintf(fmtVerb, w)
		assert.Contains(t, got, redactedString,
			"struct embed via %q must redact the Secret field", fmtVerb)
		assert.NotContains(t, got, distinctiveSecret,
			"struct embed via %q LEAKED the raw secret: %s", fmtVerb, got)
	}
}

// TestSecret_MarshalJSON verifies the JSON serialization path:
//
//   - The MarshalJSON method itself returns the literal bytes `"<redacted>"`.
//   - json.Marshal post-processes those bytes (it HTML-safe-escapes `<` and
//     `>` to < and > by default for safer browser rendering), so
//     the OUTPUT of json.Marshal carries the escaped form. Both forms decode
//     back to the same string `"<redacted>"`, and neither leaks the secret.
//
// Critical because Temporal's default DataConverter is encoding/json — if
// MarshalJSON were missing or returned the raw value, it would land in
// Temporal history payloads.
func TestSecret_MarshalJSON(t *testing.T) {
	s := NewSecret(distinctiveSecret)

	// Direct MarshalJSON: returns the unescaped literal bytes.
	rawJSON, err := s.MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, `"<redacted>"`, string(rawJSON),
		"MarshalJSON returns the unescaped literal — json.Marshal does its own escaping")

	// Through json.Marshal: HTML-safe escaping is applied; both forms decode
	// to the same string and neither contains the secret.
	encoded, err := json.Marshal(s)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), distinctiveSecret,
		"json.Marshal output must not leak the raw secret")
	var decoded string
	require.NoError(t, json.Unmarshal(encoded, &decoded),
		"json.Marshal output must decode as a string")
	assert.Equal(t, redactedString, decoded,
		"after a Marshal/Unmarshal round-trip, the secret value is the redacted string")

	// Embedded in a struct — the realistic Temporal payload shape.
	type wire struct {
		ID    string `json:"id"`
		Token Secret `json:"token"`
	}
	encoded, err = json.Marshal(wire{ID: "x", Token: s})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), distinctiveSecret,
		"struct-embed marshal must not leak the raw secret")

	var roundTrip struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(encoded, &roundTrip),
		"struct-embed marshal output must decode")
	assert.Equal(t, "x", roundTrip.ID)
	assert.Equal(t, redactedString, roundTrip.Token,
		"Token field decodes to the redacted string after round-trip")
}

// TestSecret_MarshalText verifies encoding.TextMarshaler returns
// "<redacted>". slog's text handler uses TextMarshaler for log values; this
// closes the slog.Info("...", "secret", secret) leak path.
func TestSecret_MarshalText(t *testing.T) {
	s := NewSecret(distinctiveSecret)
	got, err := s.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, redactedString, string(got))
	assert.NotContains(t, string(got), distinctiveSecret)
}

// TestSecret_Reveal verifies the constructor/accessor round-trip: the ONLY
// way out of the wrapper is .Reveal(), and it returns exactly what went in.
func TestSecret_Reveal(t *testing.T) {
	s := NewSecret("raw")
	assert.Equal(t, "raw", s.Reveal())

	// Empty string round-trips.
	s2 := NewSecret("")
	assert.Equal(t, "", s2.Reveal())

	// Non-ASCII bytes round-trip verbatim.
	s3 := NewSecret("\x00\xff\xfeédoñé")
	assert.Equal(t, "\x00\xff\xfeédoñé", s3.Reveal())
}

// TestSecret_ZeroValue verifies that a struct-literal Secret{} (zero value)
// behaves identically to NewSecret(""): all formatters redact, Reveal()
// returns "", and no panics occur. This is the path a dropped field on a
// Credential would take.
func TestSecret_ZeroValue(t *testing.T) {
	var s Secret // zero value — value field is empty string

	assert.Equal(t, redactedString, s.String())
	assert.Equal(t, redactedString, s.GoString())
	assert.Equal(t, redactedString, fmt.Sprint(s))
	assert.Equal(t, redactedString, fmt.Sprintf("%+v", s))
	assert.Equal(t, redactedString, fmt.Sprintf("%#v", s))

	jb, err := s.MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, `"<redacted>"`, string(jb),
		"zero-value MarshalJSON returns the redacted literal, not a panic or empty bytes")

	tb, err := s.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, redactedString, string(tb))

	assert.Equal(t, "", s.Reveal(), "zero-value Secret reveals empty string, not panic")
}
