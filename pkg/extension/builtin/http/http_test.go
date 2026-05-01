package http_test

import (
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
	"github.com/mikelalcon/skytime/pkg/parser"
)

// TestExtension_Name pins the namespace identifier "http" — the same name
// the parser stores in parseTimeGlobals so .star sources access via http.endpoint(...).
func TestExtension_Name(t *testing.T) {
	ext := skyhttp.New()
	require.Equal(t, "http", ext.Name())
}

// TestExtension_OperationsIdempotenceMatchesD4_14 asserts the locked
// D4-14 idempotence declarations verbatim. RFC-7231 considers PUT and
// DELETE idempotent, but D4-14 is a locked user decision and overrides.
// This test pins the contract — any future change requires a CONTEXT.md
// amendment.
func TestExtension_OperationsIdempotenceMatchesD4_14(t *testing.T) {
	ext := skyhttp.New()
	ops := ext.Operations()

	expected := map[string]bool{
		"get":    true,
		"head":   true,
		"post":   false,
		"put":    false, // D4-14 override; RFC-7231 says true
		"delete": false, // D4-14 override; RFC-7231 says true
	}
	for name, want := range expected {
		spec, ok := ops[name]
		require.True(t, ok, "operation %q must be registered", name)
		require.NotNil(t, spec.Idempotent, "operation %q Idempotent must be non-nil", name)
		require.Equal(t, want, *spec.Idempotent, "operation %q idempotent flag (D4-14)", name)
	}
}

// TestExtension_KwargsTypeShapes verifies the schema split: get/head/delete
// use GetArgs (no body field); post/put use BodyArgs (with body field).
func TestExtension_KwargsTypeShapes(t *testing.T) {
	ext := skyhttp.New()
	ops := ext.Operations()

	for _, name := range []string{"get", "head", "delete"} {
		spec := ops[name]
		require.NotNil(t, spec.KwargsType, "op %q must have KwargsType", name)
		require.Equal(t, "GetArgs", spec.KwargsType.Name(),
			"op %q must use GetArgs schema (no body)", name)
	}
	for _, name := range []string{"post", "put"} {
		spec := ops[name]
		require.NotNil(t, spec.KwargsType, "op %q must have KwargsType", name)
		require.Equal(t, "BodyArgs", spec.KwargsType.Name(),
			"op %q must use BodyArgs schema (with body)", name)
		_, ok := spec.KwargsType.FieldByName("Body")
		require.True(t, ok, "BodyArgs must have a Body field for op %q", name)
	}
}

// TestExtension_RegistrationViaRegistry: the extension is acceptable
// to extension.Registry — Idempotent non-nil, KwargsType non-nil, Func
// non-nil for every op.
func TestExtension_RegistrationViaRegistry(t *testing.T) {
	reg := extension.NewRegistry()
	require.NoError(t, reg.Register(skyhttp.New()))
}

// TestExtension_GetSucceedsAgainstHTTPTestServer is the e2e smoke for the
// GET activity-side Func. Uses httptest.NewServer; no real network.
func TestExtension_GetSucceedsAgainstHTTPTestServer(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		require.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	ext := skyhttp.New()
	spec := ext.Operations()["get"]
	require.NotNil(t, spec.Func)

	// Construct a *GetArgs as if the activity layer had decoded kwargs.
	argsType := spec.KwargsType
	args := reflect.New(argsType).Interface()
	v := reflect.ValueOf(args).Elem()
	v.FieldByName("BaseURL").SetString(srv.URL)
	v.FieldByName("Path").SetString("/")

	out, err := spec.Func(t.Context(), args, nil)
	require.NoError(t, err)
	resp, ok := out.(skyhttp.HTTPResponse)
	require.True(t, ok, "expected HTTPResponse; got %T", out)
	require.Equal(t, 200, resp.Status)
	require.Contains(t, string(resp.Body), `"ok": true`)
}

// TestExtension_PostSendsBody verifies the post Func dispatches the body
// bytes; defends against accidentally inverting the GetArgs/BodyArgs
// branch in OperationFunc dispatch.
func TestExtension_PostSendsBody(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		require.Equal(t, "POST", r.Method)
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.WriteHeader(201)
	}))
	defer srv.Close()

	ext := skyhttp.New()
	spec := ext.Operations()["post"]
	require.NotNil(t, spec.Func)

	argsType := spec.KwargsType
	args := reflect.New(argsType).Interface()
	v := reflect.ValueOf(args).Elem()
	v.FieldByName("BaseURL").SetString(srv.URL)
	v.FieldByName("Path").SetString("/upload")
	v.FieldByName("Body").SetString(`{"hello":"world"}`)

	out, err := spec.Func(t.Context(), args, nil)
	require.NoError(t, err)
	resp, ok := out.(skyhttp.HTTPResponse)
	require.True(t, ok)
	require.Equal(t, 201, resp.Status)
	require.Equal(t, `{"hello":"world"}`, string(receivedBody))
}

// TestExtension_BearerCredentialApplied verifies routing: a Bearer
// credential becomes "Authorization: Bearer <token>".
func TestExtension_BearerCredentialApplied(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(204)
	}))
	defer srv.Close()

	ext := skyhttp.New()
	spec := ext.Operations()["get"]
	args := reflect.New(spec.KwargsType).Interface()
	v := reflect.ValueOf(args).Elem()
	v.FieldByName("BaseURL").SetString(srv.URL)
	v.FieldByName("Path").SetString("/")

	cred := &extension.BearerCredential{
		ID_:   "test",
		Token: extension.NewSecret("abc123"),
	}
	_, err := spec.Func(t.Context(), args, cred)
	require.NoError(t, err)
	require.Equal(t, "Bearer abc123", gotAuth)
}

// TestExtension_RegistersAndParsesAFlow (W-3 BEHAVIOR GATE) is the
// load-bearing assertion that the chosen Initialize-return shape works
// against the existing parser. Without this gate, the plan accepts
// forward-leaning starlarkstruct usage that may not match what
// pkg/parser/globals.go expects when storing Initialize's return value.
//
// If this fails, the chosen shape doesn't match what
// pkg/parser/globals.go expects — adjust the shape (Module vs Struct
// vs Builtin) and re-run.
func TestExtension_RegistersAndParsesAFlow(t *testing.T) {
	src := []byte("gh = http.endpoint(base_url='https://x')\n" +
		"flow(name='t', inputs={}, steps=[step(action=gh.get(path='/y'))])\n")

	p, err := parser.NewParser(parser.WithExtensions(skyhttp.New()))
	require.NoError(t, err)

	_, err = p.ParseSource("behavior_gate.star", src)
	require.NoError(t, err,
		"http.endpoint(...).get(path=...) must parse cleanly through parser.NewParser")
}
