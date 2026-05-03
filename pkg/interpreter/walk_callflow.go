package interpreter

import (
	"fmt"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// walkCallFlow invokes a child workflow per D3-09. Inheritance defaults:
//
//   - RetryPolicy: copied from the parent's run-level RetryPolicy via
//     workflow.GetInfo(ctx).RetryPolicy (D3-10). Override via
//     ChildOptions["retry_policy"] = *dag.RetryPolicy.
//   - TypedSearchAttributes: propagated via workflow.GetTypedSearchAttributes
//     (D3-11). The deprecated SearchAttributes map is intentionally left
//     unset (Pitfall #4).
//   - TaskQueue: not inherited from parent automatically — the SDK uses the
//     parent's task queue when ChildWorkflowOptions.TaskQueue is empty.
//     Override via ChildOptions["task_queue"] = string.
//   - Memo: NOT propagated in v1. parentInfo.Memo is *commonpb.Memo (a
//     protobuf message), but ChildWorkflowOptions.Memo is
//     map[string]interface{}. Conversion requires Payload decoding which
//     is non-trivial and not in v1 scope. D3-11 mandates search-attribute
//     propagation but is silent on Memo; deferred.
//
// On registry miss (the called flow_name has 0 or >1 versions), returns
// a non-retryable ChildFlowNotInRegistry application error so operators
// see a clear error path rather than a non-deterministic pick.
//
// Quick 260502-guu Fix B: emits step_dispatch + step_complete via
// workflow.GetLogger(ctx). Named-return + defer guarantees the
// completion event fires on every return path.
func (i *interpreter) walkCallFlow(ctx workflow.Context, cf *dag.CallFlow) (err error) {
	logger := workflow.GetLogger(ctx)
	start := workflow.Now(ctx)
	parentIdx := i.stepIdx
	parentTot := i.stepTot
	parentPath := i.currentPath()

	logger.Info("skytime",
		"event", "step_dispatch",
		"kind", "call_flow",
		"label", cf.Name,
		"idx", parentIdx, "total", parentTot, "path", parentPath,
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
			"kind", "call_flow",
			"label", cf.Name,
			"status", status,
			"duration_ms", workflow.Now(ctx).Sub(start).Milliseconds(),
			"idx", parentIdx, "total", parentTot, "path", parentPath,
			"summary", summary,
		)
	}()

	parentInfo := workflow.GetInfo(ctx)

	// Resolve the child's content hash from the registry.
	childHash, ok := i.registry.ContentHashFor(cf.Name)
	if !ok {
		err = temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("call_flow %q at %s: child flow not found in worker registry (or registered with multiple versions)", cf.Name, cf.Pos),
			"ChildFlowNotInRegistry", nil,
		)
		return err
	}

	cwo := workflow.ChildWorkflowOptions{
		RetryPolicy:           parentInfo.RetryPolicy, // *temporal.RetryPolicy
		TypedSearchAttributes: workflow.GetTypedSearchAttributes(ctx),
	}
	// ChildOptions kwargs override defaults. Convention for v1:
	//   "task_queue":   string
	//   "retry_policy": *dag.RetryPolicy (parent inheritance is the default;
	//                   explicit override here)
	// SearchAttributes override is NOT plumbed in v1; inheritance only.
	if tq, ok := cf.ChildOptions["task_queue"].(string); ok && tq != "" {
		cwo.TaskQueue = tq
	}
	if rp, ok := cf.ChildOptions["retry_policy"].(*dag.RetryPolicy); ok {
		cwo.RetryPolicy = toTemporalRetryPolicy(rp)
	}

	childCtx := workflow.WithChildOptions(ctx, cwo)

	subInput := dag.WorkflowInput{
		FlowName:    cf.Name,
		ContentHash: childHash,
		InitState:   coerceCallFlowInputs(cf.Inputs),
	}

	var result map[string]any
	if cerr := workflow.ExecuteChildWorkflow(childCtx, "SkytimeWorkflow", subInput).Get(ctx, &result); cerr != nil {
		err = fmt.Errorf("call_flow %s at %s: %w", cf.Name, cf.Pos, cerr)
		return err
	}
	// Result is the child's final state. v1 doesn't propagate it back into
	// parent state; Phase 6 may surface a need. The child's history is
	// observable independently via Temporal's UI/CLI.
	_ = result
	return nil
}

// coerceCallFlowInputs builds the child workflow's InitState. The CallFlow
// node's Inputs is `map[string]any` of pure data (D-19 forbids lambdas
// inside CallFlow inputs). For v1 we copy keys in sorted order (D3-23
// determinism) and pass through values verbatim.
func coerceCallFlowInputs(inputs map[string]any) map[string]any {
	if len(inputs) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(inputs))
	for _, k := range sortedKeys(inputs) {
		out[k] = inputs[k]
	}
	return out
}
