package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// TestExtension_RegistersWithoutError — basic shape: Name() + non-nil Idempotent.
//
// A non-nil Idempotent pointer for every op is the contract enforced at
// registration time by extension.Registry (returns ErrIdempotentRequired
// otherwise). This test pins it so a future op addition with a forgotten
// declaration fails here, not at first registration in cmd/extbin.
func TestExtension_RegistersWithoutError(t *testing.T) {
	ext := New()
	assert.Equal(t, "webhook", ext.Name())

	ops := ext.Operations()
	require.Contains(t, ops, "post")
	require.NotNil(t, ops["post"].Idempotent, "post.Idempotent is nil → would trigger ErrIdempotentRequired at registration")
	require.NotNil(t, ops["post"].Func)
}

// TestExtension_PostIsNonIdempotent pins the load-bearing non-idempotency
// declaration. Success criterion 3 depends on this; do not flip without
// a CONTEXT.md amendment.
func TestExtension_PostIsNonIdempotent(t *testing.T) {
	ops := New().Operations()
	spec, ok := ops["post"]
	require.True(t, ok)
	require.NotNil(t, spec.Idempotent)
	assert.False(t, *spec.Idempotent, "webhook.post MUST be non-idempotent (CONTEXT.md D-WEBHOOK-OPS)")
}

// TestExtension_OutputImplementsOperationOutput compile + runtime checks.
func TestExtension_OutputImplementsOperationOutput(t *testing.T) {
	var _ dag.OperationOutput = WebhookPostOutput{}
	WebhookPostOutput{}.IsOperationOutput()
}

// newBearerCred is a test helper for building the bearer-as-URL credential
// the webhook extension consumes. The "Token IS the URL" pattern is the
// CONTEXT.md D-WEBHOOK-HOST decision.
func newBearerCred(id, url string) *extension.BearerCredential {
	return &extension.BearerCredential{ID_: id, Token: extension.NewSecret(url)}
}

// TestDoPost_HappyPath_2xx asserts the request body and headers reach the
// server unchanged + a 200 response is wrapped into WebhookPostOutput.
// Pins:
//   - body bytes round-trip exactly
//   - default Content-Type is application/json
//   - user-supplied headers survive
//   - WebhookPostOutput.Status + Body match server response
func TestDoPost_HappyPath_2xx(t *testing.T) {
	var receivedBody string
	var receivedCT string
	var receivedXCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		receivedCT = r.Header.Get("Content-Type")
		receivedXCustom = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cred := newBearerCred("webhook_url", srv.URL)
	out, err := doPost(context.Background(), &PostArgs{
		Body:    `{"text":"hello"}`,
		Headers: map[string]string{"X-Custom": "v"},
	}, cred)
	require.NoError(t, err)

	wp, ok := out.(WebhookPostOutput)
	require.True(t, ok)
	assert.Equal(t, 200, wp.Status)
	assert.Equal(t, `{"ok":true}`, wp.Body)
	assert.Equal(t, `{"text":"hello"}`, receivedBody)
	assert.Equal(t, "application/json", receivedCT)
	assert.Equal(t, "v", receivedXCustom)
}

// TestDoPost_4xx_WrapsErrNonRetryable — receiver returns 404 → ErrNonRetryable.
//
// 4xx is a client-side contract violation; retrying with the same payload
// yields the same failure. The activity layer's isRetryable check
// (errors.Is(err, ErrNonRetryable)) surfaces this as a NonRetryable
// temporal.ApplicationError.
func TestDoPost_4xx_WrapsErrNonRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cred := newBearerCred("webhook_url", srv.URL)
	_, err := doPost(context.Background(), &PostArgs{Body: "x"}, cred)
	require.Error(t, err)
	assert.True(t, errors.Is(err, extension.ErrNonRetryable),
		"expected 4xx → ErrNonRetryable, got: %v", err)
}

// TestDoPost_5xx_DoesNotWrapErrNonRetryable — 503 → retryable (Temporal must retry).
//
// This is the LOAD-BEARING half of the classification: if errors.Is(err,
// ErrNonRetryable) returns true for a 5xx, Temporal will not retry and the
// user-facing semantics are wrong. Pin firmly.
func TestDoPost_5xx_DoesNotWrapErrNonRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cred := newBearerCred("webhook_url", srv.URL)
	_, err := doPost(context.Background(), &PostArgs{Body: "x"}, cred)
	require.Error(t, err)
	assert.False(t, errors.Is(err, extension.ErrNonRetryable),
		"5xx must NOT be ErrNonRetryable so Temporal retries; got: %v", err)
}

// TestDoPost_MissingCredential_ErrNonRetryable — nil credential → ErrNonRetryable.
//
// A configuration bug — no destination URL means no useful work. Wrap so
// the activity classifies it permanent and the workflow surfaces the error
// without retry pressure on the operator.
func TestDoPost_MissingCredential_ErrNonRetryable(t *testing.T) {
	_, err := doPost(context.Background(), &PostArgs{Body: "x"}, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, extension.ErrNonRetryable))
}

// TestDoPost_WrongCredentialType_ErrNonRetryable — APIKeyCredential rejected.
//
// Defense-in-depth: the credfile loader produces typed credentials; if a
// future credfile schema regression mis-types a webhook entry as apikey,
// surface it as a permanent failure rather than a confusing nil-deref or
// silent zero URL.
func TestDoPost_WrongCredentialType_ErrNonRetryable(t *testing.T) {
	cred := &extension.APIKeyCredential{ID_: "x", HeaderName: "X-Y", Key: extension.NewSecret("z")}
	_, err := doPost(context.Background(), &PostArgs{Body: "x"}, cred)
	require.Error(t, err)
	assert.True(t, errors.Is(err, extension.ErrNonRetryable))
}

// TestExtension_PostKwargsType — drift catcher: KwargsType reflects PostArgs
// so the activity-side unpacker can decode the kwargs Dict.
func TestExtension_PostKwargsType(t *testing.T) {
	ops := New().Operations()
	spec := ops["post"]
	require.NotNil(t, spec)
	assert.Equal(t, reflect.TypeOf(PostArgs{}), spec.KwargsType)
}
