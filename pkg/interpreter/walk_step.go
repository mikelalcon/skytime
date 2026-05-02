package interpreter

import (
	"fmt"
	"time"

	"go.starlark.net/starlark"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// walkStep dispatches a Step's actions to the ExecuteBatch activity.
// Activity options follow D3-19 task-queue hierarchy: step > flow > worker.
// Retry policy is forwarded from Step.Retry (D2-13/D2-14 semantics live
// in pkg/activity/execute_batch.go; this function only wires options).
//
// Quick 260502-guu Fix B: emits step_dispatch + step_complete via
// workflow.GetLogger(ctx). Uses the named-return + defer pattern so
// step_complete fires on every return path including errors — without
// it the renderer never sees ✗ markers for failed steps.
func (i *interpreter) walkStep(ctx workflow.Context, step *dag.Step) (err error) {
	logger := workflow.GetLogger(ctx)
	start := workflow.Now(ctx)
	label := stepActionLabel(step)
	path := i.currentPath()
	idx, total := i.stepIdx, i.stepTot

	logger.Info("skytime",
		"event", "step_dispatch",
		"kind", "step",
		"label", label,
		"idx", idx, "total", total, "path", path,
	)
	defer func() {
		status := "ok"
		summary := ""
		if err != nil {
			status = "err"
			summary = err.Error()
		}
		logger.Info("skytime",
			"event", "step_complete",
			"kind", "step",
			"status", status,
			"duration_ms", workflow.Now(ctx).Sub(start).Milliseconds(),
			"idx", idx, "total", total, "path", path,
			"summary", summary,
		)
	}()

	actx := workflow.WithActivityOptions(ctx, i.activityOptionsForStep(step))
	// Use dag.ActionResults (typed slice) so the sealed-sum entries decode
	// via its UnmarshalJSON discriminator. Decoding into []dag.ActionResult
	// would fail because ActionResult is an interface; the typed slice
	// dispatches per-entry.
	var results dag.ActionResults
	if err = workflow.ExecuteActivity(actx, "ExecuteBatch", step.Actions).Get(ctx, &results); err != nil {
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

// stepActionLabel computes a Bazel-style summary of the step's actions for
// the dispatch event. Single-action steps render as `<kind>(<short-summary>)`
// where short-summary tries to surface the most identifying kwarg (path/url);
// multi-action steps render as `<N> actions`.
func stepActionLabel(step *dag.Step) string {
	if len(step.Actions) == 0 {
		return "(empty)"
	}
	if len(step.Actions) > 1 {
		return fmt.Sprintf("%d actions", len(step.Actions))
	}
	a := step.Actions[0]
	return fmt.Sprintf("%s(%s)", a.Kind_, actionShortSummary(a))
}

// actionShortSummary tries to surface the path or url kwarg from an
// ActionRef's Kwargs Dict. The result is a best-effort label for the
// dispatch line — falls back to "..." when nothing useful is found.
func actionShortSummary(a *dag.ActionRef) string {
	if a == nil || a.Kwargs == nil {
		return "..."
	}
	for _, key := range []string{"path", "url"} {
		v, _, _ := a.Kwargs.Get(starlark.String(key))
		if v != nil {
			return v.String()
		}
	}
	return "..."
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
