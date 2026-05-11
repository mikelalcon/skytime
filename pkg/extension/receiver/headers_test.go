package receiver

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
)

// TestHTTPHeaders_CaseInsensitiveLookup pins the headline contract:
// `req.headers["X-GitHub-Delivery"]` returns the same value as
// `req.headers["x-github-delivery"]` and `req.headers["X-Github-Delivery"]`.
// Without case-insensitive lookup, callers following GitHub's documented
// header casing (X-GitHub-Delivery) get a KeyError because Go's net/http
// canonicalizes incoming wire headers to "X-Github-Delivery".
func TestHTTPHeaders_CaseInsensitiveLookup(t *testing.T) {
	h := http.Header{}
	h.Set("X-GitHub-Delivery", "abc-123")
	h.Set("X-Hub-Signature-256", "sha256=deadbeef")

	hh := newHTTPHeaders(h)

	for _, k := range []string{
		"X-GitHub-Delivery", // GitHub-documented casing
		"X-Github-Delivery", // Go-canonical casing
		"x-github-delivery", // all-lowercase
		"X-GITHUB-DELIVERY", // all-upper
	} {
		v, found, err := hh.Get(starlark.String(k))
		require.NoError(t, err, "lookup with %q", k)
		require.True(t, found, "lookup with %q must find the entry", k)
		assert.Equal(t, starlark.String("abc-123"), v, "lookup with %q", k)
	}

	// Missing keys return (nil, false, nil) — Starlark raises KeyError.
	_, found, err := hh.Get(starlark.String("X-Missing-Header"))
	require.NoError(t, err)
	assert.False(t, found)
}

// TestHTTPHeaders_NonStringKeyErrors guards the Mapping contract: integer
// or other non-string lookup keys are not silently coerced; they error.
func TestHTTPHeaders_NonStringKeyErrors(t *testing.T) {
	hh := newHTTPHeaders(http.Header{})
	_, _, err := hh.Get(starlark.MakeInt(42))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key must be string")
}

// TestHTTPHeaders_LenAndIterate guards `len(req.headers)` and
// `for k in req.headers` so the wrapper isn't a worse dict than the one
// it replaces.
func TestHTTPHeaders_LenAndIterate(t *testing.T) {
	h := http.Header{}
	h.Set("X-GitHub-Delivery", "abc")
	h.Set("X-GitHub-Event", "issues")
	h.Set("X-Hub-Signature-256", "sha256=foo")

	hh := newHTTPHeaders(h)
	assert.Equal(t, 3, hh.Len())

	// Iteration yields canonical keys in sorted order for replay determinism.
	var got []string
	it := hh.Iterate()
	defer it.Done()
	var v starlark.Value
	for it.Next(&v) {
		got = append(got, string(v.(starlark.String)))
	}
	assert.Equal(t, []string{
		"X-Github-Delivery",
		"X-Github-Event",
		"X-Hub-Signature-256",
	}, got)
}

// TestHTTPHeaders_FromLambda end-to-end: a Starlark lambda that
// subscripts `req.headers["X-GitHub-Delivery"]` returns the right value
// even though Go canonicalizes the dict key on insertion.
func TestHTTPHeaders_FromLambda(t *testing.T) {
	h := http.Header{}
	h.Set("X-GitHub-Delivery", "delivery-uuid-1")

	hh := newHTTPHeaders(h)

	thread := &starlark.Thread{Name: "test"}
	// Synthesize a tiny program that subscripts the headers value.
	globals := starlark.StringDict{"headers": hh}
	val, err := starlark.Eval(thread, "test.star", `headers["X-GitHub-Delivery"]`, globals)
	require.NoError(t, err)
	assert.Equal(t, starlark.String("delivery-uuid-1"), val)

	// And the same lookup with original-on-wire casing differing from
	// Go-canonical — proves the case-insensitive contract end-to-end.
	val2, err := starlark.Eval(thread, "test.star", `headers["x-github-delivery"]`, globals)
	require.NoError(t, err)
	assert.Equal(t, starlark.String("delivery-uuid-1"), val2)
}
