package http_test

import (
	"errors"
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

// TestExtension_GetAcceptsNilCredential — Fix A nil-credential coverage
// (quick 260502-guu): the get Func must accept a nil Credential without
// dereferencing it. The served request must have NO Authorization header,
// proving applyCredential's nil short-circuit is intact and the activity
// can hand nil through when ActionRef.CredentialID == "".
func TestExtension_GetAcceptsNilCredential(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		require.Empty(t, r.Header.Get("Authorization"),
			"NilCredential: Authorization header MUST be absent when cred is nil")
		w.WriteHeader(204)
	}))
	defer srv.Close()

	ext := skyhttp.New()
	spec := ext.Operations()["get"]
	args := reflect.New(spec.KwargsType).Interface()
	v := reflect.ValueOf(args).Elem()
	v.FieldByName("BaseURL").SetString(srv.URL)
	v.FieldByName("Path").SetString("/")

	out, err := spec.Func(t.Context(), args, nil)
	require.NoError(t, err)
	resp, ok := out.(skyhttp.HTTPResponse)
	require.True(t, ok)
	require.Equal(t, 204, resp.Status)
}

// TestExtension_PostAcceptsNilCredential — Fix A nil-credential coverage:
// confirms the post Func + BodyArgs branch does not introduce a nil-deref
// of cred when applyCredential is short-circuited.
func TestExtension_PostAcceptsNilCredential(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		require.Empty(t, r.Header.Get("Authorization"),
			"NilCredential: Authorization header MUST be absent when cred is nil (post)")
		w.WriteHeader(202)
	}))
	defer srv.Close()

	ext := skyhttp.New()
	spec := ext.Operations()["post"]
	args := reflect.New(spec.KwargsType).Interface()
	v := reflect.ValueOf(args).Elem()
	v.FieldByName("BaseURL").SetString(srv.URL)
	v.FieldByName("Path").SetString("/upload")
	v.FieldByName("Body").SetString(`{"hello":"world"}`)

	out, err := spec.Func(t.Context(), args, nil)
	require.NoError(t, err)
	resp, ok := out.(skyhttp.HTTPResponse)
	require.True(t, ok)
	require.Equal(t, 202, resp.Status)
}

// TestExtension_Get_404_NonRetryable — Quick 260502-onc Fix A: non-2xx
// responses become first-class workflow failures. 4xx wraps with
// extension.ErrNonRetryable so the activity classifier surfaces a
// NonRetryable temporal.ApplicationError; the wrapped error message
// includes "HTTP 404" for renderer attribution.
func TestExtension_Get_404_NonRetryable(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	ext := skyhttp.New()
	spec := ext.Operations()["get"]
	require.NotNil(t, spec.Func)

	args := reflect.New(spec.KwargsType).Interface()
	v := reflect.ValueOf(args).Elem()
	v.FieldByName("BaseURL").SetString(srv.URL)
	v.FieldByName("Path").SetString("/nope")

	out, err := spec.Func(t.Context(), args, nil)
	require.Error(t, err, "404 must surface as error, not OkResult")
	require.Nil(t, out, "404 must NOT return an HTTPResponse output (avoid silent-success failure mode)")
	require.Contains(t, err.Error(), "HTTP 404",
		"error message must include HTTP status for renderer attribution")
	require.True(t, errors.Is(err, extension.ErrNonRetryable),
		"4xx must wrap extension.ErrNonRetryable — activity classifier branches on errors.Is")
}

// TestExtension_Get_500_Retryable — Quick 260502-onc Fix A: 5xx wraps as
// a plain error (no sentinel). The activity classifier's default-retryable
// branch lets the Temporal RetryPolicy do its job. Confirms the 4xx vs
// 5xx split is intentional, not accidental.
func TestExtension_Get_500_Retryable(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`internal server error`))
	}))
	defer srv.Close()

	ext := skyhttp.New()
	spec := ext.Operations()["get"]

	args := reflect.New(spec.KwargsType).Interface()
	v := reflect.ValueOf(args).Elem()
	v.FieldByName("BaseURL").SetString(srv.URL)
	v.FieldByName("Path").SetString("/oops")

	out, err := spec.Func(t.Context(), args, nil)
	require.Error(t, err, "5xx must surface as error")
	require.Nil(t, out)
	require.Contains(t, err.Error(), "HTTP 500", "error message must include HTTP status")
	require.False(t, errors.Is(err, extension.ErrNonRetryable),
		"5xx must NOT wrap ErrNonRetryable — Temporal must retry transient backend failures")
}

// TestExtension_Get_2xx_StillSuccess — Quick 260502-onc Fix A regression
// guard: 200, 204, and 299 (the upper edge of 2xx) all continue to return
// (HTTPResponse, nil) unchanged. Fix A must NOT widen what counts as
// failure beyond non-2xx.
func TestExtension_Get_2xx_StillSuccess(t *testing.T) {
	for _, status := range []int{200, 204, 299} {
		t.Run(stdhttp.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			ext := skyhttp.New()
			spec := ext.Operations()["get"]

			args := reflect.New(spec.KwargsType).Interface()
			v := reflect.ValueOf(args).Elem()
			v.FieldByName("BaseURL").SetString(srv.URL)
			v.FieldByName("Path").SetString("/")

			out, err := spec.Func(t.Context(), args, nil)
			require.NoError(t, err, "2xx must continue to succeed; got error: %v", err)
			resp, ok := out.(skyhttp.HTTPResponse)
			require.True(t, ok, "expected HTTPResponse; got %T", out)
			require.Equal(t, status, resp.Status)
		})
	}
}

// TestExtension_Post_422_NonRetryable — Quick 260502-onc Fix A: confirms
// the non-2xx classification applies to the body-bearing branch (post via
// asBodyArgs) too, not only the asGetArgs path.
func TestExtension_Post_422_NonRetryable(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`validation failed`))
	}))
	defer srv.Close()

	ext := skyhttp.New()
	spec := ext.Operations()["post"]
	require.NotNil(t, spec.Func)

	args := reflect.New(spec.KwargsType).Interface()
	v := reflect.ValueOf(args).Elem()
	v.FieldByName("BaseURL").SetString(srv.URL)
	v.FieldByName("Path").SetString("/issues")
	v.FieldByName("Body").SetString(`{"title":"x"}`)

	out, err := spec.Func(t.Context(), args, nil)
	require.Error(t, err)
	require.Nil(t, out)
	require.Contains(t, err.Error(), "HTTP 422")
	require.True(t, errors.Is(err, extension.ErrNonRetryable),
		"4xx on body-bearing op (POST) must also wrap ErrNonRetryable")
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
