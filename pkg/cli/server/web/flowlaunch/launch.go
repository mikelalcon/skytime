// Package flowlaunch is the single production seam for c.ExecuteWorkflow(
// "SkytimeWorkflow", dag.WorkflowInput{...}). UI-04 / D-7.3-28..D-7.3-31.
//
// Three production call sites after Phase 7.3:
//   - pkg/extension/receiver/handler.go (webhook ingress) calls Execute
//   - pkg/cli/server/web/handlers.go (manual trigger POST) calls Execute (lands in Plan 04)
//   - pkg/extension/schedules/schedules.go (cron reconcile) calls BuildWorkflowInput
//     only — the cron path goes through Temporal Schedules and never calls
//     ExecuteWorkflow from a Go process.
//
// pkg/cli/run.go retains a 2nd c.ExecuteWorkflow call site because skytime
// run is synchronous and needs the *WorkflowRun handle for run.Get(...);
// its workflowInput shape goes through BuildWorkflowInput so the input
// shape stays single-sourced.
//
// The firewall test tests/firewall_execute_workflow_test.go pins:
//
//	c.ExecuteWorkflow call sites: exactly 2 (flowlaunch/launch.go + pkg/cli/run.go)
//	BuildWorkflowInput call sites: exactly 3 (flowlaunch/launch.go internal
//	  same-package call + pkg/cli/run.go + pkg/extension/schedules/schedules.go)
package flowlaunch

import (
	"context"
	"errors"
	"fmt"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// Status is the outcome enumeration returned by Execute.
type Status int

const (
	// StatusOK means ExecuteWorkflow returned a nil error and the workflow
	// has been accepted by Temporal.
	StatusOK Status = iota
	// StatusDuplicate means the dispatch was rejected because a workflow
	// with the requested ID already exists (REJECT_DUPLICATE policy with
	// WorkflowExecutionErrorWhenAlreadyStarted=true surfaced as
	// *serviceerror.WorkflowExecutionAlreadyStarted).
	StatusDuplicate
	// StatusDispatchFailed covers every other ExecuteWorkflow error
	// (network, gRPC, server-side rejection that is not a duplicate).
	StatusDispatchFailed
)

// String returns a stable lowercase token for the Status, suitable for
// structured-log key/value pairs and tests.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusDuplicate:
		return "duplicate"
	case StatusDispatchFailed:
		return "dispatch_failed"
	default:
		return "unknown"
	}
}

// Options encodes per-caller divergence (D-7.3-28).
//
// Webhook ingress passes:
//
//	WorkflowID = composeWorkflowID(t, userKey)  // deterministic for idempotency
//	ReusePolicy = enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE
//	ErrorWhenAlreadyStarted = true              // Phase 7.1 Pitfall 1
//
// Manual trigger (Plan 04) passes:
//
//	WorkflowID = "manual/<flow>/<32-hex-random>"
//	ReusePolicy = enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE
//	ErrorWhenAlreadyStarted = false             // moot — fresh IDs
type Options struct {
	WorkflowID              string
	ReusePolicy             enumspb.WorkflowIdReusePolicy
	ErrorWhenAlreadyStarted bool
}

// BuildWorkflowInput is the single seam that constructs the
// dag.WorkflowInput shape passed to SkytimeWorkflow. UI-04 single source
// of truth — called from Execute, pkg/cli/run.go, and
// pkg/extension/schedules/schedules.go::scheduleOptionsFor.
//
// initState may be nil (cron reconcile passes nil; interpreter populates
// scheduled_time/actual_time at fire time per Phase 7.2 D-7.2-15).
func BuildWorkflowInput(flowName, contentHash string, initState map[string]any) dag.WorkflowInput {
	return dag.WorkflowInput{
		FlowName:    flowName,
		ContentHash: contentHash,
		InitState:   initState,
	}
}

// Execute dispatches one SkytimeWorkflow run. The single production
// c.ExecuteWorkflow call site (UI-04). Returns the Temporal-assigned
// workflow ID on success, the requested WorkflowID on duplicate, and ""
// on dispatch failure.
//
// err is wrapped with %w; callers can errors.As against
// *serviceerror.WorkflowExecutionAlreadyStarted when status ==
// StatusDuplicate.
func Execute(
	ctx context.Context,
	c client.Client,
	taskQueue string,
	flowName string,
	contentHash string,
	initState map[string]any,
	opts Options,
) (workflowID string, status Status, err error) {
	swOpts := client.StartWorkflowOptions{
		ID:                                       opts.WorkflowID,
		TaskQueue:                                taskQueue,
		WorkflowIDReusePolicy:                    opts.ReusePolicy,
		WorkflowExecutionErrorWhenAlreadyStarted: opts.ErrorWhenAlreadyStarted,
	}
	input := BuildWorkflowInput(flowName, contentHash, initState)
	run, execErr := c.ExecuteWorkflow(ctx, swOpts, "SkytimeWorkflow", input)
	if execErr == nil {
		return run.GetID(), StatusOK, nil
	}
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(execErr, &alreadyStarted) {
		return opts.WorkflowID, StatusDuplicate, execErr
	}
	return "", StatusDispatchFailed, fmt.Errorf("execute %s: %w", flowName, execErr)
}
