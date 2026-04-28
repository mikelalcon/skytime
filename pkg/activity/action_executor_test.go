package activity

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.temporal.io/sdk/temporal"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	extensiontesting "github.com/mikelalcon/skytime/pkg/extension/testing"
)

// fakeOutput implements dag.OperationOutput for use in pkg/activity tests.
// Defined here so action_executor_test and execute_batch_test share the
// same trivial Output type.
type fakeOutput struct {
	Got string
}

// IsOperationOutput satisfies dag.OperationOutput. The exported method form
// matches D2-03 / pkg/dag/output.go SEAL PROPERTY.
func (fakeOutput) IsOperationOutput() {}

// echoArgs is a kwargs struct used by the kwargs-decode test.
type echoArgs struct {
	Msg string `star:"msg,required"`
}

// newRunActionActivity constructs an Activity whose dispatch contains a
// single op named "fake.echo" with the supplied OperationFunc + timeout.
// The FakeCredentialHandler returns a BearerCredential for "admin" only.
//
// Wraps the inner FakeCredentialHandler in counterHandler so tests can
// observe Resolve call counts (driving Test 6 — RetryAttemptForcesBypass).
//
// Default attemptFn returns 1 (Attempt==1, normal path) — these unit tests
// run runAction outside TestActivityEnvironment, so defaultAttemptFunc
// (which calls activity.GetInfo) would panic. Tests that specifically
// exercise Attempt > 1 (D2-11) override via withAttemptFunc(...) in opts.
func newRunActionActivity(t *testing.T, fn extension.OperationFunc, defaultTimeout time.Duration, opts ...Option) (*Activity, *counterHandler) {
	t.Helper()
	dispatch := OperationDispatch{
		"fake.echo": extension.OperationSpec{
			Name:           "echo",
			Idempotent:     extension.Ptr(true),
			Func:           fn,
			KwargsType:     nil, // tests in this file don't decode kwargs unless overridden
			DefaultTimeout: defaultTimeout,
		},
	}
	inner := &extensiontesting.FakeCredentialHandler{
		Creds: map[string]extension.Credential{
			"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("ghp_test")},
		},
	}
	ch := &counterHandler{inner: inner}
	// Inject a default attemptFn returning 1 BEFORE caller-supplied opts so
	// callers can still override via withAttemptFunc; the options-applied-in-
	// order contract (TestNew_OptionsAppliedInOrder) means later writes win.
	allOpts := append([]Option{withAttemptFunc(func(_ context.Context) int32 { return 1 })}, opts...)
	a, err := New(dispatch, ch, allOpts...)
	require.NoError(t, err)
	return a, ch
}

// TestRunAction_HappyPath_NoTimeout — no per-action timeout; op returns
// immediately with a typed Output and nil error.
func TestRunAction_HappyPath_NoTimeout(t *testing.T) {
	op := func(_ context.Context, _ any, cred extension.Credential) (dag.OperationOutput, error) {
		require.NotNil(t, cred)
		return fakeOutput{Got: "ok"}, nil
	}
	a, ch := newRunActionActivity(t, op, 0)
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "admin"}
	out, err := a.runAction(context.Background(), 0, ref)
	require.NoError(t, err)
	require.Equal(t, fakeOutput{Got: "ok"}, out)
	require.Equal(t, int32(1), ch.calls.Load(), "cache.resolve should call handler once on first miss")
}

// TestRunAction_PerActionTimeout_DeadlineExceeded — D2-15: spec.DefaultTimeout
// is enforced via context.WithTimeout. The op respects ctx.Done() and returns
// ctx.Err(); runAction must propagate that error within wall-clock seconds.
func TestRunAction_PerActionTimeout_DeadlineExceeded(t *testing.T) {
	op := func(ctx context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	a, _ := newRunActionActivity(t, op, 5*time.Millisecond)
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "admin"}
	start := time.Now()
	_, err := a.runAction(context.Background(), 0, ref)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded), "got %v (expected wraps context.DeadlineExceeded)", err)
	require.Less(t, time.Since(start), 250*time.Millisecond,
		"per-action timeout should fire promptly (< 250ms wall-clock for 5ms timeout)")
}

// TestRunAction_PerActionTimeout_OpFinishesEarly — DefaultTimeout > 0 but the
// op returns quickly; runAction returns OK with no blocking.
func TestRunAction_PerActionTimeout_OpFinishesEarly(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "fast"}, nil
	}
	a, _ := newRunActionActivity(t, op, 1*time.Second)
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "admin"}
	out, err := a.runAction(context.Background(), 0, ref)
	require.NoError(t, err)
	require.Equal(t, fakeOutput{Got: "fast"}, out)
}

// TestRunAction_CredentialResolveFails_PassesClassifiedError — D2-12: handler
// returning ErrUnknownCredential is classified as NonRetryable.
func TestRunAction_CredentialResolveFails_PassesClassifiedError(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		t.Fatalf("op should not be invoked on resolve failure")
		return nil, nil
	}
	a, ch := newRunActionActivity(t, op, 0)
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "missing-id"}
	_, err := a.runAction(context.Background(), 0, ref)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable(), "ErrUnknownCredential must classify as NonRetryable per D2-12")
	require.Equal(t, errTypeUnknownCredential, appErr.Type())
	require.Equal(t, int32(1), ch.calls.Load())
}

// TestRunAction_CredentialResolveTransient_RetryableClassified — D2-12: any
// non-ErrUnknownCredential handler error is treated as transient (retryable).
func TestRunAction_CredentialResolveTransient_RetryableClassified(t *testing.T) {
	// Build a handler that always errors with a non-ErrUnknownCredential error.
	transientErr := errors.New("backend down")
	dispatch := OperationDispatch{
		"fake.echo": extension.OperationSpec{
			Name:       "echo",
			Idempotent: extension.Ptr(true),
			Func: func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
				t.Fatalf("op should not be invoked on resolve failure")
				return nil, nil
			},
		},
	}
	a, err := New(dispatch, &erroringHandler{err: transientErr},
		withAttemptFunc(func(_ context.Context) int32 { return 1 }))
	require.NoError(t, err)

	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "admin"}
	_, err = a.runAction(context.Background(), 0, ref)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.False(t, appErr.NonRetryable(), "non-ErrUnknownCredential resolve error must classify as Retryable per D2-12")
	require.Equal(t, errTypeCredentialResolveFailed, appErr.Type())
}

// TestRunAction_RetryAttemptForcesBypass — D2-11: when attemptFn(ctx) > 1,
// runAction must call cache.resolve with bypass=true. We verify by pre-warming
// the cache with bypass=false (counter increments to 1) then invoking runAction
// with attemptFn=2; the bypass=true path forces another handler call (counter
// increments to 2).
//
// Note: ExecuteBatch (Task 3) is responsible for invalidating cache entries
// before runAction is invoked on retry, but runAction itself must propagate
// the bypass flag — this test verifies that propagation independent of
// ExecuteBatch's invalidation step. Together they cover D2-11.
func TestRunAction_RetryAttemptForcesBypass(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return fakeOutput{Got: "ok"}, nil
	}
	a, ch := newRunActionActivity(t, op, 0,
		withAttemptFunc(func(_ context.Context) int32 { return 2 }))
	// Pre-warm the cache via the bypass=false path.
	_, err := a.cache.resolve(context.Background(), "admin", false)
	require.NoError(t, err)
	require.Equal(t, int32(1), ch.calls.Load(), "warmup")

	// runAction with Attempt=2 must drive bypass=true into cache.resolve;
	// the cache calls the handler again because bypass skips the cache read.
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "admin"}
	_, err = a.runAction(context.Background(), 0, ref)
	require.NoError(t, err)
	require.Equal(t, int32(2), ch.calls.Load(),
		"Attempt > 1 should drive bypass=true → handler called again despite warm cache")
}

// TestRunAction_UnknownOp_NonRetryable — defense in depth: runAction must
// reject unknown Kind_ even when called outside the validateBatch gate.
func TestRunAction_UnknownOp_NonRetryable(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		t.Fatalf("op should not be invoked on unknown-op path")
		return nil, nil
	}
	a, _ := newRunActionActivity(t, op, 0)
	ref := &dag.ActionRef{Kind_: "missing.op", Kwargs: starlark.NewDict(0), CredentialID: "admin"}
	_, err := a.runAction(context.Background(), 0, ref)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable())
	require.Equal(t, errTypeUnknownOperation, appErr.Type())
	require.Contains(t, appErr.Error(), `unknown operation "missing.op"`)
}

// TestRunAction_OpReturnsNonAppError_PassedThrough — runAction does NOT
// classify op errors. ExecuteBatch is responsible for inspecting whether the
// op error is a *temporal.ApplicationError (passthrough) or a generic error
// (treated retryable). This test pins the contract.
func TestRunAction_OpReturnsNonAppError_PassedThrough(t *testing.T) {
	rawErr := errors.New("op failed")
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return nil, rawErr
	}
	a, _ := newRunActionActivity(t, op, 0)
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "admin"}
	_, err := a.runAction(context.Background(), 0, ref)
	require.Error(t, err)
	require.True(t, errors.Is(err, rawErr), "op error should be returned UNWRAPPED — got %v", err)
	// Defensive: it must NOT have been wrapped as *temporal.ApplicationError.
	var appErr *temporal.ApplicationError
	require.False(t, errors.As(err, &appErr), "runAction must not classify op errors")
}

// TestRunAction_OpReturnsAppError_PassedThrough — symmetric to the above:
// when the op already returns *temporal.ApplicationError (e.g., it knows the
// failure is non-retryable), runAction must pass it through unmodified so
// ExecuteBatch's isRetryable inspection works.
func TestRunAction_OpReturnsAppError_PassedThrough(t *testing.T) {
	appErr := temporal.NewNonRetryableApplicationError("known fail", "OpFail", nil)
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		return nil, appErr
	}
	a, _ := newRunActionActivity(t, op, 0)
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "admin"}
	_, err := a.runAction(context.Background(), 0, ref)
	require.Error(t, err)
	var got *temporal.ApplicationError
	require.True(t, errors.As(err, &got))
	require.True(t, got.NonRetryable())
	require.Equal(t, "OpFail", got.Type())
}

// TestRunAction_KwargsDecode_PopulatesArgs — kwargs decode integration
// test: spec.KwargsType is non-nil, ref.Kwargs has a "msg" key, and the
// op receives the typed echoArgs struct via args.
func TestRunAction_KwargsDecode_PopulatesArgs(t *testing.T) {
	var gotArgs any
	op := func(_ context.Context, args any, _ extension.Credential) (dag.OperationOutput, error) {
		gotArgs = args
		return fakeOutput{Got: "decoded"}, nil
	}
	dispatch := OperationDispatch{
		"fake.echo": extension.OperationSpec{
			Name:       "echo",
			Idempotent: extension.Ptr(true),
			Func:       op,
			KwargsType: reflect.TypeOf(echoArgs{}),
		},
	}
	handler := &extensiontesting.FakeCredentialHandler{
		Creds: map[string]extension.Credential{
			"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
		},
	}
	a, err := New(dispatch, handler,
		withAttemptFunc(func(_ context.Context) int32 { return 1 }))
	require.NoError(t, err)

	kwargs := starlark.NewDict(1)
	require.NoError(t, kwargs.SetKey(starlark.String("msg"), starlark.String("hello")))
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: kwargs, CredentialID: "admin"}
	out, err := a.runAction(context.Background(), 0, ref)
	require.NoError(t, err)
	require.Equal(t, fakeOutput{Got: "decoded"}, out)

	got, ok := gotArgs.(echoArgs)
	require.True(t, ok, "args should decode to echoArgs, got %T", gotArgs)
	require.Equal(t, "hello", got.Msg)
}

// TestRunAction_KwargsDecode_MissingRequired_NonRetryable — kwargs decode
// failure (e.g., required field missing) is a contract bug. The activity
// surfaces it as NonRetryable so the workflow does not retry on a parser bug.
func TestRunAction_KwargsDecode_MissingRequired_NonRetryable(t *testing.T) {
	op := func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
		t.Fatalf("op should not be invoked on kwargs-decode failure")
		return nil, nil
	}
	dispatch := OperationDispatch{
		"fake.echo": extension.OperationSpec{
			Name:       "echo",
			Idempotent: extension.Ptr(true),
			Func:       op,
			KwargsType: reflect.TypeOf(echoArgs{}),
		},
	}
	handler := &extensiontesting.FakeCredentialHandler{
		Creds: map[string]extension.Credential{
			"admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("x")},
		},
	}
	a, err := New(dispatch, handler,
		withAttemptFunc(func(_ context.Context) int32 { return 1 }))
	require.NoError(t, err)

	// kwargs is empty — required "msg" missing.
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "admin"}
	_, err = a.runAction(context.Background(), 0, ref)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable(), "kwargs decode failures must be non-retryable")
	require.Equal(t, errTypeKwargsDecode, appErr.Type())
}

// Compile-time guard: action_executor.go must call extension.DecodeKwargsFromDict —
// the runtime kwargs decoder added in Plan 02-01 Task 4. The grep-friendly
// reference here is intentional documentation; the actual call lives in
// decodeActionRefKwargs.
var _ = extension.DecodeKwargsFromDict

// (silence unused-import warnings on `fmt` if any test refactor removes
// the only fmt user above.)
var _ = fmt.Sprint
