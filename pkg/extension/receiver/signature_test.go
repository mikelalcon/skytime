package receiver

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// computeHex computes the hex-encoded HMAC of body with secret using the
// named algo. Tests use this helper to produce expected header values
// inline rather than hardcoding hex strings (which would obscure why each
// test is correct and drift across stdlib changes).
func computeHex(t *testing.T, algo string, secret, body []byte) string {
	t.Helper()
	var h hash.Hash
	switch algo {
	case "sha256":
		h = hmac.New(sha256.New, secret)
	case "sha1":
		h = hmac.New(sha1.New, secret)
	case "sha512":
		h = hmac.New(sha512.New, secret)
	default:
		t.Fatalf("computeHex: unsupported algo %q", algo)
	}
	_, err := h.Write(body)
	require.NoError(t, err)
	return hex.EncodeToString(h.Sum(nil))
}

func TestSignature_SHA256_Valid(t *testing.T) {
	body := []byte(`{"x":1}`)
	secret := []byte("topsecret")
	header := "sha256=" + computeHex(t, "sha256", secret, body)
	require.NoError(t, validateHMAC(body, secret, "sha256", header))
}

func TestSignature_SHA256_Invalid(t *testing.T) {
	body := []byte(`{"x":1}`)
	secret := []byte("topsecret")
	header := "sha256=deadbeef"
	err := validateHMAC(body, secret, "sha256", header)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature_mismatch")
}

func TestSignature_SHA256_MissingPrefix(t *testing.T) {
	body := []byte(`{"x":1}`)
	secret := []byte("topsecret")
	// Bare hex without the "sha256=" prefix must be rejected.
	header := computeHex(t, "sha256", secret, body)
	err := validateHMAC(body, secret, "sha256", header)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature_mismatch")
}

func TestSignature_SHA1_Valid(t *testing.T) {
	body := []byte(`{"x":1}`)
	secret := []byte("topsecret")
	header := "sha1=" + computeHex(t, "sha1", secret, body)
	require.NoError(t, validateHMAC(body, secret, "sha1", header))
}

func TestSignature_SHA512_Valid(t *testing.T) {
	body := []byte(`{"x":1}`)
	secret := []byte("topsecret")
	header := "sha512=" + computeHex(t, "sha512", secret, body)
	require.NoError(t, validateHMAC(body, secret, "sha512", header))
}

func TestSignature_UnknownAlgo(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic on unknown algo: %v", r)
		}
	}()
	err := validateHMAC([]byte("x"), []byte("s"), "md5", "md5=00")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unsupported algo "md5"`)
}

// TestSignature_HMACEqualNotBytesEqual is a source-grep gate: the file must
// use crypto/hmac.Equal (constant-time) and must not use bytes.Equal for
// the comparison. Plan 08 ships a cross-package firewall that extends this.
func TestSignature_HMACEqualNotBytesEqual(t *testing.T) {
	src, err := os.ReadFile("signature.go")
	require.NoError(t, err)
	require.True(t, bytes.Contains(src, []byte("hmac.Equal")), "signature.go must use hmac.Equal for constant-time comparison")
	require.False(t, bytes.Contains(src, []byte("bytes.Equal")), "signature.go must NOT use bytes.Equal — timing-attack vulnerable")
}

func TestSignature_EmptyBody(t *testing.T) {
	body := []byte{}
	secret := []byte("topsecret")
	header := "sha256=" + computeHex(t, "sha256", secret, body)
	require.NoError(t, validateHMAC(body, secret, "sha256", header))
}

func TestSignature_EmptyHeader(t *testing.T) {
	body := []byte(`{"x":1}`)
	secret := []byte("topsecret")
	err := validateHMAC(body, secret, "sha256", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature_mismatch")
	// Bonus: ensure the prefix-missing pathway is what fired (not unsupported algo)
	assert.False(t, strings.Contains(err.Error(), "unsupported algo"))
}
