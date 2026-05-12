package deliveries

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactHeaders_Authorization(t *testing.T) {
	h := http.Header{"Authorization": {"Bearer eyJxxxx"}}
	out := RedactHeaders(h)
	require.Equal(t, "<redacted>", out["Authorization"])
}

func TestRedactHeaders_SecretTokenKeySignature_CaseInsensitive(t *testing.T) {
	cases := []string{
		"X-Hub-Signature-256",
		"X-CSRF-Token",
		"X-API-Key",
		"X-My-Secret",
		"Stripe-Signature",
		"x-hub-signature",            // lowercase variant
		"AUTHORIZATION-OVERRIDE",     // contains "authorization"
		"X-Some-API-KEY",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			h := http.Header{name: {"sensitive-value"}}
			out := RedactHeaders(h)
			require.Equal(t, "<redacted>", out[name], "header %q should be redacted", name)
		})
	}
}

func TestRedactHeaders_NonSensitivePassthrough(t *testing.T) {
	h := http.Header{
		"Content-Type": {"application/json"},
		"User-Agent":   {"gh-webhook-forward/1.0"},
		"Accept":       {"*/*"},
	}
	out := RedactHeaders(h)
	require.Equal(t, "application/json", out["Content-Type"])
	require.Equal(t, "gh-webhook-forward/1.0", out["User-Agent"])
	// Accept's value '*/*' contains '*' and '/' — but RFC 7230 token
	// regex is applied to the NAME only, not the VALUE. "Accept" is a
	// well-formed name, so it passes through. The value is left raw
	// (the renderer's html/template will escape it).
	require.Equal(t, "*/*", out["Accept"])
}

func TestRedactHeaders_MalformedHeaderNameStripped(t *testing.T) {
	// M4 defense (Phase 7.3 checker): header names with bytes outside
	// the RFC 7230 token charset must be dropped from the output map
	// entirely. A future XSS attempt via `X-<script>alert(1)</script>`
	// header name would not make it onto the SSE wire.
	h := http.Header{
		"X-<script>alert(1)</script>": {"bad"},
		"X<bad>":                      {"bad"},
		"X\"quoted":                   {"bad"},
		"X/slash":                     {"bad"},
		"X Space":                     {"bad"}, // space is not in the token charset
		"Content-Type":                {"application/json"},
	}
	out := RedactHeaders(h)
	// The malformed names must be absent.
	for _, bad := range []string{
		"X-<script>alert(1)</script>",
		"X<bad>",
		"X\"quoted",
		"X/slash",
		"X Space",
	} {
		_, present := out[bad]
		require.False(t, present, "malformed header name %q should have been dropped", bad)
	}
	// The well-formed name survives.
	require.Equal(t, "application/json", out["Content-Type"])
}

func TestTruncateValue_80Chars(t *testing.T) {
	v := strings.Repeat("a", 81)
	got := TruncateValue(v)
	require.True(t, strings.HasSuffix(got, "…"))
	require.Equal(t, 80+len("…"), len(got))
}

func TestTruncateValue_Passthrough(t *testing.T) {
	v := strings.Repeat("a", 80)
	require.Equal(t, v, TruncateValue(v))
	require.Equal(t, "short", TruncateValue("short"))
	require.Equal(t, "", TruncateValue(""))
}

func TestSummarizePayload_EmptyBody(t *testing.T) {
	require.Equal(t, "(empty body)", SummarizePayload("application/json", nil, 200))
}

func TestSummarizePayload_JSONCompact(t *testing.T) {
	body := []byte(`{
	  "a": 1,
	  "b": 2
	}`)
	got := SummarizePayload("application/json", body, 200)
	require.Equal(t, `{"a":1,"b":2}`, got)
}

func TestSummarizePayload_NonJSONTruncate(t *testing.T) {
	body := []byte(strings.Repeat("x", 300))
	got := SummarizePayload("text/plain", body, 50)
	require.True(t, strings.HasSuffix(got, "…"))
	require.Equal(t, 50+len("…"), len(got))
}
