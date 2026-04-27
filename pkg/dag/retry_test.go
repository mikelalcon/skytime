package dag

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
)

// Compile-time interface assertions: visible at file scope so reviewers see
// the contract without running tests.
var (
	_ starlark.Unpacker = (*RetryPolicy)(nil)
	_ starlark.Unpacker = (*Timeout)(nil)
)

// retryDict builds a *starlark.Dict from a Go-shaped key/value pair list.
// Values that need typing are passed in their Starlark form already.
func retryDict(t *testing.T, pairs ...any) *starlark.Dict {
	t.Helper()
	require.Equal(t, 0, len(pairs)%2, "retryDict requires even arg count")
	d := starlark.NewDict(len(pairs) / 2)
	for i := 0; i < len(pairs); i += 2 {
		key := starlark.String(pairs[i].(string))
		val, ok := pairs[i+1].(starlark.Value)
		require.True(t, ok, "value at index %d must already be a starlark.Value", i+1)
		require.NoError(t, d.SetKey(key, val))
	}
	return d
}

// --- RetryPolicy.Unpack ------------------------------------------------------

func TestRetryPolicy_Unpack_FullDict(t *testing.T) {
	lst := starlark.NewList([]starlark.Value{starlark.String("FOO"), starlark.String("BAR")})
	d := retryDict(t,
		"initial_interval", starlark.String("1s"),
		"backoff_coefficient", starlark.Float(2.0),
		"max_attempts", starlark.MakeInt(5),
		"non_retryable_errors", lst,
	)

	var rp RetryPolicy
	require.NoError(t, rp.Unpack(d))

	assert.Equal(t, time.Second, rp.InitialInterval)
	assert.Equal(t, 2.0, rp.BackoffCoefficient)
	assert.Equal(t, 5, rp.MaxAttempts)
	assert.Equal(t, []string{"FOO", "BAR"}, rp.NonRetryableErrors)
}

func TestRetryPolicy_Unpack_BackoffAcceptsInt(t *testing.T) {
	d := retryDict(t, "backoff_coefficient", starlark.MakeInt(2))
	var rp RetryPolicy
	require.NoError(t, rp.Unpack(d))
	assert.Equal(t, 2.0, rp.BackoffCoefficient, "Starlark Int 2 → 2.0 float coefficient")
}

func TestRetryPolicy_Unpack_EmptyDictKeepsZeroValues(t *testing.T) {
	d := starlark.NewDict(0)
	var rp RetryPolicy
	require.NoError(t, rp.Unpack(d))
	assert.Zero(t, rp.InitialInterval)
	assert.Zero(t, rp.BackoffCoefficient)
	assert.Zero(t, rp.MaxAttempts)
	assert.Empty(t, rp.NonRetryableErrors)
}

func TestRetryPolicy_Unpack_RejectsNonDict(t *testing.T) {
	var rp RetryPolicy
	err := rp.Unpack(starlark.String("not a dict"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a dict")
}

func TestRetryPolicy_Unpack_RejectsUnknownKey(t *testing.T) {
	d := retryDict(t, "max_attempt", starlark.MakeInt(5))
	var rp RetryPolicy
	err := rp.Unpack(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown key")
	assert.Contains(t, err.Error(), "max_attempt")
}

func TestRetryPolicy_Unpack_RejectsWrongType(t *testing.T) {
	d := retryDict(t, "max_attempts", starlark.String("five"))
	var rp RetryPolicy
	err := rp.Unpack(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_attempts")
	assert.Contains(t, err.Error(), "int")
}

func TestRetryPolicy_Unpack_RejectsBadDuration(t *testing.T) {
	d := retryDict(t, "initial_interval", starlark.String("not-a-duration"))
	var rp RetryPolicy
	err := rp.Unpack(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initial_interval")
}

func TestRetryPolicy_Unpack_RejectsNonStringNonRetryableEntry(t *testing.T) {
	lst := starlark.NewList([]starlark.Value{starlark.MakeInt(123)})
	d := retryDict(t, "non_retryable_errors", lst)
	var rp RetryPolicy
	err := rp.Unpack(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non_retryable_errors")
}

// --- Timeout.Unpack ----------------------------------------------------------

func TestTimeout_Unpack_FullDict(t *testing.T) {
	d := retryDict(t,
		"start_to_close", starlark.String("30s"),
		"schedule_to_start", starlark.String("5s"),
	)
	var tm Timeout
	require.NoError(t, tm.Unpack(d))
	assert.Equal(t, 30*time.Second, tm.StartToClose)
	assert.Equal(t, 5*time.Second, tm.ScheduleToStart)
}

func TestTimeout_Unpack_EmptyDictKeepsZeroValues(t *testing.T) {
	d := starlark.NewDict(0)
	var tm Timeout
	require.NoError(t, tm.Unpack(d))
	assert.Zero(t, tm.StartToClose)
	assert.Zero(t, tm.ScheduleToStart)
}

func TestTimeout_Unpack_RejectsNonDict(t *testing.T) {
	var tm Timeout
	err := tm.Unpack(starlark.String("nope"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a dict")
}

func TestTimeout_Unpack_RejectsUnknownKey(t *testing.T) {
	d := retryDict(t, "start_to_close_typo", starlark.String("30s"))
	var tm Timeout
	err := tm.Unpack(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown key")
}

func TestTimeout_Unpack_RejectsBadDuration(t *testing.T) {
	d := retryDict(t, "start_to_close", starlark.String("never"))
	var tm Timeout
	err := tm.Unpack(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_to_close")
}

func TestTimeout_Unpack_RejectsNonStringValue(t *testing.T) {
	d := retryDict(t, "start_to_close", starlark.MakeInt(30))
	var tm Timeout
	err := tm.Unpack(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_to_close")
	assert.Contains(t, err.Error(), "string")
}
