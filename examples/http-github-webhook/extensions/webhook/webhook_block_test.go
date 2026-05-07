package webhook

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/mikelalcon/skytime/pkg/activity"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	extensiontesting "github.com/mikelalcon/skytime/pkg/extension/testing"
)

// newWebhookActivity is a test-local copy of pkg/activity/execute_batch_test.go's
// newIntegrationActivity helper, scoped to this file. It builds an *Activity
// registered with a fresh TestActivityEnvironment, with a single
// OperationSpec for "webhook.post" backed by a counter-incrementing mock.
//
// The Idempotent: extension.Ptr(false) flag MIRRORS the production webhook
// extension's declaration (see webhook.go's Operations()) — verified by
// TestExtension_PostIsNonIdempotent. The mock OperationFunc is the only
// piece replaced; the dispatch + validation logic exercised in this test
// is the REAL pkg/activity code.
//
// Returns the env, the Activity impl, and a *atomic.Int32 the test asserts
// against (calls.Load() == 2 for the two-invocation case).
func newWebhookActivity(t *testing.T) (*testsuite.TestActivityEnvironment, *activity.Activity, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	op := func(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
		calls.Add(1)
		return WebhookPostOutput{Status: 200, Body: `{"ok":true}`}, nil
	}
	dispatch := activity.OperationDispatch{
		"webhook.post": extension.OperationSpec{
			Name:       "post",
			Idempotent: extension.Ptr(false), // MUST match webhook.go's production declaration
			Func:       op,
		},
	}
	creds := map[string]extension.Credential{
		"webhook_url": &extension.BearerCredential{
			ID_:   "webhook_url",
			Token: extension.NewSecret("https://example.invalid/hook"),
		},
	}
	handler := &extensiontesting.FakeCredentialHandler{Creds: creds}
	impl, err := activity.New(dispatch, handler)
	require.NoError(t, err)

	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})
	return env, impl, &calls
}

// mkPostRef constructs a *dag.ActionRef for webhook.post via STRUCT LITERAL
// (there is no dag.NewActionRef constructor — see pkg/dag/action.go:23 +
// pkg/extension/builtin/http/http.go:150 for the canonical pattern).
func mkPostRef() *dag.ActionRef {
	kw := starlark.NewDict(1)
	if err := kw.SetKey(starlark.String("body"), starlark.String(`{"text":"hello"}`)); err != nil {
		panic(err)
	}
	return &dag.ActionRef{
		Kind_:        "webhook.post",
		Kwargs:       kw,
		CredentialID: "webhook_url",
	}
}

// TestWebhookPost_NonIdempotent_BatchOfTwo_RejectedByActivity asserts the
// activity layer's defense-in-depth: a 2-element batch of NON-idempotent
// webhook.post ActionRefs is rejected NonRetryable. Pins the upstream
// invariant (pkg/activity/validate_batch.go errTypeMultiNonIdempotent) at
// the example-project layer using the actual webhook extension's Idempotent
// declaration.
//
// The parser-side enforcement (D2-06: split at parse time) is upstream;
// this test is the activity-boundary defense — proving that even a
// hand-built batch can't sneak through and corrupt non-idempotent state
// via retry.
func TestWebhookPost_NonIdempotent_BatchOfTwo_RejectedByActivity(t *testing.T) {
	env, _, calls := newWebhookActivity(t)

	batch := []*dag.ActionRef{mkPostRef(), mkPostRef()}
	_, err := env.ExecuteActivity("ExecuteBatch", batch)
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable(), "non-idempotent multi-batch must be NonRetryable; got: %v", err)
	// validate_batch.go's errTypeMultiNonIdempotent error message contains
	// the substring "non-idempotent" — match permissively rather than
	// brittlely against the exact constant string.
	require.Contains(t, appErr.Error(), "non-idempotent",
		"expected error to mention non-idempotent rejection; got: %v", err)

	// The op MUST NOT have been invoked at all — rejection happens BEFORE
	// dispatch (validateBatch runs before the per-action loop).
	require.Equal(t, int32(0), calls.Load(),
		"op must not be invoked when batch is rejected upstream")
}

// TestWebhookPost_NonIdempotent_OneActivityInvocationPerActionRef is the
// load-bearing success-criterion-3 mechanical assertion. Two SEPARATE
// single-element ExecuteBatch invocations — each succeeds and each bumps
// the counter exactly once. counter == 2 after both calls.
//
// This is the Phase 6 proof that "block of two webhook.post ActionRefs
// results in two distinct activity-side invocations of the post operation"
// (CONTEXT.md success criterion 3) at the activity-dispatch boundary.
//
// The parser-side block-splitting (D2-06: a block of N non-idempotent
// ActionRefs becomes N single-element batches) is exercised by 06-06's
// TestFlows_* tests; this test pins the activity-side counterpart.
func TestWebhookPost_NonIdempotent_OneActivityInvocationPerActionRef(t *testing.T) {
	env, _, calls := newWebhookActivity(t)

	// First invocation: single-element batch.
	encoded1, err := env.ExecuteActivity("ExecuteBatch", []*dag.ActionRef{mkPostRef()})
	require.NoError(t, err, "first invocation must succeed")
	var results1 dag.ActionResults
	require.NoError(t, encoded1.Get(&results1))
	require.Len(t, results1, 1)
	_, isOk1 := results1[0].(dag.OkResult)
	require.True(t, isOk1, "first result must be OkResult; got %T", results1[0])

	// Second invocation: another single-element batch.
	encoded2, err := env.ExecuteActivity("ExecuteBatch", []*dag.ActionRef{mkPostRef()})
	require.NoError(t, err, "second invocation must succeed")
	var results2 dag.ActionResults
	require.NoError(t, encoded2.Get(&results2))
	require.Len(t, results2, 1)
	_, isOk2 := results2[0].(dag.OkResult)
	require.True(t, isOk2, "second result must be OkResult; got %T", results2[0])

	// The load-bearing assertion: the op was invoked exactly TWICE — once
	// per ActionRef, never batched. The literal `if calls != 2` form is
	// the success-criterion-3 mechanical pin.
	if calls := calls.Load(); calls != 2 {
		t.Fatalf("expected exactly 2 op invocations (one per ActionRef), got %d", calls)
	}
}
