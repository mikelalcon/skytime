package receiver

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"go.starlark.net/starlark"
)

// httpHeaders is a Starlark value wrapping HTTP headers with
// case-insensitive key lookups. Storage uses Go's canonical header casing
// (http.CanonicalHeaderKey, applied implicitly by net/http on parse);
// lookups canonicalize the supplied key before matching, so
// `req.headers["X-GitHub-Delivery"]`, `req.headers["x-github-delivery"]`,
// and `req.headers["X-Github-Delivery"]` all return the same value.
//
// This honors D-7.1-05's "case-preserving key as received" intent against
// Go's net/http canonicalization (which discards the original wire casing
// during parse). Per RFC 7230 §3.2 HTTP header names are case-insensitive,
// so case-insensitive lookup is the spec-correct behavior anyway.
//
// Iteration yields canonical keys in deterministic (sorted) order so that
// lambdas observing `for k in req.headers` get replay-stable output.
type httpHeaders struct {
	canonical map[string]string // canonical key → first header value
	keys      []string          // canonical keys, sorted
}

// newHTTPHeaders snapshots a single header value per canonical key.
// Webhook providers send single-valued metadata headers in practice;
// callers that need multi-value semantics should read the original
// http.Header.
func newHTTPHeaders(h http.Header) *httpHeaders {
	out := &httpHeaders{
		canonical: make(map[string]string, len(h)),
		keys:      make([]string, 0, len(h)),
	}
	for k, vv := range h {
		if len(vv) == 0 {
			continue
		}
		out.canonical[k] = vv[0]
		out.keys = append(out.keys, k)
	}
	sort.Strings(out.keys)
	return out
}

// starlark.Value

func (*httpHeaders) Type() string         { return "http.Headers" }
func (*httpHeaders) Freeze()              {} // immutable from construction
func (*httpHeaders) Truth() starlark.Bool { return starlark.True }
func (h *httpHeaders) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", h.Type())
}

func (h *httpHeaders) String() string {
	var b strings.Builder
	b.WriteString("http.Headers{")
	for i, k := range h.keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q: %q", k, h.canonical[k])
	}
	b.WriteByte('}')
	return b.String()
}

// starlark.Mapping — subscript access: req.headers["X-..."]
// Lookup key is canonicalized via http.CanonicalHeaderKey before matching.
func (h *httpHeaders) Get(k starlark.Value) (starlark.Value, bool, error) {
	s, ok := starlark.AsString(k)
	if !ok {
		return nil, false, fmt.Errorf("%s key must be string, got %s", h.Type(), k.Type())
	}
	v, found := h.canonical[http.CanonicalHeaderKey(s)]
	if !found {
		return nil, false, nil
	}
	return starlark.String(v), true, nil
}

// starlark.HasLen — `len(req.headers)` works.
func (h *httpHeaders) Len() int { return len(h.keys) }

// starlark.Iterable — `for k in req.headers` yields canonical keys
// in sorted order for replay determinism.
func (h *httpHeaders) Iterate() starlark.Iterator {
	return &httpHeadersIter{keys: h.keys}
}

type httpHeadersIter struct {
	keys []string
	i    int
}

func (it *httpHeadersIter) Next(p *starlark.Value) bool {
	if it.i >= len(it.keys) {
		return false
	}
	*p = starlark.String(it.keys[it.i])
	it.i++
	return true
}

func (*httpHeadersIter) Done() {}

// Compile-time interface checks. Sequence subsumes Iterable + Len, which
// is what enables both `for k in req.headers` and `len(req.headers)`.
var (
	_ starlark.Value    = (*httpHeaders)(nil)
	_ starlark.Mapping  = (*httpHeaders)(nil)
	_ starlark.Sequence = (*httpHeaders)(nil)
)
