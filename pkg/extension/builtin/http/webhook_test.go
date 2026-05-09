package http

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/extension/receiver"
)

// callHTTPWebhook is a test helper that constructs a minimal `http`
// module containing the webhook factory (mimicking what Task 2's
// production Initialize() will wire) and evaluates the given expression
// against that namespace. This keeps Task 1 tests testing the factory's
// behavior — kwargs validation, JSON round-trip, type assertions —
// without coupling them to Task 2's Members-map edit.
//
// Task 2's TestExtension_InitializeIncludesWebhook + the
// TestExtension_StarFileCallsHttpWebhook test exercise the FULL path
// through the production skytimeHTTP{}.Initialize() — that's the
// integration gate.
func callHTTPWebhook(t *testing.T, inExpr string) (starlark.Value, error) {
	t.Helper()

	mod := &starlarkstruct.Module{
		Name: "http",
		Members: starlark.StringDict{
			"webhook": starlark.NewBuiltin("http.webhook", webhookFactory),
		},
	}
	predeclared := starlark.StringDict{"http": mod}

	thread := &starlark.Thread{Name: "test-call"}
	src := "result = " + inExpr + "\n"
	globals, err := starlark.ExecFile(thread, "test_webhook.star", src, predeclared)
	if err != nil {
		return nil, err
	}
	v, ok := globals["result"]
	require.True(t, ok, "expected 'result' in globals")
	return v, nil
}

// TestHttpWebhook_FactorySuccess covers the happy path: a fully-defaulted
// http.webhook(path=, method=) call returns a value satisfying both
// extension.TriggerSource (sealed) AND receiver.HTTPMounter, with the
// configured kwargs surfacing through the contract methods.
func TestHttpWebhook_FactorySuccess(t *testing.T) {
	v, err := callHTTPWebhook(t, `http.webhook(path="/hooks/x", method="POST")`)
	require.NoError(t, err)
	require.NotNil(t, v)

	// Both seals/contracts.
	src, ok := v.(extension.TriggerSource)
	require.True(t, ok, "value must satisfy extension.TriggerSource (sealed); got %T", v)
	mounter, ok := v.(receiver.HTTPMounter)
	require.True(t, ok, "value must satisfy receiver.HTTPMounter; got %T", v)

	// Contract methods.
	require.Equal(t, "http.webhook", src.Kind())
	require.Equal(t, []string{"headers", "payload"}, src.ReqSchema())

	path, method := mounter.HTTPMount()
	require.Equal(t, "/hooks/x", path)
	require.Equal(t, "POST", method)

	// Defaults populated on the concrete type.
	concrete, ok := v.(*httpWebhookSource)
	require.True(t, ok)
	require.Equal(t, "sha256", concrete.signatureAlgo)
	require.Equal(t, "X-Signature", concrete.signatureHeader)
	require.Equal(t, "", concrete.secretCredID)
}

// TestHttpWebhook_RejectsBadAlgo asserts parse-time rejection with a
// position-aware *dag.ParseError when signature_algo is not in the
// {sha256, sha1, sha512} allowlist.
func TestHttpWebhook_RejectsBadAlgo(t *testing.T) {
	_, err := callHTTPWebhook(t, `http.webhook(path="/x", method="POST", signature_algo="md5")`)
	require.Error(t, err)

	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError; got %T: %v", err, err)
	require.Contains(t, pe.Msg, "signature_algo")
	require.Contains(t, pe.Msg, "md5")
}

// TestHttpWebhook_RejectsBadMethod asserts parse-time rejection when method
// is not in the {GET, POST, PUT, DELETE, PATCH, HEAD} allowlist.
func TestHttpWebhook_RejectsBadMethod(t *testing.T) {
	_, err := callHTTPWebhook(t, `http.webhook(path="/x", method="OPTIONS")`)
	require.Error(t, err)

	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError; got %T: %v", err, err)
	require.Contains(t, pe.Msg, "method")
	require.Contains(t, pe.Msg, "OPTIONS")
}

// TestHttpWebhook_RejectsMissingPath asserts the path kwarg is required.
func TestHttpWebhook_RejectsMissingPath(t *testing.T) {
	_, err := callHTTPWebhook(t, `http.webhook(method="POST")`)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "path")
}

// TestHttpWebhook_RejectsMissingMethod asserts the method kwarg is required.
func TestHttpWebhook_RejectsMissingMethod(t *testing.T) {
	_, err := callHTTPWebhook(t, `http.webhook(path="/x")`)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "method")
}

// TestHttpWebhook_NoSecretInConfig asserts the {kind, config} envelope
// carries credential_id (when secret_credential is set) but NEVER any
// resolved Secret-like value. The marshaled bytes must not contain keys
// "secret", "reveal", or "value" — defense against accidentally widening
// the envelope shape.
func TestHttpWebhook_NoSecretInConfig(t *testing.T) {
	v, err := callHTTPWebhook(t,
		`http.webhook(path="/x", method="POST", secret_credential="my-secret-id", signature_algo="sha512", signature_header="X-Custom-Sig")`)
	require.NoError(t, err)

	src, ok := v.(extension.TriggerSource)
	require.True(t, ok)

	data, err := src.MarshalJSON()
	require.NoError(t, err)

	// Envelope shape: {kind, config} where config is a JSON object.
	var env struct {
		Kind   string         `json:"kind"`
		Config map[string]any `json:"config"`
	}
	require.NoError(t, json.Unmarshal(data, &env))
	require.Equal(t, "http.webhook", env.Kind)

	require.Equal(t, "/x", env.Config["path"])
	require.Equal(t, "POST", env.Config["method"])
	require.Equal(t, "sha512", env.Config["signature_algo"])
	require.Equal(t, "X-Custom-Sig", env.Config["signature_header"])
	require.Equal(t, "my-secret-id", env.Config["credential_id"])

	// No secret-like keys anywhere in the marshaled bytes.
	lower := strings.ToLower(string(data))
	require.NotContains(t, lower, `"secret"`, "config must not contain a 'secret' key")
	require.NotContains(t, lower, `"reveal"`, "config must not contain a 'reveal' key")
	require.NotContains(t, lower, `"value"`, "config must not contain a 'value' key")
}

// TestHttpWebhook_NoSecretInConfig_OmittedWhenUnset confirms the
// credential_id key is OMITTED entirely when secret_credential is not set
// (unsigned mount per D-7.1-04). This keeps the JSON compact and avoids
// implying a credential exists when one was not configured.
func TestHttpWebhook_NoSecretInConfig_OmittedWhenUnset(t *testing.T) {
	v, err := callHTTPWebhook(t, `http.webhook(path="/x", method="POST")`)
	require.NoError(t, err)

	src, ok := v.(extension.TriggerSource)
	require.True(t, ok)

	data, err := src.MarshalJSON()
	require.NoError(t, err)

	require.NotContains(t, string(data), `"credential_id"`,
		"credential_id must be omitted when secret_credential is unset")
}

// TestHttpWebhook_HTTPMounterAssertion separately documents the type
// assertion contract Plan 04's receiver depends on. Assertion succeeds and
// the returned (path, method) round-trip the parse-time kwargs.
func TestHttpWebhook_HTTPMounterAssertion(t *testing.T) {
	v, err := callHTTPWebhook(t, `http.webhook(path="/hooks/custom", method="PUT")`)
	require.NoError(t, err)

	mounter, ok := v.(receiver.HTTPMounter)
	require.True(t, ok, "value must satisfy receiver.HTTPMounter")
	path, method := mounter.HTTPMount()
	require.Equal(t, "/hooks/custom", path)
	require.Equal(t, "PUT", method)
}

// TestHttpWebhook_TriggerSourceSeal is a compile-time assertion mirrored
// at test time: *httpWebhookSource implements extension.TriggerSource
// (including the sealed triggerSourceMarker method). The compile-time
// assertion lives in webhook.go (`var _ extension.TriggerSource = ...`)
// — this test exists to surface seal regressions as test failures rather
// than build failures during refactors.
func TestHttpWebhook_TriggerSourceSeal(t *testing.T) {
	var src extension.TriggerSource = &httpWebhookSource{
		path:            "/x",
		method:          "POST",
		signatureAlgo:   "sha256",
		signatureHeader: "X-Signature",
	}
	require.Equal(t, "http.webhook", src.Kind())
}

// TestHttpWebhook_PathRequiresLeadingSlash asserts paths must start with
// "/". The error references "path" and "leading slash" / "/" so consultants
// get an actionable diagnostic.
func TestHttpWebhook_PathRequiresLeadingSlash(t *testing.T) {
	_, err := callHTTPWebhook(t, `http.webhook(path="hooks/x", method="POST")`)
	require.Error(t, err)

	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError; got %T: %v", err, err)
	require.Contains(t, pe.Msg, "path")
	require.Contains(t, pe.Msg, "leading slash")
}

// TestHttpWebhook_RegisterFactoryRoundTrip asserts the http.webhook
// init()-time registration with extension.RegisterTriggerSourceFactory
// installs an unmarshaler that reconstructs an equivalent
// *httpWebhookSource from the canonical config bytes. Round-trip path:
// dag.Trigger marshals → JSON → dag.Trigger unmarshals (which dispatches
// to extension's installed unmarshaler) → equivalent Source.
func TestHttpWebhook_RegisterFactoryRoundTrip(t *testing.T) {
	v, err := callHTTPWebhook(t,
		`http.webhook(path="/rt", method="POST", secret_credential="cred-1", signature_algo="sha512", signature_header="X-Sig")`)
	require.NoError(t, err)
	original, ok := v.(*httpWebhookSource)
	require.True(t, ok)

	trig := &dag.Trigger{
		FlowName: "round_trip_flow",
		Source:   original,
	}
	data, err := json.Marshal(trig)
	require.NoError(t, err)

	var rebuilt dag.Trigger
	require.NoError(t, json.Unmarshal(data, &rebuilt))
	require.NotNil(t, rebuilt.Source)

	mounter, ok := rebuilt.Source.(receiver.HTTPMounter)
	require.True(t, ok, "rebuilt source must satisfy receiver.HTTPMounter; got %T", rebuilt.Source)

	path, method := mounter.HTTPMount()
	require.Equal(t, "/rt", path)
	require.Equal(t, "POST", method)

	rebuiltConcrete, ok := rebuilt.Source.(*httpWebhookSource)
	require.True(t, ok)
	require.Equal(t, "sha512", rebuiltConcrete.signatureAlgo)
	require.Equal(t, "X-Sig", rebuiltConcrete.signatureHeader)
	require.Equal(t, "cred-1", rebuiltConcrete.secretCredID)
}

// TestHttpWebhook_StarlarkValueShape checks that the returned value has a
// sensible Starlark shape — non-callable, frozen, hash-rejected — so it
// behaves predictably when stored in module-level globals (D-7.1-01:
// `source = http.webhook(...)` at module scope).
func TestHttpWebhook_StarlarkValueShape(t *testing.T) {
	v, err := callHTTPWebhook(t, `http.webhook(path="/x", method="GET")`)
	require.NoError(t, err)

	require.Equal(t, "http.webhook", v.Type())
	require.Equal(t, starlark.True, v.Truth())
	require.Contains(t, v.String(), "/x")

	_, hashErr := v.Hash()
	require.Error(t, hashErr, "http.webhook values must be unhashable (no map-key use)")
}

// TestExtension_InitializeIncludesWebhook (Task 2 behavior gate): the
// skytimeHTTP.Initialize() Members map exposes "webhook" pointing to a
// *starlark.Builtin whose name is "http.webhook".
func TestExtension_InitializeIncludesWebhook(t *testing.T) {
	mod, err := skytimeHTTP{}.Initialize(&starlark.Thread{Name: "test"}, nil)
	require.NoError(t, err)
	module, ok := mod.(*starlarkstruct.Module)
	require.True(t, ok, "Initialize must return *starlarkstruct.Module; got %T", mod)

	hook, ok := module.Members["webhook"]
	require.True(t, ok, `Members must contain a "webhook" entry`)
	b, ok := hook.(*starlark.Builtin)
	require.True(t, ok, "Members[webhook] must be a *starlark.Builtin; got %T", hook)
	require.Equal(t, "http.webhook", b.Name())
}

// TestExtension_InitializeIncludesEndpoint_Regression confirms the
// existing "endpoint" attribute is preserved (Task 2 must not replace
// the Members map outright).
func TestExtension_InitializeIncludesEndpoint_Regression(t *testing.T) {
	mod, err := skytimeHTTP{}.Initialize(&starlark.Thread{Name: "test"}, nil)
	require.NoError(t, err)
	module, ok := mod.(*starlarkstruct.Module)
	require.True(t, ok)

	ep, ok := module.Members["endpoint"]
	require.True(t, ok, `Members must STILL contain an "endpoint" entry`)
	b, ok := ep.(*starlark.Builtin)
	require.True(t, ok)
	require.Equal(t, "http.endpoint", b.Name())
}

// TestExtension_StarFileCallsHttpWebhook is the load-bearing Task 2
// integration check: a `.star`-shaped invocation `http.webhook(...)`
// flowing through the PRODUCTION skytimeHTTP{}.Initialize() module
// produces a value satisfying extension.TriggerSource. This is distinct
// from callHTTPWebhook (which manually constructs a minimal module to
// keep Task 1 factory-level tests independent) — here we exercise the
// real Initialize path that the parser will use.
func TestExtension_StarFileCallsHttpWebhook(t *testing.T) {
	mod, err := skytimeHTTP{}.Initialize(&starlark.Thread{Name: "init"}, nil)
	require.NoError(t, err)

	predeclared := starlark.StringDict{"http": mod}

	thread := &starlark.Thread{Name: "exec"}
	src := `result = http.webhook(path="/star", method="POST")` + "\n"
	globals, err := starlark.ExecFile(thread, "star_file_call.star", src, predeclared)
	require.NoError(t, err, "the production Initialize() module must expose .webhook reachable from a .star source")

	v := globals["result"]
	require.NotNil(t, v)
	_, ok := v.(extension.TriggerSource)
	require.True(t, ok, "http.webhook(...) result must satisfy extension.TriggerSource")
}

// =============================================================================
// Plan 04 Task 1 — accessor methods + NewHTTPWebhookSourceForTest test
// constructor. Plan 04b reads SignatureAlgo / SignatureHeader / SecretCredID
// via type-assertion in handler.go::readSigningConfig, so this plan exposes
// them without changing any production behavior.
// =============================================================================

// TestHTTPWebhook_AccessorRoundTrip asserts the three accessor methods
// (SignatureAlgo, SignatureHeader, SecretCredID) round-trip the kwargs
// passed to NewHTTPWebhookSourceForTest exactly. Plan 04b reads these
// via type-assertion to a small interface; the names and signatures
// must remain stable.
func TestHTTPWebhook_AccessorRoundTrip(t *testing.T) {
	src := NewHTTPWebhookSourceForTest("/x", "POST", "secret-id", "sha512", "X-Custom-Sig")

	type accessors interface {
		SignatureAlgo() string
		SignatureHeader() string
		SecretCredID() string
	}
	a, ok := src.(accessors)
	require.True(t, ok, "NewHTTPWebhookSourceForTest result must expose SignatureAlgo/SignatureHeader/SecretCredID; got %T", src)

	require.Equal(t, "sha512", a.SignatureAlgo())
	require.Equal(t, "X-Custom-Sig", a.SignatureHeader())
	require.Equal(t, "secret-id", a.SecretCredID())
}

// TestHTTPWebhook_AccessorEmptyDefaults builds a source via the parser
// factory with secret_credential omitted (defaults to "" — unsigned mount
// per D-7.1-04) and signature_header omitted (defaults to "X-Signature"
// per D-7.1-03). The accessors return the parse-time defaults verbatim.
//
// This test goes through the parse-time factory (callHTTPWebhook), not
// NewHTTPWebhookSourceForTest, because the defaults are applied in the
// factory — the test constructor takes raw fields and applies no
// defaults. The factory path is what production uses.
func TestHTTPWebhook_AccessorEmptyDefaults(t *testing.T) {
	v, err := callHTTPWebhook(t, `http.webhook(path="/x", method="POST")`)
	require.NoError(t, err)

	type accessors interface {
		SignatureAlgo() string
		SignatureHeader() string
		SecretCredID() string
	}
	a, ok := v.(accessors)
	require.True(t, ok, "factory result must expose accessors; got %T", v)

	require.Equal(t, "sha256", a.SignatureAlgo(), "default signature_algo per D-7.1-03")
	require.Equal(t, "X-Signature", a.SignatureHeader(), "default signature_header per D-7.1-03")
	require.Equal(t, "", a.SecretCredID(), "unsigned mount when secret_credential omitted (D-7.1-04)")
}

// TestNewHTTPWebhookSourceForTest_SatisfiesContracts confirms the test
// constructor returns a value satisfying extension.TriggerSource AND
// receiver.HTTPMounter. Plan 04's mount tests use this constructor to
// build trigger fixtures without going through the parser.
func TestNewHTTPWebhookSourceForTest_SatisfiesContracts(t *testing.T) {
	src := NewHTTPWebhookSourceForTest("/hooks/test", "PUT", "", "sha256", "X-Signature")
	require.NotNil(t, src)

	// extension.TriggerSource (sealed)
	require.Equal(t, "http.webhook", src.Kind())
	require.Equal(t, []string{"headers", "payload"}, src.ReqSchema())

	// receiver.HTTPMounter
	mounter, ok := src.(receiver.HTTPMounter)
	require.True(t, ok, "value must satisfy receiver.HTTPMounter; got %T", src)
	path, method := mounter.HTTPMount()
	require.Equal(t, "/hooks/test", path)
	require.Equal(t, "PUT", method)
}
