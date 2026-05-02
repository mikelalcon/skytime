package interpreter

import (
	"fmt"
	"reflect"
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
	// Use dag.ActionResults (typed slice) so the sealed-sum entries decode
	// via its UnmarshalJSON discriminator. Decoding into []dag.ActionResult
	// would fail because ActionResult is an interface; the typed slice
	// dispatches per-entry. Declared at function scope so the defer below
	// can read post-ExecuteActivity values via closure capture for the
	// success-path summary.
	var results dag.ActionResults
	defer func() {
		status := "ok"
		summary := ""
		if err != nil {
			status = "err"
			summary = err.Error()
		} else {
			// Quick 260502-onc Fix B-1: surface a per-step summary
			// derived from the activity results (e.g. "status=200" for
			// HTTP-shaped Output, "N ok" for multi-action batches).
			summary = extractStatusSummary(results)
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
	if err = workflow.ExecuteActivity(actx, "ExecuteBatch", step.Actions).Get(ctx, &results); err != nil {
		return err
	}
	// Quick 260502-onc Fix B-2: convert the activity layer's D2-14
	// (results, nil) "soft failure" into a workflow-level failure so the
	// renderer prints flow_failed. Pre-fix, walkStep ignored results
	// (`_ = results`) and the workflow walked past NonRetryableErrResult
	// silently. With Fix A wrapping non-2xx HTTP as NonRetryable, the
	// renderer now sees a real failure event.
	if perActionErr := extractFirstNonRetryable(results); perActionErr != nil {
		err = perActionErr
		return err
	}
	return nil
}

// extractStatusSummary computes the summary attr for a successful step.
// Single-action steps with an Output struct exposing an `int` field
// named "Status" render as "status=N" (the HTTP extension's HTTPResponse
// shape). Multi-action steps render as "<N> ok" (block batches don't
// have a single meaningful status). Steps whose Output type does not
// have an int Status field render as "" (empty — best-effort, no
// annotation).
//
// Reflection-based to preserve the firewall: pkg/interpreter cannot
// import pkg/extension/builtin/http (interpreter is a foundation
// package, builtin extensions are leaves). reflect.FieldByName is
// O(struct-fields) per call — negligible at single-step granularity.
func extractStatusSummary(results dag.ActionResults) string {
	if len(results) == 0 {
		return ""
	}
	if len(results) > 1 {
		return fmt.Sprintf("%d ok", len(results))
	}
	ok, isOK := results[0].(dag.OkResult)
	if !isOK || ok.Output == nil {
		return ""
	}
	v := reflect.ValueOf(ok.Output)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	f := v.FieldByName("Status")
	if !f.IsValid() || !f.CanInterface() || f.Kind() != reflect.Int {
		return ""
	}
	return fmt.Sprintf("status=%d", f.Int())
}

// extractFirstNonRetryable scans the per-action result slice and returns
// the first NonRetryableErrResult's Err, or nil if none is present. Used
// by walkStep to convert the activity layer's D2-14 (results, nil) "soft
// failure" into a workflow-level failure so the renderer surfaces it.
func extractFirstNonRetryable(results dag.ActionResults) error {
	for _, r := range results {
		if nr, ok := r.(dag.NonRetryableErrResult); ok {
			return nr.Err
		}
	}
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
