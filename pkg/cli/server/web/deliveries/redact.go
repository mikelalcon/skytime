package deliveries

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// redactSubstrings: case-insensitive substring matchers on the
// header NAME (lowercased before match). Per D-7.3-19 + Research
// Pattern 5. Source-agnostic — no provider-specific names.
var redactSubstrings = []string{
	"authorization",
	"secret",
	"token",
	"key",
	"signature",
}

// headerNameToken is the RFC 7230 (HTTP/1.1) token charset for
// header field names. A well-formed name uses only ALPHA, DIGIT, and
// '-'. We reject anything else as defense-in-depth — a malicious
// webhook with a header name containing '<', '>', '"', '/', or any
// other byte cannot smuggle markup through the SSE payload into the
// dashboard's DOM.
//
// Note: the actual RFC 7230 token grammar permits more punctuation
// (`!#$%&'*+-.^_`|~`), but real-world clients (browsers, gh-webhook,
// curl) emit only `[A-Za-z0-9-]` for custom header names. We
// conservatively limit to that subset.
var headerNameToken = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// RedactHeaders returns a NEW map[string]string. Two-step:
//   (1) header NAMES not matching the RFC 7230 token regex are DROPPED
//       entirely — XSS defense-in-depth (M4 from Phase 7.3 checker)
//   (2) for surviving names, values are joined with ", " when multi-
//       valued; names whose lowercased form contains any redactSubstrings
//       entry have their value replaced with "<redacted>"; other values
//       pass through TruncateValue.
func RedactHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for name, vals := range h {
		// Step 1: name sanitization. Drop malformed names.
		if !headerNameToken.MatchString(name) {
			continue
		}
		// Step 2: value redaction.
		lower := strings.ToLower(name)
		redacted := false
		for _, m := range redactSubstrings {
			if strings.Contains(lower, m) {
				out[name] = "<redacted>"
				redacted = true
				break
			}
		}
		if !redacted {
			out[name] = TruncateValue(strings.Join(vals, ", "))
		}
	}
	return out
}

// TruncateValue caps a header value at 80 chars with U+2026 ellipsis
// appended (D-7.3-19). NOT byte-safe across multi-byte boundaries —
// for v1 headers are ASCII-only in practice.
func TruncateValue(v string) string {
	const max = 80
	if len(v) <= max {
		return v
	}
	return v[:max] + "…"
}

// SummarizePayload renders a compact one-line preview of the request
// body for the dashboard row. JSON content-types get json.Compact
// applied then truncated; non-JSON gets a verbatim byte prefix.
// Empty body returns "(empty body)" (D-7.3-18).
func SummarizePayload(contentType string, body []byte, max int) string {
	if len(body) == 0 {
		return "(empty body)"
	}
	if max <= 0 {
		max = 200
	}
	if strings.Contains(strings.ToLower(contentType), "json") {
		var compact bytes.Buffer
		if err := json.Compact(&compact, body); err == nil {
			s := compact.String()
			if len(s) > max {
				return s[:max] + "…"
			}
			return s
		}
		// fall through on bad JSON
	}
	if len(body) > max {
		return string(body[:max]) + "…"
	}
	return string(body)
}
