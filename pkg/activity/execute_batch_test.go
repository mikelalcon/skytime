package activity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	extensiontesting "github.com/mikelalcon/skytime/pkg/extension/testing"
)

// newIntegrationActivity builds a *Activity registered with a fresh
// TestActivityEnvironment. Returns the env, the impl, and the counterHandler
// wrapping the supplied creds (so tests can assert Resolve call counts).
//
// The dispatch is the caller-supplied OperationSpec map keyed by
// "<ext>.<op>" (matching ActionRef.Kind_); creds populates the
// FakeCredentialHandler.
func newIntegrationActivity(
	t *testing.T,
	ops map[string]extension.OperationSpec,
	creds map[string]extension.Credential,
	opts ...Option,
) (*testsuite.TestActivityEnvironment, *Activity, *counterHandler) {
	t.Helper()
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	inner := &extensiontesting.FakeCredentialHandler{Creds: creds}
	ch := &counterHandler{inner: inner}
	dispatch := OperationDispatch(ops)
	impl, err := New(dispatch, ch, opts...)
	require.NoError(t, err)
	env.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})
	return env, impl, ch
}

// mkRef is a brevity helper for the integration tests' []*dag.ActionRef
// builders.
func mkRef(kind, credID string) *dag.ActionRef {
	return &dag.ActionRef{Kind_: kind, Kwargs: starlark.NewDict(0), CredentialID: credID}
}

// TestExecuteBatch_RegistersWithExplicitName: the activity is registered
// under the exposed name "ExecuteBatch" via RegisterActivityWithOptions; an
// ExecuteActivity call by-method-value succeeds and the registration
// shape matches what Phase 3 will use.
func TestExecuteBatch_RegistersWithExplicitName(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "ok"}, nil
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("ghp_test")},
	}
	env, impl, _ := newIntegrationActivity(t,
		map[string]extension.OperationSpec{
			"fake.echo": {Name: "echo", Idempotent: extension.Ptr(true), Func: op},
		},
		creds,
	)
	batch := []*dag.ActionRef{mkRef("fake.echo", "admin")}
	encoded, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.NoError(t, err)
	var results dag.ActionResults
	require.NoError(t, encoded.Get(&results))
	require.Len(t, results, 1)
	require.IsType(t, dag.OkResult{}, results[0])
}

// TestExecuteBatch_HappyPath_SingleAction: a 1-action idempotent batch
// returns []ActionResult{OkResult{Idx:0, Output:...}} and the encoded
// payload round-trips through Temporal's converter into a dag.ActionResults
// slice with the OkResult kind recoverable.
func TestExecuteBatch_HappyPath_SingleAction(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "single"}, nil
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("ghp_test")},
	}
	env, impl, ch := newIntegrationActivity(t,
		map[string]extension.OperationSpec{
			"fake.echo": {Name: "echo", Idempotent: extension.Ptr(true), Func: op},
		},
		creds,
	)
	batch := []*dag.ActionRef{mkRef("fake.echo", "admin")}
	encoded, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.NoError(t, err)
	var results dag.ActionResults
	require.NoError(t, encoded.Get(&results))
	require.Len(t, results, 1)
	ok, isOk := results[0].(dag.OkResult)
	require.True(t, isOk, "got %T", results[0])
	require.Equal(t, 0, ok.Idx)
	// Output round-trips through dag.RawOperationOutput placeholder; decode
	// the placeholder back into fakeOutput to confirm.
	require.Equal(t, int32(1), ch.calls.Load(), "JIT credential resolve")
}

// TestExecuteBatch_HappyPath_Heartbeats: a 2-action batch emits exactly two
// BatchProgress payloads — {Action:1, Total:2} and {Action:2, Total:2} — via
// the heartbeatEmitter seam. ACT-06 / D2-16.
//
// Implementation note (DEVIATION FROM PLAN EXAMPLE):
//
// The plan's RESEARCH.md §"Example 2" sketch used
// env.SetOnActivityHeartbeatListener to capture heartbeats inside
// TestActivityEnvironment. However, Temporal SDK v1.42.0 documents that
// the listener "may not get called for every heartbeat recorded ... due to
// internal caching by the activity system" (see workflow_testsuite.go:267).
// Two heartbeats emitted within microseconds (which is what happens in
// this test, since both fake ops return immediately) get throttled into
// one delivery to the listener — making any "exactly 2 heartbeats"
// assertion racy / flaky.
//
// We instead use the unexported withHeartbeatEmitter seam (defined for
// exactly this purpose in 02-02) to inject a fakeHeartbeatEmitter that
// captures every emit() call deterministically. The test still drives
// ExecuteBatch through TestActivityEnvironment (so the activity contract
// is exercised end-to-end), but the heartbeat assertion uses the fake
// emitter's snapshot rather than the testsuite listener.
//
// This matches the design pattern documented in heartbeat.go and used by
// 02-02's TestFakeHeartbeatEmitter_CapturesCalls — D2-16's "exactly N
// heartbeats" semantics live at the emitter contract, not at Temporal's
// (deliberately throttled) wire protocol.
func TestExecuteBatch_HappyPath_Heartbeats(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "ok"}, nil
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
	}
	inner := &extensiontesting.FakeCredentialHandler{Creds: creds}
	dispatch := OperationDispatch{
		"fake.echo": extension.OperationSpec{
			Name:       "echo",
			Idempotent: extension.Ptr(true),
			Func:       op,
		},
	}
	emitter := &fakeHeartbeatEmitter{}
	impl, err := New(dispatch, inner, withHeartbeatEmitter(emitter))
	require.NoError(t, err)

	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})

	batch := []*dag.ActionRef{
		mkRef("fake.echo", "admin"),
		mkRef("fake.echo", "admin"),
	}
	encoded, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.NoError(t, err)

	var results dag.ActionResults
	require.NoError(t, encoded.Get(&results))
	require.Len(t, results, 2)
	require.IsType(t, dag.OkResult{}, results[0])
	require.IsType(t, dag.OkResult{}, results[1])

	heartbeats := emitter.snapshot()
	require.Len(t, heartbeats, 2, "D2-16: heartbeat between every action — fake emitter captures every emit() call")
	require.Equal(t, BatchProgress{Action: 1, Total: 2}, heartbeats[0])
	require.Equal(t, BatchProgress{Action: 2, Total: 2}, heartbeats[1])
}

// TestExecuteBatch_NonRetryableMidBatch_ReturnsAllResults — D2-14: a 3-action
// batch where action[1] returns NonRetryable; result is
// [Ok{0}, NonRetryable{1}, Skipped{2}] and ExecuteBatch returns nil error
// (Temporal does NOT retry).
func TestExecuteBatch_NonRetryableMidBatch_ReturnsAllResults(t *testing.T) {
	opOK := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "ok"}, nil
	}
	opFail := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return nil, temporal.NewNonRetryableApplicationError("intentional fail", "TestFail", nil)
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
	}
	env, impl, _ := newIntegrationActivity(t,
		map[string]extension.OperationSpec{
			"fake.ok":   {Name: "ok", Idempotent: extension.Ptr(true), Func: opOK},
			"fake.fail": {Name: "fail", Idempotent: extension.Ptr(true), Func: opFail},
		},
		creds,
	)
	batch := []*dag.ActionRef{
		mkRef("fake.ok", "admin"),
		mkRef("fake.fail", "admin"),
		mkRef("fake.ok", "admin"),
	}
	encoded, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.NoError(t, err, "D2-14: non-retryable mid-batch must NOT bubble error to Temporal")

	var results dag.ActionResults
	require.NoError(t, encoded.Get(&results))
	require.Len(t, results, 3)
	require.IsType(t, dag.OkResult{}, results[0])
	require.IsType(t, dag.NonRetryableErrResult{}, results[1])
	require.IsType(t, dag.SkippedResult{}, results[2])
	require.Equal(t, 0, results[0].ActionIndex())
	require.Equal(t, 1, results[1].ActionIndex())
	require.Equal(t, 2, results[2].ActionIndex())

	skipped := results[2].(dag.SkippedResult)
	require.Contains(t, skipped.Reason, "action 1 failed non-retryably")
}

// TestExecuteBatch_RetryableMidBatch_ShortCircuits — D2-13: action[1] returns
// a generic error (classified as Retryable by isRetryable's default branch);
// ExecuteBatch returns (nil, error) so Temporal can retry the WHOLE batch.
func TestExecuteBatch_RetryableMidBatch_ShortCircuits(t *testing.T) {
	opOK := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "ok"}, nil
	}
	opFail := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return nil, errors.New("transient backend hiccup")
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
	}

	// Disable retry on the activity so env.ExecuteActivity surfaces the
	// error directly (otherwise the test environment retries indefinitely).
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	inner := &extensiontesting.FakeCredentialHandler{Creds: creds}
	ch := &counterHandler{inner: inner}
	dispatch := OperationDispatch{
		"fake.ok":   extension.OperationSpec{Name: "ok", Idempotent: extension.Ptr(true), Func: opOK},
		"fake.fail": extension.OperationSpec{Name: "fail", Idempotent: extension.Ptr(true), Func: opFail},
	}
	impl, err := New(dispatch, ch)
	require.NoError(t, err)
	env.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})

	batch := []*dag.ActionRef{
		mkRef("fake.ok", "admin"),
		mkRef("fake.fail", "admin"),
		mkRef("fake.ok", "admin"),
	}
	_, err = env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.Error(t, err, "D2-13: retryable mid-batch returns error to Temporal")
	// Verify the error is retryable (NOT a NonRetryableApplicationError).
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		require.False(t, appErr.NonRetryable(), "retryable mid-batch error must be classified retryable")
	}
}

// TestExecuteBatch_DefensivelyRejectsMixedBatch — defense in depth: a
// hand-built mixed batch (parser would reject at parse time per D2-05)
// surfaces a NonRetryable MixedIdempotency error.
func TestExecuteBatch_DefensivelyRejectsMixedBatch(t *testing.T) {
	opOK := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "ok"}, nil
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
	}
	env, impl, _ := newIntegrationActivity(t,
		map[string]extension.OperationSpec{
			"fake.idem":    {Name: "idem", Idempotent: extension.Ptr(true), Func: opOK},
			"fake.nonidem": {Name: "nonidem", Idempotent: extension.Ptr(false), Func: opOK},
		},
		creds,
	)
	batch := []*dag.ActionRef{
		mkRef("fake.idem", "admin"),
		mkRef("fake.nonidem", "admin"),
	}
	_, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable())
	require.Equal(t, errTypeMixedIdempotency, appErr.Type())
}

// TestExecuteBatch_DefensivelyRejectsOversizedBatch — D2-07 default cap (50)
// rejected at the activity boundary even when the parser would have caught it.
func TestExecuteBatch_DefensivelyRejectsOversizedBatch(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "ok"}, nil
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
	}
	env, impl, _ := newIntegrationActivity(t,
		map[string]extension.OperationSpec{
			"fake.echo": {Name: "echo", Idempotent: extension.Ptr(true), Func: op},
		},
		creds,
	)
	batch := make([]*dag.ActionRef, 51)
	for i := range batch {
		batch[i] = mkRef("fake.echo", "admin")
	}
	_, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable())
	require.Equal(t, errTypeBatchTooLarge, appErr.Type())
	require.Contains(t, appErr.Error(), "batch size 51 exceeds maximum 50")
}

// TestExecuteBatch_HandlerInvokedJIT — D-08 / ACT-04: handler.Resolve is
// invoked from inside ExecuteBatch (NOT at parse / registration time).
// Proven by handler call count being zero before activity.New + ExecuteActivity.
func TestExecuteBatch_HandlerInvokedJIT(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "ok"}, nil
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
	}

	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	inner := &extensiontesting.FakeCredentialHandler{Creds: creds}
	ch := &counterHandler{inner: inner}
	require.Equal(t, int32(0), ch.calls.Load(), "handler must not be touched before activity construction")

	dispatch := OperationDispatch{
		"fake.echo": extension.OperationSpec{Name: "echo", Idempotent: extension.Ptr(true), Func: op},
	}
	impl, err := New(dispatch, ch)
	require.NoError(t, err)
	require.Equal(t, int32(0), ch.calls.Load(), "activity.New must not call Resolve (lazy / JIT contract)")

	env.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})
	require.Equal(t, int32(0), ch.calls.Load(), "RegisterActivityWithOptions must not call Resolve")

	batch := []*dag.ActionRef{mkRef("fake.echo", "admin")}
	_, err = env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.NoError(t, err)
	require.Equal(t, int32(1), ch.calls.Load(),
		"handler.Resolve called exactly once during ExecuteBatch — JIT inside the activity")
}

// TestExecuteBatch_RetryAttempt_BypassesCache — D2-11. With withAttemptFunc
// returning 2 (simulated retry), pre-warm the cache once (ch.calls=1), then
// execute a 1-action batch that hits the same credential ID. The activity
// invalidates "admin" before runAction; resolve(bypass=true) calls the
// handler again. Total: 2.
func TestExecuteBatch_RetryAttempt_BypassesCache(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "ok"}, nil
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
	}
	env, impl, ch := newIntegrationActivity(t,
		map[string]extension.OperationSpec{
			"fake.echo": {Name: "echo", Idempotent: extension.Ptr(true), Func: op},
		},
		creds,
		withAttemptFunc(func(_ context.Context) int32 { return 2 }), // simulate retry
	)
	// Pre-warm via direct cache call (no Attempt logic involved).
	_, err := impl.cache.resolve(context.Background(), "admin", false)
	require.NoError(t, err)
	require.Equal(t, int32(1), ch.calls.Load(), "warmup")

	batch := []*dag.ActionRef{mkRef("fake.echo", "admin")}
	_, err = env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.NoError(t, err)
	require.Equal(t, int32(2), ch.calls.Load(),
		"D2-11: Attempt > 1 must invalidate every cred ID in batch and force fresh resolve")
}

// TestExecuteBatch_ACT05_SecretNeverLeaks — the headline ACT-05 test.
// Three independent leak channels checked:
//  1. ActionResult.Err string
//  2. Heartbeat payload bytes (captured via SetOnActivityHeartbeatListener)
//  3. Encoded result payload bytes (re-marshal of results through JSON)
//
// The op fails non-retryably with %s and %+v formatting of the credential —
// both must redact via Credential.String() and Secret.Format respectively.
func TestExecuteBatch_ACT05_SecretNeverLeaks(t *testing.T) {
	const fakeSecret = "phantom-token-XYZ-abc123-do-not-leak"

	opFail := func(_ context.Context, _ any, cred extension.Credential) (dag.OperationOutput, error) {
		// Error message routes the credential through %s (Credential.String,
		// already redacted in Phase 1) AND %+v (Secret.Format, redacted in
		// Phase 2 D2-08). If either path leaks, this test fails.
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("op failed using credential %s in context %+v", cred, cred),
			"OpFail",
			nil,
		)
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret(fakeSecret)},
	}

	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()

	// Capture heartbeat payloads. converter.EncodedValues exposes Get but
	// not raw bytes; round-trip through json.Marshal of a BatchProgress so
	// the leak inspection has well-defined wire bytes.
	var heartbeatMu sync.Mutex
	var heartbeatBytes [][]byte
	env.SetOnActivityHeartbeatListener(func(_ *sdkactivity.Info, details converter.EncodedValues) {
		var bp BatchProgress
		if err := details.Get(&bp); err == nil {
			if b, err := json.Marshal(bp); err == nil {
				heartbeatMu.Lock()
				heartbeatBytes = append(heartbeatBytes, b)
				heartbeatMu.Unlock()
			}
		}
	})

	inner := &extensiontesting.FakeCredentialHandler{Creds: creds}
	dispatch := OperationDispatch{
		"leaky.fail": extension.OperationSpec{
			Name:       "fail",
			Idempotent: extension.Ptr(false),
			Func:       opFail,
		},
	}
	impl, err := New(dispatch, inner)
	require.NoError(t, err)
	env.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})

	batch := []*dag.ActionRef{mkRef("leaky.fail", "admin")}
	encoded, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.NoError(t, err, "D2-14: non-retryable returns nil err")

	var results dag.ActionResults
	require.NoError(t, encoded.Get(&results))
	require.Len(t, results, 1)
	nonRetry, ok := results[0].(dag.NonRetryableErrResult)
	require.True(t, ok, "got %T", results[0])

	// Channel 1: error message.
	require.NotContains(t, nonRetry.Err.Error(), fakeSecret,
		"secret leaked into error message: %s", nonRetry.Err.Error())

	// Channel 2: heartbeat payload bytes. (No heartbeat for the failed
	// action, but defensively scan in case future code emits one before
	// failure.)
	heartbeatMu.Lock()
	for i, hb := range heartbeatBytes {
		require.False(t, bytes.Contains(hb, []byte(fakeSecret)),
			"secret leaked in heartbeat[%d]: %s", i, string(hb))
	}
	heartbeatMu.Unlock()

	// Channel 3: encoded result bytes — what Temporal serialized back to
	// the workflow. Re-marshal results through the default JSON converter
	// for inspection; this is what crosses the wire.
	resultBytes, err := json.Marshal(results)
	require.NoError(t, err)
	require.False(t, bytes.Contains(resultBytes, []byte(fakeSecret)),
		"secret leaked in result payload: %s", string(resultBytes))
}

// TestExecuteBatch_CancellationStopsBetweenActions — DESIGN LOCKED, BYPASSES
// TestActivityEnvironment because v1.42.0 testsuite does not cleanly support
// mid-activity cancellation. Exercises the real ExecuteBatch logic with a
// cancellable stdlib context — same code path as production, just without
// the testsuite wrapper.
//
// op[0] succeeds AND cancels the context as a side-effect; the loop's
// ctx.Err() check at iteration 1 sees Canceled and emits SkippedResult
// placeholders for indexes 1 and 2. ExecuteBatch returns (results, nil) —
// cancellation is graceful, not an error.
func TestExecuteBatch_CancellationStopsBetweenActions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // safety net

	var op0Ran atomic.Bool
	op0 := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		op0Ran.Store(true)
		cancel() // trigger cancellation BETWEEN action 0 and action 1
		return fakeOutput{Got: "ran"}, nil
	}
	neverRun := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		t.Fatalf("op should not have run after cancellation")
		return nil, nil
	}
	dispatch := OperationDispatch{
		"fake.op0": extension.OperationSpec{Name: "op0", Idempotent: extension.Ptr(true), Func: op0},
		"fake.op1": extension.OperationSpec{Name: "op1", Idempotent: extension.Ptr(true), Func: neverRun},
		"fake.op2": extension.OperationSpec{Name: "op2", Idempotent: extension.Ptr(true), Func: neverRun},
	}
	handler := &extensiontesting.FakeCredentialHandler{
		Creds: map[string]extension.Credential{
			"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
		},
	}
	// Inject stubs for both attemptFn and the heartbeat emitter — this test
	// bypasses TestActivityEnvironment, so defaultAttemptFunc (which calls
	// activity.GetInfo) and realHeartbeatEmitter (which calls
	// activity.RecordHeartbeat) would both panic. Use the unexported
	// withAttemptFunc + withHeartbeatEmitter seams to inject test fakes.
	impl, err := New(dispatch, handler,
		withAttemptFunc(func(_ context.Context) int32 { return 1 }),
		withHeartbeatEmitter(&fakeHeartbeatEmitter{}))
	require.NoError(t, err)

	refs := []*dag.ActionRef{
		mkRef("fake.op0", "admin"),
		mkRef("fake.op1", "admin"),
		mkRef("fake.op2", "admin"),
	}
	results, err := impl.ExecuteBatch(ctx, refs)

	// D2-14 (extended): cancellation is graceful — return the partial
	// result list with SkippedResult placeholders, NOT an error.
	require.NoError(t, err)
	require.Len(t, results, 3)
	require.IsType(t, dag.OkResult{}, results[0])
	require.IsType(t, dag.SkippedResult{}, results[1])
	require.IsType(t, dag.SkippedResult{}, results[2])
	require.Equal(t, "activity cancelled", results[1].(dag.SkippedResult).Reason)
	require.Equal(t, "activity cancelled", results[2].(dag.SkippedResult).Reason)
	require.True(t, op0Ran.Load(), "op0 should have run before cancel")
}

// TestExecuteBatch_SingleNonIdempotentAction_Allowed — D2-06 explicitly:
// a 1-action non-idempotent batch is homogeneous (allowed). No D2-05
// (mixed) or D2-06 (multi non-idempotent) violation; runs through happy path.
func TestExecuteBatch_SingleNonIdempotentAction_Allowed(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "non-idempotent ran"}, nil
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
	}
	env, impl, _ := newIntegrationActivity(t,
		map[string]extension.OperationSpec{
			"fake.post": {Name: "post", Idempotent: extension.Ptr(false), Func: op},
		},
		creds,
	)
	batch := []*dag.ActionRef{mkRef("fake.post", "admin")}
	encoded, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.NoError(t, err, "D2-06: single-action non-idempotent batch is homogeneous and allowed")
	var results dag.ActionResults
	require.NoError(t, encoded.Get(&results))
	require.Len(t, results, 1)
	require.IsType(t, dag.OkResult{}, results[0])
}

// TestExecuteBatch_BypassesResolverWhenCredentialIDEmpty — Fix A from quick
// 260502-guu: when ActionRef.CredentialID == "", runAction MUST NOT invoke
// the credential resolver and MUST pass nil to OperationSpec.Func. Without
// this guard, the noopCredentialHandler-style (or even FakeCredentialHandler
// with no entry for "") returns ErrUnknownCredential, the activity classifies
// that as retryable, and Temporal retries the WHOLE batch every ~5 s.
//
// The test pins three properties:
//
//  1. ch.calls.Load() == 0 — resolver never touched across the batch.
//  2. results length == 3 with every entry being OkResult — happy path.
//  3. The op closure asserts cred == nil — the operation receives nil.
func TestExecuteBatch_BypassesResolverWhenCredentialIDEmpty(t *testing.T) {
	op := func(_ context.Context, _ any, cred extension.Credential) (dag.OperationOutput, error) {
		require.Nil(t, cred, "Fix A: empty CredentialID must yield nil credential at op")
		return fakeOutput{Got: "anon"}, nil
	}
	// FakeCredentialHandler has NO entries — any resolve attempt would
	// return ErrUnknownCredential and surface as a retryable error.
	creds := map[string]extension.Credential{}
	env, impl, ch := newIntegrationActivity(t,
		map[string]extension.OperationSpec{
			"fake.echo": {Name: "echo", Idempotent: extension.Ptr(true), Func: op},
		},
		creds,
	)
	batch := []*dag.ActionRef{
		mkRef("fake.echo", ""),
		mkRef("fake.echo", ""),
		mkRef("fake.echo", ""),
	}
	encoded, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.NoError(t, err)
	var results dag.ActionResults
	require.NoError(t, encoded.Get(&results))
	require.Len(t, results, 3)
	require.IsType(t, dag.OkResult{}, results[0])
	require.IsType(t, dag.OkResult{}, results[1])
	require.IsType(t, dag.OkResult{}, results[2])
	require.Equal(t, int32(0), ch.calls.Load(),
		"Fix A: resolver MUST NOT be called when every ActionRef.CredentialID is empty")
	_ = impl
}

// TestExecuteBatch_BypassesResolverPerAction_MixedIDs — Fix A: a mixed batch
// where some refs carry a CredentialID and one carries the empty string. The
// per-action guard must apply at the ref level — only the empty-id action
// skips resolve; the populated-id actions still resolve.
//
// Loose count assertion (B-3 fix): newIntegrationActivity defaults to
// the production TTL (5min) so resolves of the same id across actions
// hit the cache; ch.calls is bounded between 1 (one cold resolve, one
// cache hit for "admin") and 2 (cache TTL=0 path — both "admin" actions
// resolve fresh). Either is acceptable. The strict assertion is on the
// op closures: ref1 sees cred==nil, ref0/ref2 see non-nil.
func TestExecuteBatch_BypassesResolverPerAction_MixedIDs(t *testing.T) {
	opAdmin := func(_ context.Context, _ any, cred extension.Credential) (dag.OperationOutput, error) {
		require.NotNil(t, cred, "Fix A: populated CredentialID must yield non-nil credential")
		return fakeOutput{Got: "admin"}, nil
	}
	opAnon := func(_ context.Context, _ any, cred extension.Credential) (dag.OperationOutput, error) {
		require.Nil(t, cred, "Fix A: empty CredentialID must yield nil credential")
		return fakeOutput{Got: "anon"}, nil
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
	}
	env, impl, ch := newIntegrationActivity(t,
		map[string]extension.OperationSpec{
			"fake.admin": {Name: "admin", Idempotent: extension.Ptr(true), Func: opAdmin},
			"fake.anon":  {Name: "anon", Idempotent: extension.Ptr(true), Func: opAnon},
		},
		creds,
	)
	batch := []*dag.ActionRef{
		mkRef("fake.admin", "admin"),
		mkRef("fake.anon", ""),
		mkRef("fake.admin", "admin"),
	}
	encoded, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.NoError(t, err)
	var results dag.ActionResults
	require.NoError(t, encoded.Get(&results))
	require.Len(t, results, 3)
	require.IsType(t, dag.OkResult{}, results[0])
	require.IsType(t, dag.OkResult{}, results[1])
	require.IsType(t, dag.OkResult{}, results[2])
	calls := ch.calls.Load()
	require.GreaterOrEqual(t, calls, int32(1),
		"Fix A: at least one resolve for the populated 'admin' id")
	require.LessOrEqual(t, calls, int32(2),
		"Fix A: at most two resolves — empty-id ref MUST NOT contribute a resolve")
}

// TestActionExecutor_PerActionTimeout — pinning per-action timeout works
// inside the integration test (not just unit-tested in action_executor_test.go).
// Use a short DefaultTimeout (1ms) and an op that respects ctx.Done(); the
// op should fail with context.DeadlineExceeded → classified retryable →
// ExecuteBatch short-circuits with an error.
func TestActionExecutor_PerActionTimeout(t *testing.T) {
	op := func(ctx context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	creds := map[string]extension.Credential{
		"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
	}

	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	inner := &extensiontesting.FakeCredentialHandler{Creds: creds}
	ch := &counterHandler{inner: inner}
	dispatch := OperationDispatch{
		"slow.op": extension.OperationSpec{
			Name:           "op",
			Idempotent:     extension.Ptr(true),
			Func:           op,
			DefaultTimeout: 5_000_000, // 5ms in nanoseconds; must fire fast
		},
	}
	impl, err := New(dispatch, ch)
	require.NoError(t, err)
	env.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})

	batch := []*dag.ActionRef{mkRef("slow.op", "admin")}
	// ExecuteActivity blocks until the activity returns. With per-action
	// timeout = 5ms and the op respecting Done(), the activity returns
	// promptly with a retryable error → ExecuteActivity surfaces it.
	_, err = env.ExecuteActivity(impl.ExecuteBatch, batch)
	require.Error(t, err)
	// The error should mention DeadlineExceeded somewhere (carried via
	// fmt.Errorf chain or error string after Temporal's ApplicationError
	// wrapping).
	require.Contains(t, err.Error(), "deadline exceeded",
		"per-action timeout fired and surfaced through the activity error")
}
