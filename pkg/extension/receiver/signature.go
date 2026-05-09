package receiver

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
)

// validateHMAC validates that headerValue (form: "<algo>=<hex>") matches
// HMAC(algo, secret, body) using a constant-time comparison.
//
// Pre: algo ∈ {"sha256", "sha1", "sha512"} (D-7.1-03 allowlist; the parser
// rejects any other value at parse time, but defensive check here too).
// Pre: body is the RAW request bytes — do NOT pass json-re-encoded bytes
// (Pitfall 2). The handler in Plan 04 calls io.ReadAll on the
// MaxBytesReader-wrapped body BEFORE json.Unmarshal exactly so this
// invariant holds.
//
// Returns nil on byte-exact HMAC match. Returns an error whose Error()
// contains "signature_mismatch" on any mismatch / missing prefix /
// missing header (so handler maps to error_class=signature_mismatch).
// Returns an error containing "unsupported algo" for an unknown algo
// (this is a programmer error — the parser should have rejected it).
//
// CRITICAL: comparison MUST use hmac.Equal (constant-time). The
// stdlib byte-slice equality helper is timing-attack-vulnerable and is
// banned in this file. The Plan 08 cross-package firewall test asserts
// the constant-time helper is the only comparator used here.
func validateHMAC(body, secret []byte, algo, headerValue string) error {
	prefix := algo + "="
	if len(headerValue) < len(prefix) || headerValue[:len(prefix)] != prefix {
		return fmt.Errorf("signature_mismatch: header missing %q prefix", prefix)
	}
	providedHex := headerValue[len(prefix):]

	mac, err := newHMAC(algo, secret)
	if err != nil {
		return err
	}
	if _, err := mac.Write(body); err != nil {
		return fmt.Errorf("signature_mismatch: hmac write: %w", err)
	}
	expectedHex := hex.EncodeToString(mac.Sum(nil))

	// hmac.Equal is constant-time; the stdlib byte-slice equality
	// helper is NOT. DO NOT REPLACE.
	if !hmac.Equal([]byte(providedHex), []byte(expectedHex)) {
		return fmt.Errorf("signature_mismatch")
	}
	return nil
}

// newHMAC returns an hmac hash for the given algo. Allowlist:
// "sha256" → hmac.New(sha256.New, secret), and likewise sha1, sha512.
// Unknown algo → error.
func newHMAC(algo string, secret []byte) (hash.Hash, error) {
	switch algo {
	case "sha256":
		return hmac.New(sha256.New, secret), nil
	case "sha1":
		return hmac.New(sha1.New, secret), nil
	case "sha512":
		return hmac.New(sha512.New, secret), nil
	default:
		return nil, fmt.Errorf("unsupported algo %q (allowed: sha256, sha1, sha512)", algo)
	}
}
