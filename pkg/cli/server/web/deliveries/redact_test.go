package deliveries

import "testing"

func TestRedactHeaders_Authorization(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 02. Asserts http.Header{\"Authorization\": [...]} renders to {\"Authorization\": \"<redacted>\"}.")
}

func TestRedactHeaders_SecretTokenKeySignature_CaseInsensitive(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 02. Asserts X-Stripe-Signature, X-API-Key, X-CSRF-Token, X-Hub-Signature-256, X-Some-Secret all redact regardless of casing.")
}

func TestRedactHeaders_NonSensitivePassthrough(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 02. Asserts Content-Type and User-Agent are NOT redacted.")
}

func TestTruncateValue_80Chars(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 02. Asserts values >80 chars get '\\u2026' suffix.")
}

func TestTruncateValue_Passthrough(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 02. Asserts values <=80 chars are returned unchanged.")
}
