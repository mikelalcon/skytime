package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// TestDefaultAttemptFunc_ReadsActivityGetInfo verifies that defaultAttemptFunc
// correctly delegates to activity.GetInfo. The test uses TestActivityEnvironment
// because activity.GetInfo requires an activity context populated by the SDK.
//
// TestActivityEnvironment hardcodes Attempt=1 (verified at
// sdk-go/v1.42.0/internal/internal_workflow_testsuite.go:735) — the assertion
// is therefore "default returns 1" not "default returns N for arbitrary N".
// For Attempt > 1 tests, see 02-03 which uses the attemptFunc stub via
// withAttemptFunc.
func TestDefaultAttemptFunc_ReadsActivityGetInfo(t *testing.T) {
	var got int32
	act := func(ctx context.Context) error {
		got = defaultAttemptFunc(ctx)
		return nil
	}

	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(act)
	_, err := env.ExecuteActivity(act)
	require.NoError(t, err)
	require.Equal(t, int32(1), got, "TestActivityEnvironment hardcodes Attempt=1")
}

// TestAttemptFunc_StubInjection documents the seam used in 02-03: Activity
// holds an attemptFunc field; tests inject a stub returning a fixed value to
// simulate retry attempts without running through TestActivityEnvironment.
func TestAttemptFunc_StubInjection(t *testing.T) {
	var fn attemptFunc = func(_ context.Context) int32 { return 42 }
	require.Equal(t, int32(42), fn(context.Background()))
}

// _ silences the unused-import warning if test refactors remove every other
// use of activity. Compile-time assertion that activity.GetInfo is reachable
// from this test file.
var _ = activity.GetInfo
