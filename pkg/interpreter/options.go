package interpreter

import (
	"time"

	"go.temporal.io/sdk/temporal"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// defaultActionTimeout is D2-15: per-action default when not specified.
const defaultActionTimeout = 60 * time.Second

// batchTimeoutHeadroom is D2-15: padding above the sum of per-action
// timeouts to account for activity start/end latency, heartbeats, etc.
const batchTimeoutHeadroom = 30 * time.Second

// toTemporalRetryPolicy converts dag.RetryPolicy to *temporal.RetryPolicy.
// Returns nil when src is nil (Temporal default applies).
func toTemporalRetryPolicy(src *dag.RetryPolicy) *temporal.RetryPolicy {
	if src == nil {
		return nil
	}
	return &temporal.RetryPolicy{
		InitialInterval:        src.InitialInterval,
		BackoffCoefficient:     src.BackoffCoefficient,
		MaximumAttempts:        int32(src.MaxAttempts),
		NonRetryableErrorTypes: src.NonRetryableErrors,
	}
}

// computeBatchTimeout returns the StartToCloseTimeout for a batch (D2-15):
// sum of per-action timeouts (default 60s each) plus 30s headroom.
//
// The Phase 1 dag.Step.Timeout field captures Step-level overrides; per-action
// timeouts come from extension OperationSpec.DefaultTimeout. For v1 we use
// the Step.Timeout.StartToClose if set, else default 60s × len(Actions) + 30s.
func computeBatchTimeout(step *dag.Step) time.Duration {
	if step.Timeout != nil && step.Timeout.StartToClose > 0 {
		return step.Timeout.StartToClose + batchTimeoutHeadroom
	}
	return time.Duration(len(step.Actions))*defaultActionTimeout + batchTimeoutHeadroom
}
