package interpreter

import (
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// walkStep dispatches a Step's actions to the ExecuteBatch activity.
// Activity options follow D3-19 task-queue hierarchy: step > flow > worker.
// Retry policy is forwarded from Step.Retry (D2-13/D2-14 semantics live
// in pkg/activity/execute_batch.go; this function only wires options).
func (i *interpreter) walkStep(ctx workflow.Context, step *dag.Step) error {
	actx := workflow.WithActivityOptions(ctx, i.activityOptionsForStep(step))
	// Use dag.ActionResults (typed slice) so the sealed-sum entries decode
	// via its UnmarshalJSON discriminator. Decoding into []dag.ActionResult
	// would fail because ActionResult is an interface; the typed slice
	// dispatches per-entry.
	var results dag.ActionResults
	if err := workflow.ExecuteActivity(actx, "ExecuteBatch", step.Actions).Get(ctx, &results); err != nil {
		return err
	}
	// Plan 03-03 v1 simplification: per-action results are observable in
	// history, but this walker does NOT thread them into state. Future
	// plans can add output_alias-style aggregation if needed. For v1 the
	// workflow semantics are: a Step succeeds → continue; an action that
	// errored bubbles via the activity error path (D2-13 retryable
	// short-circuit) or returns NonRetryableErrResult inside results
	// (D2-14: walker continues; the activity returned nil error).
	_ = results
	return nil
}

// activityOptionsForStep computes the workflow.ActivityOptions for one Step.
// D3-19 task-queue hierarchy: step > flow > worker default.
func (i *interpreter) activityOptionsForStep(step *dag.Step) workflow.ActivityOptions {
	opts := workflow.ActivityOptions{
		StartToCloseTimeout: computeBatchTimeout(step),
		HeartbeatTimeout:    60 * time.Second,
	}
	switch {
	case step.TaskQueue != "":
		opts.TaskQueue = step.TaskQueue
	case i.flow.TaskQueue != "":
		opts.TaskQueue = i.flow.TaskQueue
		// else: leave empty → Temporal uses the worker's default task queue
	}
	if step.Retry != nil {
		opts.RetryPolicy = toTemporalRetryPolicy(step.Retry)
	}
	return opts
}
