package activity

import (
	"context"

	"go.temporal.io/sdk/activity"
)

// attemptFunc returns the current activity attempt number. The default reads
// from activity.GetInfo(ctx).Attempt, but tests can swap a stub via the
// (unexported) withAttemptFunc option to simulate retries without driving a
// full TestWorkflowEnvironment + RetryPolicy setup.
//
// D2-11: when Attempt > 1, the activity invalidates cached credentials for
// every cred ID in the current batch and forces a fresh Resolve call. Testing
// this path without the stub is impractical: TestActivityEnvironment.ExecuteActivity
// hardcodes Attempt=1 and exposes no SetAttempt method (verified at
// sdk-go/v1.42.0/internal/internal_workflow_testsuite.go:735). The seam below
// is the only way to assert "Attempt > 1 invalidates cache" without the full
// workflow harness.
type attemptFunc func(ctx context.Context) int32

// defaultAttemptFunc reads the attempt number from the SDK's activity info.
// Used in production; tests that need to simulate retry attempts construct an
// Activity with a stub via withAttemptFunc.
func defaultAttemptFunc(ctx context.Context) int32 {
	return activity.GetInfo(ctx).Attempt
}
