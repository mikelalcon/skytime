// Package http provides the baked-in generic HTTP extension shipped
// with cmd/skytime per D4-14 (Phase 4 CONTEXT.md).
//
// Surface:
//
//	gh = http.endpoint(base_url="https://api.example.com", credential="my_id")
//	step(action=gh.get(path="/repos/foo/bar"))
//	step(action=gh.post(path="/issues", body='{"title":"x"}'))
//
// Five operations, with D4-14-locked idempotence flags:
//
//	.get(path=..., headers=...)             Idempotent: true
//	.head(path=..., headers=...)            Idempotent: true
//	.post(path=..., body=..., headers=...)  Idempotent: false
//	.put(path=..., body=..., headers=...)   Idempotent: false (D4-14 override; RFC-7231 says true)
//	.delete(path=..., headers=...)          Idempotent: false (D4-14 override; RFC-7231 says true)
//
// RFC-7231 considers PUT and DELETE idempotent. D4-14 is a locked user
// decision and overrides the RFC; consultants in Phase 6 declare
// per-op idempotence on real GitHub/Slack flows where the application
// semantics differ from the protocol-layer guarantees.
//
// Implementation: net/http stdlib only. No third-party HTTP client
// libraries (D4-14).
package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"reflect"
	"strings"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// GetArgs is the kwargs schema for .get / .head / .delete. The
// `star:"path,required"` tag drives extension.UnpackOperationKwargs.
// base_url is captured at endpoint() factory time and stored on the
// per-method Builtin; the parser-side builtin injects base_url into the
// kwargs Dict so the activity-side OperationFunc can reconstruct the URL
// without re-routing through the closure.
type GetArgs struct {
	BaseURL string            `star:"base_url,required"`
	Path    string            `star:"path,required"`
	Headers map[string]string `star:"headers"`
}

// BodyArgs is the kwargs schema for .post / .put. body is optional
// (a .put may be body-less for cache-invalidation semantics).
type BodyArgs struct {
	BaseURL string            `star:"base_url,required"`
	Path    string            `star:"path,required"`
	Body    string            `star:"body"`
	Headers map[string]string `star:"headers"`
}

// skytimeHTTP is the Extension implementation. New() returns an instance.
type skytimeHTTP struct{}

// New constructs the baked-in HTTP extension.
func New() extension.Extension { return skytimeHTTP{} }

// Name returns "http" — the global key parser/globals.go binds the
// Initialize return value under.
func (skytimeHTTP) Name() string { return "http" }

// Initialize returns the Starlark-side namespace value. Convention from
// pkg/parser/builtins_test.go's fakeExtension: a *starlarkstruct.Module
// whose attribute "endpoint" is a *starlark.Builtin factory. The
// parser's globals dispatcher (pkg/parser/globals.go) gates on
// starlark.HasAttrs and stores the return value under Name(); .star
// authors then write `http.endpoint(...)` (attribute lookup → call).
//
// Phase 7.1 added `webhook` as a second module attribute (D-7.1-01) —
// the inbound counterpart of the outbound `endpoint`. One coherent
// `http.*` namespace, both directions of the HTTP boundary.
func (skytimeHTTP) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	return &starlarkstruct.Module{
		Name: "http",
		Members: starlark.StringDict{
			"endpoint": starlark.NewBuiltin("http.endpoint", endpointFactory),
			"webhook":  starlark.NewBuiltin("http.webhook", webhookFactory),
		},
	}, nil
}

// Operations returns the 5-operation map with D4-14-locked idempotence.
// extension.Registry validates each spec at registration time.
func (skytimeHTTP) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"get":    {Name: "get", Idempotent: extension.Ptr(true), Func: doGet, KwargsType: reflect.TypeOf(GetArgs{}), DefaultTimeout: 30 * time.Second},
		"head":   {Name: "head", Idempotent: extension.Ptr(true), Func: doHead, KwargsType: reflect.TypeOf(GetArgs{}), DefaultTimeout: 30 * time.Second},
		"post":   {Name: "post", Idempotent: extension.Ptr(false), Func: doPost, KwargsType: reflect.TypeOf(BodyArgs{}), DefaultTimeout: 30 * time.Second},
		"put":    {Name: "put", Idempotent: extension.Ptr(false), Func: doPut, KwargsType: reflect.TypeOf(BodyArgs{}), DefaultTimeout: 30 * time.Second},
		"delete": {Name: "delete", Idempotent: extension.Ptr(false), Func: doDelete, KwargsType: reflect.TypeOf(GetArgs{}), DefaultTimeout: 30 * time.Second},
	}
}

// ---------------------------------------------------------------------------
// Starlark-side factory and per-method builtins
// ---------------------------------------------------------------------------

// endpointFactory implements http.endpoint(base_url=..., credential=...).
// Returns a *starlarkstruct.Module whose attributes are .get, .head,
// .post, .put, .delete — each a *starlark.Builtin that produces an
// *dag.ActionRef when called from .star.
func endpointFactory(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var baseURL, credential string
	if err := starlark.UnpackArgs("http.endpoint", args, kwargs,
		"base_url", &baseURL,
		"credential?", &credential,
	); err != nil {
		return nil, err
	}
	return &starlarkstruct.Module{
		Name: "http.endpoint",
		Members: starlark.StringDict{
			"get":    newMethodBuiltin("http.get", baseURL, credential),
			"head":   newMethodBuiltin("http.head", baseURL, credential),
			"post":   newMethodBuiltin("http.post", baseURL, credential),
			"put":    newMethodBuiltin("http.put", baseURL, credential),
			"delete": newMethodBuiltin("http.delete", baseURL, credential),
		},
	}, nil
}

// newMethodBuiltin constructs a *starlark.Builtin that, when called
// from Starlark, produces a *dag.ActionRef. base_url and credential are
// captured by closure; the per-call kwargs (path, body, headers) come
// in at call time.
func newMethodBuiltin(kind, baseURL, credential string) *starlark.Builtin {
	return starlark.NewBuiltin(kind, func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		// Build the kwargs Dict the parser stores on the ActionRef.
		// Always inject base_url so the activity-side OperationFunc can
		// reconstruct the URL without round-tripping through the closure.
		outDict := starlark.NewDict(len(kwargs) + 1)
		if err := outDict.SetKey(starlark.String("base_url"), starlark.String(baseURL)); err != nil {
			return nil, err
		}
		for _, kv := range kwargs {
			if err := outDict.SetKey(kv[0], kv[1]); err != nil {
				return nil, err
			}
		}
		outDict.Freeze()
		return &dag.ActionRef{
			Pos:          callerPosition(thread),
			Kind_:        kind,
			Kwargs:       outDict,
			CredentialID: credential,
		}, nil
	})
}

// callerPosition extracts the .star call-site position. Mirrors
// parser.callerPosition (Pitfall #3): use thread.CallFrame(1).Pos so
// errors point at the .star site, not the builtin def site. Returns
// the zero value when the call stack is too shallow (defensive — every
// .star-driven invocation will have depth >= 2).
func callerPosition(thread *starlark.Thread) syntax.Position {
	if thread.CallStackDepth() < 2 {
		return syntax.Position{}
	}
	return thread.CallFrame(1).Pos
}

// ---------------------------------------------------------------------------
// Activity-side OperationFunc implementations
// ---------------------------------------------------------------------------

// asGetArgs accepts either GetArgs (value) or *GetArgs (pointer) so the
// op functions stay tolerant to caller convention. The activity-side
// decoder (pkg/activity/action_executor.go decodeActionRefKwargs)
// returns the decoded struct as a VALUE per its documented contract;
// the http_test.go tier passes a POINTER built via
// `reflect.New(argsType).Interface()`. Both must work.
//
// Quick 260502-guu Rule 1 fix: previously these doX funcs hard-cast to
// *GetArgs / *BodyArgs which panicked at runtime for any flow whose
// activity layer uses the production decoder. The pre-existing test
// coverage masked the bug because tests built pointer-typed args.
func asGetArgs(args any) *GetArgs {
	if p, ok := args.(*GetArgs); ok {
		return p
	}
	v := args.(GetArgs)
	return &v
}

func asBodyArgs(args any) *BodyArgs {
	if p, ok := args.(*BodyArgs); ok {
		return p
	}
	v := args.(BodyArgs)
	return &v
}

func doGet(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asGetArgs(args)
	return doHTTP(ctx, "GET", a.BaseURL, a.Path, nil, a.Headers, cred)
}

func doHead(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asGetArgs(args)
	return doHTTP(ctx, "HEAD", a.BaseURL, a.Path, nil, a.Headers, cred)
}

func doPost(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asBodyArgs(args)
	return doHTTP(ctx, "POST", a.BaseURL, a.Path, []byte(a.Body), a.Headers, cred)
}

func doPut(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asBodyArgs(args)
	return doHTTP(ctx, "PUT", a.BaseURL, a.Path, []byte(a.Body), a.Headers, cred)
}

func doDelete(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asGetArgs(args)
	return doHTTP(ctx, "DELETE", a.BaseURL, a.Path, nil, a.Headers, cred)
}

// doHTTP is the shared net/http dispatch. ctx-aware so activity-level
// timeouts cancel in-flight requests; net/http stdlib only (D4-14).
func doHTTP(ctx context.Context, method, baseURL, path string, body []byte, headers map[string]string, cred extension.Credential) (dag.OperationOutput, error) {
	url := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("http %s %s: build request: %w", method, url, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	applyCredential(req, cred)

	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http %s %s: read body: %w", method, url, err)
	}
	// Fix A (quick 260502-onc): non-2xx responses are first-class
	// workflow failures. 4xx → wrap with extension.ErrNonRetryable so
	// the activity classifier surfaces NonRetryable
	// temporal.ApplicationError. 5xx → plain wrapped error so the
	// activity's default-retryable branch lets the Temporal RetryPolicy
	// do its job. 2xx falls through to the success return below
	// unchanged.
	if resp.StatusCode >= 400 {
		bodySnippet := string(respBody)
		if len(bodySnippet) > 200 {
			bodySnippet = bodySnippet[:200] + "..."
		}
		if resp.StatusCode < 500 {
			return nil, fmt.Errorf("HTTP %d %s %s: %s: %w",
				resp.StatusCode, method, url, bodySnippet, extension.ErrNonRetryable)
		}
		// 5xx (and any other >=500): plain wrapped error → retryable.
		return nil, fmt.Errorf("HTTP %d %s %s: %s",
			resp.StatusCode, method, url, bodySnippet)
	}
	respHeaders := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		respHeaders[k] = strings.Join(v, ", ")
	}
	return HTTPResponse{
		Status:  resp.StatusCode,
		Body:    respBody,
		Headers: respHeaders,
	}, nil
}

// applyCredential routes by credential kind:
//
//	BearerCredential → "Authorization: Bearer <token>"
//	BasicCredential  → "Authorization: Basic base64(user:password)" via stdlib SetBasicAuth
//	APIKeyCredential → req.Header.Set(HeaderName, key)  (default header: "Authorization")
//
// Nil credential is a no-op (some endpoints are public). Unknown kinds
// are ignored — sealed Credential interface guarantees only the three
// known concrete types reach here, so the default case is unreachable
// without modifying pkg/extension/credential.go.
func applyCredential(req *stdhttp.Request, cred extension.Credential) {
	if cred == nil {
		return
	}
	switch c := cred.(type) {
	case *extension.BearerCredential:
		req.Header.Set("Authorization", "Bearer "+c.Token.Reveal())
	case *extension.BasicCredential:
		req.SetBasicAuth(c.User, c.Password.Reveal())
	case *extension.APIKeyCredential:
		header := c.HeaderName
		if header == "" {
			header = "Authorization"
		}
		req.Header.Set(header, c.Key.Reveal())
	}
}
