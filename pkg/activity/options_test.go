package activity

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
	extensiontesting "github.com/mikelalcon/skytime/pkg/extension/testing"
)

// minimalDispatch builds a one-entry OperationDispatch suitable for option
// tests (Func and KwargsType nil — these tests don't invoke ops).
func minimalDispatch() OperationDispatch {
	return OperationDispatch{
		"fake.echo": extension.OperationSpec{
			Name:       "echo",
			Idempotent: extension.Ptr(true),
		},
	}
}

// minimalHandler returns an empty FakeCredentialHandler — enough for option
// tests; ID lookups are not performed here.
func minimalHandler() *extensiontesting.FakeCredentialHandler {
	return &extensiontesting.FakeCredentialHandler{
		Creds: map[string]extension.Credential{},
	}
}

// TestNew_Defaults: New(dispatch, handler) yields the documented defaults —
// 5min cache TTL (D2-10), 50 max block size (D2-07), defaultAttemptFunc as
// the attemptFn, realHeartbeatEmitter as the emitter, and a non-nil cache
// wrapping the supplied handler.
func TestNew_Defaults(t *testing.T) {
	a, err := New(minimalDispatch(), minimalHandler())
	require.NoError(t, err)
	require.NotNil(t, a)
	require.Equal(t, 5*time.Minute, a.cacheTTL)
	require.Equal(t, 50, a.maxBlockSize)
	require.NotNil(t, a.attemptFn)
	require.NotNil(t, a.emitter)
	require.NotNil(t, a.cache)

	// Default attemptFn must be defaultAttemptFunc (identity check via
	// runtime.FuncForPC name match — function values aren't comparable
	// directly in Go).
	gotName := runtime.FuncForPC(reflect.ValueOf(a.attemptFn).Pointer()).Name()
	require.Contains(t, gotName, "defaultAttemptFunc",
		"default attemptFn should be defaultAttemptFunc, got %q", gotName)

	// Default emitter is realHeartbeatEmitter{}.
	_, ok := a.emitter.(realHeartbeatEmitter)
	require.True(t, ok, "default emitter should be realHeartbeatEmitter, got %T", a.emitter)

	// Cache wraps the supplied handler with the default TTL.
	require.Equal(t, 5*time.Minute, a.cache.ttl)
}

// TestWithCredentialCacheTTL: option is honored by both Activity.cacheTTL
// AND the cache it constructs (the cache must be built AFTER options apply).
func TestWithCredentialCacheTTL(t *testing.T) {
	a, err := New(minimalDispatch(), minimalHandler(), WithCredentialCacheTTL(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 2*time.Hour, a.cacheTTL)
	require.Equal(t, 2*time.Hour, a.cache.ttl,
		"cache must be constructed AFTER options so WithCredentialCacheTTL takes effect on the cache")
}

// TestWithCredentialCacheTTL_Zero: 0 is "never cache" — explicitly allowed.
func TestWithCredentialCacheTTL_Zero(t *testing.T) {
	a, err := New(minimalDispatch(), minimalHandler(), WithCredentialCacheTTL(0))
	require.NoError(t, err)
	require.Equal(t, time.Duration(0), a.cacheTTL)
	require.Equal(t, time.Duration(0), a.cache.ttl)
}

// TestWithCredentialCacheTTL_NegativeRejected: negative TTL is a config bug.
func TestWithCredentialCacheTTL_NegativeRejected(t *testing.T) {
	_, err := New(minimalDispatch(), minimalHandler(), WithCredentialCacheTTL(-1*time.Second))
	require.Error(t, err)
	require.Contains(t, err.Error(), "credential cache TTL")
}

// TestWithMaxBlockSize: positive int is honored.
func TestWithMaxBlockSize(t *testing.T) {
	a, err := New(minimalDispatch(), minimalHandler(), WithMaxBlockSize(7))
	require.NoError(t, err)
	require.Equal(t, 7, a.maxBlockSize)
}

// TestWithMaxBlockSize_RejectsZero: < 1 is rejected at construction (matches
// the parser's WithMaxBlockSize fast-fail behavior for symmetry).
func TestWithMaxBlockSize_RejectsZero(t *testing.T) {
	_, err := New(minimalDispatch(), minimalHandler(), WithMaxBlockSize(0))
	require.Error(t, err)
	require.Contains(t, err.Error(), "max block size")
}

// TestWithMaxBlockSize_RejectsNegative: defense in depth.
func TestWithMaxBlockSize_RejectsNegative(t *testing.T) {
	_, err := New(minimalDispatch(), minimalHandler(), WithMaxBlockSize(-5))
	require.Error(t, err)
	require.Contains(t, err.Error(), "max block size")
}

// TestWithAttemptFunc_Internal: the unexported withAttemptFunc seam swaps
// attemptFn for a test stub. Reachable only from same-package tests, which
// is the intent — production code uses defaultAttemptFunc unconditionally.
func TestWithAttemptFunc_Internal(t *testing.T) {
	stub := func(_ context.Context) int32 { return 42 }
	a, err := New(minimalDispatch(), minimalHandler(), withAttemptFunc(stub))
	require.NoError(t, err)
	require.Equal(t, int32(42), a.attemptFn(context.Background()))
}

// TestWithAttemptFunc_RejectsNil: nil attempt func is a programmer error.
func TestWithAttemptFunc_RejectsNil(t *testing.T) {
	_, err := New(minimalDispatch(), minimalHandler(), withAttemptFunc(nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "attemptFunc")
}

// TestWithHeartbeatEmitter_Internal: the unexported withHeartbeatEmitter seam
// swaps emitter for a test fake (typically *fakeHeartbeatEmitter).
func TestWithHeartbeatEmitter_Internal(t *testing.T) {
	fake := &fakeHeartbeatEmitter{}
	a, err := New(minimalDispatch(), minimalHandler(), withHeartbeatEmitter(fake))
	require.NoError(t, err)
	require.Same(t, fake, a.emitter)
}

// TestWithHeartbeatEmitter_RejectsNil: nil emitter is a programmer error.
func TestWithHeartbeatEmitter_RejectsNil(t *testing.T) {
	_, err := New(minimalDispatch(), minimalHandler(), withHeartbeatEmitter(nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "heartbeatEmitter")
}

// TestNew_NilHandler_Errors: required argument; surface fast-fail.
func TestNew_NilHandler_Errors(t *testing.T) {
	_, err := New(minimalDispatch(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "handler")
}

// TestNew_NilDispatch_Errors: required argument; surface fast-fail.
func TestNew_NilDispatch_Errors(t *testing.T) {
	_, err := New(nil, minimalHandler())
	require.Error(t, err)
	require.Contains(t, err.Error(), "dispatch")
}

// TestNew_OptionErrorIsWrapped: option failures propagate through New with
// a constructor-prefix wrap so callers can grep "activity.New" in logs.
func TestNew_OptionErrorIsWrapped(t *testing.T) {
	_, err := New(minimalDispatch(), minimalHandler(), WithMaxBlockSize(0))
	require.Error(t, err)
	require.Contains(t, err.Error(), "activity.New")
}

// TestNew_OptionsAppliedInOrder: later options override earlier ones (last
// write wins). Documents the contract for callers building option slices.
func TestNew_OptionsAppliedInOrder(t *testing.T) {
	a, err := New(minimalDispatch(), minimalHandler(),
		WithMaxBlockSize(10),
		WithMaxBlockSize(20),
	)
	require.NoError(t, err)
	require.Equal(t, 20, a.maxBlockSize)
}

// TestNew_NoExecuteBatchYet: the plan explicitly defers ExecuteBatch to
// 02-03. Use reflection to assert *Activity has no ExecuteBatch method
// yet (catches accidental scope creep).
func TestNew_NoExecuteBatchYet(t *testing.T) {
	a, err := New(minimalDispatch(), minimalHandler())
	require.NoError(t, err)
	ty := reflect.TypeOf(a)
	_, has := ty.MethodByName("ExecuteBatch")
	require.False(t, has, "ExecuteBatch lands in 02-03; this plan stops at the building blocks")
}

// Compile-time sanity: error sentinel symbols present in the package.
var _ = errors.New // silence unused-import if other tests refactor
