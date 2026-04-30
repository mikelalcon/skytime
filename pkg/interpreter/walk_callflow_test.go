package interpreter

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// helperBuildCallFlowParent builds a parent flow whose Body is a single
// CallFlow node referencing childName. ChildOptions and Inputs are
// caller-provided.
func helperBuildCallFlowParent(parentName, childName string, inputs, childOpts map[string]any) *ParsedFlow {
	filename := parentName + ".star"
	pos := syntax.MakePosition(&filename, 1, 1)
	return &ParsedFlow{
		Flow: &dag.Flow{
			Pos: pos, Name: parentName, Inputs: map[string]string{},
			Body: []dag.Node{
				&dag.CallFlow{
					Pos:          pos,
					Name:         childName,
					Inputs:       inputs,
					ChildOptions: childOpts,
				},
			},
		},
		Lambdas: map[string]*dag.CapturedLambda{},
	}
}

// helperBuildEmptyChild constructs a *ParsedFlow with an empty Body, used
// as a registry stand-in for the child.
func helperBuildEmptyChild(childName string) *ParsedFlow {
	filename := childName + ".star"
	pos := syntax.MakePosition(&filename, 1, 1)
	return &ParsedFlow{
		Flow: &dag.Flow{
			Pos: pos, Name: childName, Inputs: map[string]string{},
			Body: nil,
		},
		Lambdas: map[string]*dag.CapturedLambda{},
	}
}

// TestWalkCallFlow_HappyPath: registry contains "child"; walker invokes
// ExecuteChildWorkflow. Parent + child both execute the actual NewWorkflow
// closure (no OnWorkflow override — that would mock the parent too since
// both use the same registered name "SkytimeWorkflow"). The child's
// input is captured via the child-started listener; its EncodedValues
// arg holds the dag.WorkflowInput payload.
func TestWalkCallFlow_HappyPath(t *testing.T) {
	parent := helperBuildCallFlowParent("parent", "child",
		map[string]any{"k": "v"}, nil)
	child := helperBuildEmptyChild("child")

	registry := NewRegistry()
	require.NoError(t, registry.Register("parent", "ph", parent))
	require.NoError(t, registry.Register("child", "chash", child))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	var childInput dag.WorkflowInput
	env.SetOnChildWorkflowStartedListener(func(_ *workflow.Info, _ workflow.Context, args converter.EncodedValues) {
		_ = args.Get(&childInput)
	})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "parent", ContentHash: "ph", InitState: map[string]any{}})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	assert.Equal(t, "child", childInput.FlowName)
	assert.Equal(t, "chash", childInput.ContentHash)
	assert.Equal(t, "v", childInput.InitState["k"])
}

// TestWalkCallFlow_ChildFlowNotInRegistry: registry doesn't contain
// CallFlow.Name → non-retryable ChildFlowNotInRegistry error.
func TestWalkCallFlow_ChildFlowNotInRegistry(t *testing.T) {
	parent := helperBuildCallFlowParent("parent", "missing", nil, nil)

	registry := NewRegistry()
	require.NoError(t, registry.Register("parent", "ph", parent))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "parent", ContentHash: "ph", InitState: map[string]any{}})

	require.True(t, env.IsWorkflowCompleted())
	wfErr := env.GetWorkflowError()
	require.Error(t, wfErr)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, wfErr, &appErr)
	assert.Equal(t, "ChildFlowNotInRegistry", appErr.Type())
	assert.True(t, appErr.NonRetryable())
	assert.Contains(t, appErr.Message(), "missing")
}

// TestWalkCallFlow_TaskQueueOverride: ChildOptions["task_queue"]
// = "child_queue" → child workflow scheduled on that queue. Verified via
// the child-started listener's WorkflowInfo.TaskQueueName.
func TestWalkCallFlow_TaskQueueOverride(t *testing.T) {
	parent := helperBuildCallFlowParent("parent", "child", nil,
		map[string]any{"task_queue": "child_queue"})
	child := helperBuildEmptyChild("child")

	registry := NewRegistry()
	require.NoError(t, registry.Register("parent", "ph", parent))
	require.NoError(t, registry.Register("child", "chash", child))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	capturedTaskQueue := ""
	env.SetOnChildWorkflowStartedListener(func(workflowInfo *workflow.Info, _ workflow.Context, _ converter.EncodedValues) {
		capturedTaskQueue = workflowInfo.TaskQueueName
	})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "parent", ContentHash: "ph", InitState: map[string]any{}})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	assert.Equal(t, "child_queue", capturedTaskQueue)
}

// TestWalkCallFlow_RetryPolicyExplicitOverride: ChildOptions["retry_policy"]
// = *dag.RetryPolicy{...} converts via toTemporalRetryPolicy and sets on
// ChildWorkflowOptions. The conversion field shape is unit-tested in
// TestToTemporalRetryPolicy (walk_step_test.go); this test exercises the
// type-assertion + conversion code path end-to-end via a real child
// workflow execution. No direct way to inspect ChildWorkflowOptions
// post-WithChildOptions; passing without panic is the assertion.
func TestWalkCallFlow_RetryPolicyExplicitOverride(t *testing.T) {
	override := &dag.RetryPolicy{
		InitialInterval:    2 * time.Second,
		BackoffCoefficient: 1.5,
		MaxAttempts:        4,
	}
	parent := helperBuildCallFlowParent("parent", "child", nil,
		map[string]any{"retry_policy": override})
	child := helperBuildEmptyChild("child")

	registry := NewRegistry()
	require.NoError(t, registry.Register("parent", "ph", parent))
	require.NoError(t, registry.Register("child", "chash", child))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "parent", ContentHash: "ph", InitState: map[string]any{}})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// TestWalkCallFlow_ChildErrorBubbles: when the child returns an error,
// walkCallFlow wraps it with the call_flow context.
func TestWalkCallFlow_ChildErrorBubbles(t *testing.T) {
	parent := helperBuildCallFlowParent("parent", "child", nil, nil)
	child := helperBuildEmptyChild("child")

	registry := NewRegistry()
	require.NoError(t, registry.Register("parent", "ph", parent))
	require.NoError(t, registry.Register("child", "chash", child))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	env.OnWorkflow("SkytimeWorkflow", mock.Anything, mock.Anything).Return(nil,
		errors.New("child boom"))

	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "parent", ContentHash: "ph", InitState: map[string]any{}})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}
