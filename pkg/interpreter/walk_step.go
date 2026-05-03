package interpreter

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"go.starlark.net/starlark"
	"go.temporal.io/sdk/temporal"
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
	// deferEmit gates the success-path step_complete emission. The
	// empty-batch short-circuit (D4.1-09) emits step_complete inline
	// with summary="empty batch" and sets deferEmit=false to avoid
	// duplicate emission. Every other return path keeps deferEmit=true
	// so the existing post-ExecuteActivity summary logic fires.
	deferEmit := true
	// Use dag.ActionResults (typed slice) so the sealed-sum entries decode
	// via its UnmarshalJSON discriminator. Decoding into []dag.ActionResult
	// would fail because ActionResult is an interface; the typed slice
	// dispatches per-entry. Declared at function scope so the defer below
	// can read post-ExecuteActivity values via closure capture for the
	// success-path summary.
	var results dag.ActionResults
	defer func() {
		if !deferEmit {
			return
		}
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

	// Plan 04.1-05b D4.1-06/07: dispatch ActionFn / BlockFn before the
	// activity call. Static steps (Actions populated, no ActionFn /
	// BlockFn) flow through unchanged.
	actions, err := i.buildStepActions(ctx, step)
	if err != nil {
		return err
	}

	// D4.1-09 empty-batch short-circuit: when block_fn returns []
	// emit step_complete with summary="empty batch" and skip the activity
	// dispatch entirely. Cheaper than dispatching a no-op activity;
	// observable to the renderer.
	if len(actions) == 0 && step.BlockFn != nil {
		logger.Info("skytime",
			"event", "step_complete",
			"kind", "step",
			"status", "ok",
			"duration_ms", workflow.Now(ctx).Sub(start).Milliseconds(),
			"idx", idx, "total", total, "path", path,
			"summary", "empty batch",
		)
		deferEmit = false
		return nil
	}

	// W8: lambda-returned ActionRefs do NOT pass through the parser's
	// freeze pass. Freeze each returned ref explicitly so its Kwargs
	// *Dict becomes immutable for the rest of the workflow's lifetime.
	// Replays then see byte-identical Items() iteration. Static
	// (parser-built) actions are already frozen — Freeze() is idempotent
	// so re-freezing is a no-op.
	for _, ref := range actions {
		ref.Freeze()
	}

	// D4.1-14: resolve lambda-valued kwargs on each action BEFORE
	// ExecuteActivity. Plan 04.1-05a's resolveKwargs walks ref.Kwargs in
	// deterministic order (insertion order via *starlark.Dict.Items),
	// evaluates each *StarlarkLambda value, and returns a frozen *Dict
	// suitable for the activity boundary. The fast path (no lambdas)
	// returns the original frozen dict unchanged.
	for _, ref := range actions {
		resolved, rerr := i.resolveKwargs(ctx, ref)
		if rerr != nil {
			err = rerr
			return err
		}
		if resolved != nil {
			ref.Kwargs = resolved
		}
	}

	actx := workflow.WithActivityOptions(ctx, i.activityOptionsForStep(step))
	if err = workflow.ExecuteActivity(actx, "ExecuteBatch", actions).Get(ctx, &results); err != nil {
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

// buildStepActions returns step.Actions for static steps, evaluates
// step.ActionFn for dynamic single-action steps, and evaluates
// step.BlockFn for dynamic batches (D4.1-06).
//
// Strict return-type contract per D4.1-07:
//   - action_fn MUST return a single *ActionRef.
//   - block_fn MUST return a Starlark list whose every element is *ActionRef.
//
// Wrong types surface as temporal.NewNonRetryableApplicationError with
// the lambda position embedded in the message — this is a programming
// error in the .star file and Temporal must NOT retry it (D4.1-08).
//
// fail() callsite preservation (D4.1-08, B6): pkg/bridge/lambda_call.go
// re-wraps *starlark.EvalError so its Error() includes the inner-fail
// callsite (e.g. "<file>:<line>:<col>: fail: <reason>"). evalLambda then
// adds the lambda's def-position prefix (lambda <id> @ <pos>: ...). The
// composed message therefore carries BOTH positions, letting
// the test for the LambdaPanic case grep for ":<inner-line>:".
func (i *interpreter) buildStepActions(ctx workflow.Context, step *dag.Step) ([]*dag.ActionRef, error) {
	switch {
	case step.ActionFn != nil:
		val, err := i.evalLambda(ctx, step.ActionFn.ID)
		if err != nil {
			return nil, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("action_fn lambda failed (defined at %s): %v", step.ActionFn.Pos, err),
				"ActionFnFailed", nil)
		}
		ref, ok := val.(*dag.ActionRef)
		if !ok {
			return nil, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("action_fn returned %s, expected ActionRef (at %s)", val.Type(), step.ActionFn.Pos),
				"ActionFnTypeMismatch", nil)
		}
		return []*dag.ActionRef{ref}, nil
	case step.BlockFn != nil:
		val, err := i.evalLambda(ctx, step.BlockFn.ID)
		if err != nil {
			return nil, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("block_fn lambda failed (defined at %s): %v", step.BlockFn.Pos, err),
				"BlockFnFailed", nil)
		}
		list, ok := val.(*starlark.List)
		if !ok {
			return nil, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("block_fn returned %s, expected list of ActionRef (at %s)", val.Type(), step.BlockFn.Pos),
				"BlockFnTypeMismatch", nil)
		}
		out := make([]*dag.ActionRef, 0, list.Len())
		iter := list.Iterate()
		defer iter.Done()
		var v starlark.Value
		for iter.Next(&v) {
			ref, ok := v.(*dag.ActionRef)
			if !ok {
				return nil, temporal.NewNonRetryableApplicationError(
					fmt.Sprintf("block_fn batch entry is %s, expected ActionRef (at %s)", v.Type(), step.BlockFn.Pos),
					"BlockFnTypeMismatch", nil)
			}
			out = append(out, ref)
		}
		return out, nil
	default:
		return step.Actions, nil
	}
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
//
// Production round-trip handling: when results have crossed the Temporal
// JSON wire (e.g., real ExecuteBatch invocation, NOT a unit-test direct
// return), OkResult.Output is decoded as dag.RawOperationOutput{Bytes:
// <json>} per result_marshal.go's UnmarshalActionResult contract. The
// helper falls through to a JSON-keyed "status" lookup so the production
// renderer still sees status=N. The plain reflect path remains for unit
// tests that bypass the round-trip.
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

	// Fast path: production round-trip puts a RawOperationOutput here.
	// Parse the raw JSON for a "status" key (HTTPResponse and any other
	// extension Output that follows the convention).
	if raw, isRaw := ok.Output.(dag.RawOperationOutput); isRaw {
		if len(raw.Bytes) == 0 {
			return ""
		}
		var probe struct {
			Status *int `json:"status"`
		}
		if err := json.Unmarshal(raw.Bytes, &probe); err == nil && probe.Status != nil {
			return fmt.Sprintf("status=%d", *probe.Status)
		}
		return ""
	}

	// Reflection path: unit tests pass the typed Output directly (no
	// round-trip), so the Status field is reachable via reflect.
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
