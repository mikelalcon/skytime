package webhook

import (
	"bytes"
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"reflect"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// skytimeWebhook is the webhook extension. Generic outbound POST —
// the destination URL is the secret (CONTEXT.md D-WEBHOOK-HOST).
//
// One operation: post. Declared NON-IDEMPOTENT so the parser splits
// any block of N webhook.post ActionRefs into N single-action batches
// at parse time (D2-06), and the activity layer rejects multi-action
// non-idempotent batches at runtime as defense-in-depth (D2-06 +
// pkg/activity/validate_batch.go errTypeMultiNonIdempotent).
type skytimeWebhook struct{}

// New constructs the webhook extension for registration via cli.WithExtensions.
func New() extension.Extension { return skytimeWebhook{} }

// Name returns the Starlark namespace identifier "webhook".
func (skytimeWebhook) Name() string { return "webhook" }

// Initialize returns the parser-time `webhook` namespace value with
// one factory builtin: client(credential=...). Authoring surface:
//
//	wh = webhook.client(credential = "webhook_url")
//	step(action = wh.post(body = '{"text": "hello"}'))
//
// The credential MUST be a BearerCredential whose Token is the
// destination URL. Configure the URL in your credfile under whatever
// ID you reference here.
func (skytimeWebhook) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	return &starlarkstruct.Module{
		Name: "webhook",
		Members: starlark.StringDict{
			"client": starlark.NewBuiltin("webhook.client", clientFactory),
		},
	}, nil
}

// Operations declares the single op with NON-IDEMPOTENT semantics.
//
// webhook.post is the load-bearing demo of one-action-per-activity-
// invocation: even when authored inside a `block`, the activity layer
// (pkg/activity/execute_batch.go) MUST run each post as its own
// ExecuteBatch invocation. This contract is enforced upstream; the
// declaration below is the single source of truth that drives the
// routing.
func (skytimeWebhook) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"post": {
			Name:           "post",
			Idempotent:     extension.Ptr(false), // load-bearing non-idempotency (CONTEXT.md success criterion 3)
			Func:           doPost,
			KwargsType:     reflect.TypeOf(PostArgs{}),
			DefaultTimeout: 30 * time.Second,
		},
	}
}

// Compile-time interface check.
var _ extension.Extension = skytimeWebhook{}

// PostArgs is the kwargs schema for webhook.post. body is required —
// the receiver gets exactly this byte sequence (Content-Type defaults
// to application/json; override via the headers map).
type PostArgs struct {
	Body    string            `star:"body,required"`
	Headers map[string]string `star:"headers"`
}

// clientFactory implements `webhook.client(credential="webhook_url")`.
// The credential's bearer Token is the destination URL.
//
// Why credential is required (not "credential?"): a webhook with no
// destination URL is meaningless — declare it required at the parse
// layer. (Different from http.endpoint(), which supports unauthenticated
// public-API access.)
func clientFactory(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var credential string
	if err := starlark.UnpackArgs("webhook.client", args, kwargs, "credential", &credential); err != nil {
		return nil, err
	}
	if credential == "" {
		return nil, fmt.Errorf("webhook.client: credential is required (the URL is the secret)")
	}
	return &starlarkstruct.Module{
		Name: "webhook.client",
		Members: starlark.StringDict{
			"post": newPostBuiltin(credential),
		},
	}, nil
}

// newPostBuiltin returns the Starlark builtin for `wh.post(body=, headers=)`.
// It captures the credential ID at factory time and emits an *dag.ActionRef
// carrying Kind="webhook.post", Kwargs={body, headers}, CredentialID=credential.
//
// Construction is via STRUCT LITERAL — there is no dag.NewActionRef
// constructor. Pattern verified at pkg/extension/builtin/http/http.go:150-155.
func newPostBuiltin(credential string) *starlark.Builtin {
	return starlark.NewBuiltin("webhook.post", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		d := starlark.NewDict(len(kwargs))
		for _, kv := range kwargs {
			if err := d.SetKey(kv[0], kv[1]); err != nil {
				return nil, fmt.Errorf("webhook.post: bad kwarg: %w", err)
			}
		}
		return &dag.ActionRef{
			Pos:          callerPosition(thread),
			Kind_:        "webhook.post", // "<extension>.<op>" — keyed by activity dispatch table
			Kwargs:       d,
			CredentialID: credential,
		}, nil
	})
}

// callerPosition extracts the .star call-site position. Mirrors the
// pkg/extension/builtin/http pattern (and parser.callerPosition):
// thread.CallFrame(1).Pos so errors point at the .star call site, not
// the builtin def site. Returns the zero value when the call stack is
// too shallow (defensive — every .star-driven invocation has depth >= 2).
func callerPosition(thread *starlark.Thread) syntax.Position {
	if thread.CallStackDepth() < 2 {
		return syntax.Position{}
	}
	return thread.CallFrame(1).Pos
}

// doPost issues the HTTP POST. Only stdlib net/http; no third-party client.
//
// Classification:
//   - cred missing or wrong type → ErrNonRetryable (configuration bug; retry won't help)
//   - 2xx/3xx → success, returns WebhookPostOutput
//   - 4xx → wraps ErrNonRetryable (client-side error; retry won't help)
//   - 5xx → returns plain wrapped error (transient; Temporal retries)
//   - transport error → returns plain wrapped error (transient)
func doPost(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asPostArgs(args)

	bearer, ok := cred.(*extension.BearerCredential)
	if !ok || bearer == nil {
		return nil, fmt.Errorf("webhook.post: requires a bearer credential whose token is the destination URL: %w", extension.ErrNonRetryable)
	}
	url := bearer.Token.Reveal()
	if url == "" {
		return nil, fmt.Errorf("webhook.post: credential %q has empty URL: %w", bearer.ID(), extension.ErrNonRetryable)
	}

	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, url, bytes.NewReader([]byte(a.Body)))
	if err != nil {
		// Malformed URL: ErrNonRetryable.
		return nil, fmt.Errorf("webhook.post: build request: %w: %s", extension.ErrNonRetryable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range a.Headers {
		req.Header.Set(k, v) // user override last-write-wins
	}

	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		// Transport error — DNS, dial, TLS handshake. Temporal retries.
		return nil, fmt.Errorf("webhook.post: transport: %w", err)
	}
	defer resp.Body.Close()

	const maxBody = 16 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("webhook.post: read response body: %w", err)
	}

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return nil, fmt.Errorf("webhook.post: HTTP %d: %s: %w", resp.StatusCode, string(body), extension.ErrNonRetryable)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("webhook.post: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return WebhookPostOutput{Status: resp.StatusCode, Body: string(body)}, nil
}

// asPostArgs accepts either PostArgs (value) or *PostArgs (pointer) so
// the op stays tolerant to caller convention. The activity-side decoder
// (pkg/activity/action_executor.go decodeActionRefKwargs) returns the
// decoded struct as a VALUE; tests build pointer-typed args via
// reflect.New. Both must work — same pattern as the pkg/extension/builtin/http
// extension (asGetArgs / asBodyArgs).
func asPostArgs(args any) *PostArgs {
	if p, ok := args.(*PostArgs); ok {
		return p
	}
	v := args.(PostArgs)
	return &v
}
