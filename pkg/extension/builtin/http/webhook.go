package http

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"sort"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// httpWebhookSource is the concrete TriggerSource for http.webhook(...).
// Lives alongside http.endpoint (D-7.1-01: pkg/extension/builtin/http
// covers HTTP both directions). Configurable signature scheme
// (D-7.1-03 allowlist {sha256, sha1, sha512} + arbitrary header)
// future-proofs for Stripe / GitLab / generic providers without
// per-source code in the receiver.
//
// Embeds extension.TriggerSourceSeal to satisfy the unexported
// triggerSourceMarker() method of extension.TriggerSource — Go's method
// promotion makes the carrier's marker reachable on this type even
// though pkg/extension/builtin/http is a sub-package and cannot declare
// the unexported method directly.
type httpWebhookSource struct {
	extension.TriggerSourceSeal // embed to satisfy the TriggerSource seal across package boundaries

	path            string // required, must start with "/"
	method          string // required, uppercase HTTP verb (POST / GET / PUT / DELETE / PATCH / HEAD)
	secretCredID    string // empty = unsigned mount (gh webhook forward without --secret, local dev)
	signatureAlgo   string // default "sha256"; allowlist {sha256, sha1, sha512}
	signatureHeader string // default "X-Signature"; arbitrary string for cross-provider use
}

const httpWebhookKind = "http.webhook"

// allowedSignatureAlgos is the parse-time allowlist (D-7.1-03 LOCKED).
// Adding entries requires CONTEXT.md amendment AND a corresponding
// case in pkg/extension/receiver/signature.go::newHMAC.
var allowedSignatureAlgos = map[string]bool{
	"sha256": true,
	"sha1":   true,
	"sha512": true,
}

// allowedHTTPMethods is the parse-time method allowlist. Stdlib
// net/http.MethodGet / MethodPost / etc. constants would also work
// but the locked-set form here is greppable and stable.
var allowedHTTPMethods = map[string]bool{
	stdhttp.MethodGet:    true,
	stdhttp.MethodPost:   true,
	stdhttp.MethodPut:    true,
	stdhttp.MethodDelete: true,
	stdhttp.MethodPatch:  true,
	stdhttp.MethodHead:   true,
}

// ----- TriggerSource contract -----

// Kind returns "http.webhook" — the JSON discriminator + startup banner label.
func (*httpWebhookSource) Kind() string { return httpWebhookKind }

// ReqSchema returns the locked req.<field> set for HTTP-shaped sources
// (D-7.1-05). Both http.webhook and github.webhook expose payload +
// headers; cron sources (Phase 7.2) will declare different fields.
//
// Sorted alphabetically so the parser-time req-walker (Plan 03) can
// rely on deterministic suggestions in "did you mean" diagnostics.
func (*httpWebhookSource) ReqSchema() []string { return []string{"headers", "payload"} }

// MarshalJSON produces the {kind, config} envelope (D-07-09). config
// contains the path/method/signature_algo/signature_header strings
// AND the credential ID string (when set). NEVER a resolved Secret —
// the resolver is invoked JIT inside the receiver handler (Plan 04),
// not here.
func (s *httpWebhookSource) MarshalJSON() ([]byte, error) {
	cfg := map[string]any{
		"path":             s.path,
		"method":           s.method,
		"signature_algo":   s.signatureAlgo,
		"signature_header": s.signatureHeader,
	}
	if s.secretCredID != "" {
		cfg["credential_id"] = s.secretCredID
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`{"kind":%q,"config":%s}`, httpWebhookKind, string(cfgBytes))), nil
}

// (triggerSourceMarker is supplied by the embedded extension.TriggerSourceSeal —
// Go's method promotion routes the unexported seal method onto
// *httpWebhookSource, satisfying extension.TriggerSource at compile time.)

// ----- HTTPMounter contract -----

// HTTPMount returns the (path, method) the receiver mounts on its
// ServeMux. Plan 04 type-asserts this interface; cron sources don't
// implement it and are skipped.
//
// The package import path of receiver.HTTPMounter is intentionally NOT
// imported here — *httpWebhookSource satisfies the interface
// structurally. Importing pkg/extension/receiver here would create a
// cycle (receiver imports pkg/extension/builtin/http transitively via
// the worker.Triggers walk).
func (s *httpWebhookSource) HTTPMount() (path, method string) {
	return s.path, s.method
}

// ----- starlark.Value contract -----
// Required so a httpWebhookSource value can flow through Starlark
// (it is the return value of the http.webhook(...) builtin call).

// String renders a debug-friendly representation; surfaces in slog
// fields when the value is logged by mistake (path is non-secret;
// secret_credential is the ID string only, never the resolved Secret).
func (s *httpWebhookSource) String() string {
	return fmt.Sprintf("http.webhook(path=%q, method=%q)", s.path, s.method)
}

// Type returns the Starlark type name. Matches Kind() for clarity in
// "type-error: expected X, got http.webhook" diagnostics.
func (*httpWebhookSource) Type() string { return "http.webhook" }

// Freeze is a no-op — httpWebhookSource is immutable after construction
// (no exported setters; struct fields are unexported and unwritable
// from Starlark).
func (*httpWebhookSource) Freeze() {}

// Truth returns starlark.True so `if source: ...` evaluates to True
// (matches the convention for non-zero structured values).
func (*httpWebhookSource) Truth() starlark.Bool { return starlark.True }

// Hash refuses to be a map key. Webhook sources are referenced by
// identity (passed once to trigger(...)), never used as dict keys.
func (*httpWebhookSource) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: http.webhook")
}

// Compile-time seal assertions.
var _ extension.TriggerSource = (*httpWebhookSource)(nil)
var _ starlark.Value = (*httpWebhookSource)(nil)

// ----- parser-side factory -----

// webhookFactory implements `http.webhook(path=, method=, secret_credential=, signature_algo=, signature_header=)`.
//
// Validates ALL kwargs at parse time per D-7.1-03 + D-7.1 invariants:
//   - path MUST be set and start with "/"
//   - method MUST be set and ∈ allowedHTTPMethods
//   - signature_algo defaults "sha256"; if set, MUST ∈ allowedSignatureAlgos
//   - signature_header defaults "X-Signature"; arbitrary non-empty string
//   - secret_credential is opaque; empty string mounts unsigned (D-7.1-04)
//
// Errors are *dag.ParseError so the parser's existing error rendering
// produces "<file>:<line>:<col>: <msg>".
func webhookFactory(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		path            string
		method          string
		secretCred      string
		signatureAlgo   = "sha256"
		signatureHeader = "X-Signature"
	)
	if err := starlark.UnpackArgs("http.webhook", args, kwargs,
		"path", &path,
		"method", &method,
		"secret_credential?", &secretCred,
		"signature_algo?", &signatureAlgo,
		"signature_header?", &signatureHeader,
	); err != nil {
		return nil, &dag.ParseError{Pos: callerPosition(thread), Msg: "http.webhook: " + err.Error()}
	}
	if path == "" {
		return nil, &dag.ParseError{Pos: callerPosition(thread), Msg: "http.webhook: path is required"}
	}
	if path[0] != '/' {
		return nil, &dag.ParseError{
			Pos: callerPosition(thread),
			Msg: fmt.Sprintf("http.webhook: path %q must start with a leading slash (e.g. %q)", path, "/"+path),
		}
	}
	if method == "" {
		return nil, &dag.ParseError{Pos: callerPosition(thread), Msg: "http.webhook: method is required"}
	}
	if !allowedHTTPMethods[method] {
		allowed := sortedKeys(allowedHTTPMethods)
		return nil, &dag.ParseError{
			Pos: callerPosition(thread),
			Msg: fmt.Sprintf("http.webhook: method %q not allowed; valid methods: %v", method, allowed),
		}
	}
	if !allowedSignatureAlgos[signatureAlgo] {
		allowed := sortedKeys(allowedSignatureAlgos)
		return nil, &dag.ParseError{
			Pos: callerPosition(thread),
			Msg: fmt.Sprintf("http.webhook: signature_algo %q not allowed; valid algos: %v", signatureAlgo, allowed),
		}
	}
	if signatureHeader == "" {
		return nil, &dag.ParseError{Pos: callerPosition(thread), Msg: "http.webhook: signature_header must be non-empty"}
	}

	return &httpWebhookSource{
		path:            path,
		method:          method,
		secretCredID:    secretCred,
		signatureAlgo:   signatureAlgo,
		signatureHeader: signatureHeader,
	}, nil
}

// sortedKeys returns map keys sorted alphabetically. Used for
// deterministic error messages in parse-time rejections (test
// stability + log gravitas — operators see the same valid-set every
// time they hit a typo).
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// init registers the http.webhook unmarshaler with the kind-keyed
// factory registry (D-07-09). Round-trip path: dag.Trigger.MarshalJSON
// → triggerJSON.Source bytes → on UnmarshalJSON, extension's installed
// dispatcher (extensionTriggerUnmarshaler) reads the {kind, config}
// envelope, looks up "http.webhook" in the registry, and passes ONLY
// the inner config bytes to THIS factory to rebuild *httpWebhookSource.
//
// Errors are intentionally swallowed (mirrors RegisterFakeFactories);
// duplicate registration across test re-imports is benign.
func init() {
	_ = extension.RegisterTriggerSourceFactory(httpWebhookKind, func(configData []byte) (extension.TriggerSource, error) {
		// configData is the {path, method, signature_algo, signature_header,
		// credential_id?} object, NOT the outer {kind, config} envelope —
		// the extension dispatcher peels the envelope before invoking us.
		var cfg struct {
			Path            string `json:"path"`
			Method          string `json:"method"`
			SignatureAlgo   string `json:"signature_algo"`
			SignatureHeader string `json:"signature_header"`
			CredentialID    string `json:"credential_id"`
		}
		if err := json.Unmarshal(configData, &cfg); err != nil {
			return nil, fmt.Errorf("http.webhook unmarshal: %w", err)
		}
		return &httpWebhookSource{
			path:            cfg.Path,
			method:          cfg.Method,
			secretCredID:    cfg.CredentialID,
			signatureAlgo:   cfg.SignatureAlgo,
			signatureHeader: cfg.SignatureHeader,
		}, nil
	})
}
