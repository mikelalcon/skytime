package interpreter

// White-box tests for walkStep + activityOptionsForStep.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// helperRegisterFakeExecuteBatch registers a fake activity under the
// "ExecuteBatch" name so OnActivity can target it. The fake's body never
// runs because OnActivity overrides; what matters is type registration so
// the SDK knows what to encode/decode.
func helperRegisterFakeExecuteBatch(env *testsuite.TestWorkflowEnvironment) {
	fake := func(_ context.Context, _ []*dag.ActionRef) ([]dag.ActionResult, error) {
		return nil, nil
	}
	env.RegisterActivityWithOptions(fake, activity.RegisterOptions{Name: "ExecuteBatch"})
}

// helperMakeStepFlow builds a minimal *ParsedFlow whose Body is a single
// dag.Step with the given Actions, optional TaskQueue, optional Retry,
// optional Timeout, and an optional flow-level TaskQueue.
func helperMakeStepFlow(t *testing.T, flowName, flowTQ string, step *dag.Step) *ParsedFlow {
	t.Helper()
	filename := flowName + ".star"
	return &ParsedFlow{
		Flow: &dag.Flow{
			Pos:       syntax.MakePosition(&filename, 1, 1),
			Name:      flowName,
			Inputs:    map[string]string{},
			Body:      []dag.Node{step},
			TaskQueue: flowTQ,
		},
		Lambdas: map[string]*dag.CapturedLambda{},
	}
}

func helperMakeStepWithActions(taskQueue string, retry *dag.RetryPolicy, n int) *dag.Step {
	filename := "step.star"
	actions := make([]*dag.ActionRef, 0, n)
	for i := 0; i < n; i++ {
		actions = append(actions, &dag.ActionRef{
			Pos:    syntax.MakePosition(&filename, int32(i+1), 1),
			Kind_:  "fake.echo",
			Kwargs: starlark.NewDict(0),
		})
	}
	return &dag.Step{
		Pos:       syntax.MakePosition(&filename, 1, 1),
		Actions:   actions,
		Retry:     retry,
		TaskQueue: taskQueue,
	}
}

// TestWalkStep_HappyPath: a Step with one ActionRef triggers
// workflow.ExecuteActivity("ExecuteBatch", ...) via mocked OnActivity.
func TestWalkStep_HappyPath(t *testing.T) {
	step := helperMakeStepWithActions("", nil, 1)
	parsed := helperMakeStepFlow(t, "happy", "", step)

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	// Mock the activity by name; return a single OkResult.
	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(
		dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil,
	)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName:    "happy",
		ContentHash: "h",
		InitState:   map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// TestWalkStep_TaskQueueOverride: a Step with TaskQueue="slow_io" causes
// the activity to be scheduled on that queue. Verified by setting the
// activity to register on a specific task queue and observing only that
// queue's mock fires.
func TestWalkStep_TaskQueueOverride(t *testing.T) {
	step := helperMakeStepWithActions("slow_io", nil, 1)
	parsed := helperMakeStepFlow(t, "tqstep", "" /* flow TQ unset */, step)

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	// Capture the per-activity options via the started listener.
	capturedTaskQueue := ""
	env.SetOnActivityStartedListener(func(activityInfo *activity.Info, _ context.Context, _ converter.EncodedValues) {
		capturedTaskQueue = activityInfo.TaskQueue
	})
	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(
		dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil,
	)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "tqstep", ContentHash: "h", InitState: nil})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	assert.Equal(t, "slow_io", capturedTaskQueue, "step.TaskQueue must override the default queue")
}

// TestWalkStep_FlowTaskQueueInherits: when Step.TaskQueue == "" but
// Flow.TaskQueue == "critical", the activity gets TaskQueue == "critical".
func TestWalkStep_FlowTaskQueueInherits(t *testing.T) {
	step := helperMakeStepWithActions("" /* step TQ unset */, nil, 1)
	parsed := helperMakeStepFlow(t, "ftq", "critical", step)

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	capturedTaskQueue := ""
	env.SetOnActivityStartedListener(func(activityInfo *activity.Info, _ context.Context, _ converter.EncodedValues) {
		capturedTaskQueue = activityInfo.TaskQueue
	})
	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(
		dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil,
	)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "ftq", ContentHash: "h", InitState: nil})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	assert.Equal(t, "critical", capturedTaskQueue, "flow.TaskQueue must be inherited when step.TaskQueue is empty")
}

// TestWalkStep_RetryPolicyConverted: Step.Retry decodes into
// activity options' RetryPolicy field. Tested via a unit-level call to
// activityOptionsForStep — the integration-level retry behavior is
// Temporal's job, not ours.
func TestWalkStep_RetryPolicyConverted(t *testing.T) {
	retry := &dag.RetryPolicy{
		InitialInterval:    1 * time.Second,
		BackoffCoefficient: 2.0,
		MaxAttempts:        3,
		NonRetryableErrors: []string{"FOO"},
	}
	step := helperMakeStepWithActions("", retry, 1)
	parsed := helperMakeStepFlow(t, "retrytest", "", step)

	// Build a bare interpreter (no workflow context needed for the helper).
	i := &interpreter{
		flow: parsed.Flow,
	}
	opts := i.activityOptionsForStep(step)
	require.NotNil(t, opts.RetryPolicy)
	assert.Equal(t, 1*time.Second, opts.RetryPolicy.InitialInterval)
	assert.Equal(t, 2.0, opts.RetryPolicy.BackoffCoefficient)
	assert.Equal(t, int32(3), opts.RetryPolicy.MaximumAttempts)
	assert.Equal(t, []string{"FOO"}, opts.RetryPolicy.NonRetryableErrorTypes)
}

// TestComputeBatchTimeout_SumPlusHeadroom: helper computes
// len(actions) * defaultActionTimeout + headroom when no Timeout
// override; uses Timeout.StartToClose + headroom when set.
func TestComputeBatchTimeout_SumPlusHeadroom(t *testing.T) {
	cases := []struct {
		name string
		step *dag.Step
		want time.Duration
	}{
		{
			name: "no timeout, 2 actions",
			step: &dag.Step{Actions: []*dag.ActionRef{{}, {}}},
			want: 2*60*time.Second + 30*time.Second,
		},
		{
			name: "no timeout, 1 action",
			step: &dag.Step{Actions: []*dag.ActionRef{{}}},
			want: 60*time.Second + 30*time.Second,
		},
		{
			name: "explicit StartToClose override",
			step: &dag.Step{
				Actions: []*dag.ActionRef{{}, {}},
				Timeout: &dag.Timeout{StartToClose: 5 * time.Minute},
			},
			want: 5*time.Minute + 30*time.Second,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeBatchTimeout(tc.step)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Quick 260502-onc Fix B: extractStatusSummary + extractFirstNonRetryable
// helper-level coverage. The integration test below
// (TestWalkStep_NonRetryableResult_FailsWorkflow) proves the helpers are
// wired into walkStep.
// ---------------------------------------------------------------------------

// fakeStatusOutput is a value-receiver dag.OperationOutput exposing the
// "Status int" shape extractStatusSummary reflects on. Mirrors the
// HTTPResponse shape without importing pkg/extension/builtin/http
// (firewall preserved).
type fakeStatusOutput struct{ Status int }

func (fakeStatusOutput) IsOperationOutput() {}

// fakeStatusOutputPtr is a pointer-receiver variant — extractStatusSummary
// must reflect.Elem when given a pointer.
type fakeStatusOutputPtr struct{ Status int }

func (*fakeStatusOutputPtr) IsOperationOutput() {}

// fakeNoStatusOutput has NO Status field — extractStatusSummary returns "".
type fakeNoStatusOutput struct{ Foo string }

func (fakeNoStatusOutput) IsOperationOutput() {}

// fakeStringStatusOutput has a Status field that is NOT an int —
// extractStatusSummary returns "" (Kind != Int).
type fakeStringStatusOutput struct{ Status string }

func (fakeStringStatusOutput) IsOperationOutput() {}

func TestExtractStatusSummary_HTTPResponseShape(t *testing.T) {
	results := dag.ActionResults{dag.OkResult{Idx: 0, Output: fakeStatusOutput{Status: 200}}}
	require.Equal(t, "status=200", extractStatusSummary(results))
}

func TestExtractStatusSummary_NoStatusField(t *testing.T) {
	results := dag.ActionResults{dag.OkResult{Idx: 0, Output: fakeNoStatusOutput{Foo: "bar"}}}
	require.Equal(t, "", extractStatusSummary(results))
}

func TestExtractStatusSummary_MultiAction(t *testing.T) {
	results := dag.ActionResults{
		dag.OkResult{Idx: 0, Output: fakeStatusOutput{Status: 200}},
		dag.OkResult{Idx: 1, Output: fakeStatusOutput{Status: 200}},
		dag.OkResult{Idx: 2, Output: fakeStatusOutput{Status: 200}},
	}
	require.Equal(t, "3 ok", extractStatusSummary(results))
}

func TestExtractStatusSummary_NilOutput(t *testing.T) {
	results := dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}
	require.Equal(t, "", extractStatusSummary(results))
}

func TestExtractStatusSummary_PointerOutput(t *testing.T) {
	results := dag.ActionResults{dag.OkResult{Idx: 0, Output: &fakeStatusOutputPtr{Status: 404}}}
	require.Equal(t, "status=404", extractStatusSummary(results))
}

func TestExtractStatusSummary_StatusNotInt(t *testing.T) {
	results := dag.ActionResults{dag.OkResult{Idx: 0, Output: fakeStringStatusOutput{Status: "ok"}}}
	require.Equal(t, "", extractStatusSummary(results))
}

func TestExtractStatusSummary_EmptyResults(t *testing.T) {
	require.Equal(t, "", extractStatusSummary(dag.ActionResults{}))
	require.Equal(t, "", extractStatusSummary(nil))
}

// TestExtractStatusSummary_NonOkSingleResult — a single-result slice
// containing a NonRetryableErrResult or SkippedResult does not have a
// reflectable Output; helper returns "" (caller's failure-path summary
// kicks in via the defer's err.Error() branch instead).
func TestExtractStatusSummary_NonOkSingleResult(t *testing.T) {
	results := dag.ActionResults{dag.NonRetryableErrResult{Idx: 0, Err: errorsNew("x")}}
	require.Equal(t, "", extractStatusSummary(results))
}

func TestExtractFirstNonRetryable_None(t *testing.T) {
	results := dag.ActionResults{
		dag.OkResult{Idx: 0, Output: nil},
		dag.OkResult{Idx: 1, Output: nil},
	}
	require.Nil(t, extractFirstNonRetryable(results))
}

// TestExtractFirstNonRetryable_FirstWins — covers the "first NonRetryable
// in a mixed slice wins" property from BEHAVIOR M-3 note. This is the
// helper-level proxy for the originally-named
// TestWalkStep_NonRetryableMidBlock_FailsWorkflow.
func TestExtractFirstNonRetryable_FirstWins(t *testing.T) {
	errA := errorsNew("first failure")
	errB := errorsNew("second failure")
	results := dag.ActionResults{
		dag.OkResult{Idx: 0, Output: nil},
		dag.NonRetryableErrResult{Idx: 1, Err: errA},
		dag.NonRetryableErrResult{Idx: 2, Err: errB},
		dag.SkippedResult{Idx: 3, Reason: "after first failure"},
	}
	got := extractFirstNonRetryable(results)
	// Compare by Error() text — errImpl is a value type so require.Same
	// (pointer identity) does not apply. The "first wins" property is what
	// matters: got's message must be errA's, NOT errB's.
	require.NotNil(t, got)
	require.Equal(t, errA.Error(), got.Error(),
		"first NonRetryableErrResult.Err must win, even when more follow")
	require.NotEqual(t, errB.Error(), got.Error(),
		"second NonRetryableErrResult.Err must NOT shadow the first")
}

func TestExtractFirstNonRetryable_SkippedIgnored(t *testing.T) {
	results := dag.ActionResults{
		dag.OkResult{Idx: 0, Output: nil},
		dag.SkippedResult{Idx: 1, Reason: "x"},
	}
	require.Nil(t, extractFirstNonRetryable(results))
}

// TestWalkStep_NonRetryableResult_FailsWorkflow — Quick 260502-onc Fix B-2
// integration gate: the activity returns (results, nil) per D2-14 with a
// NonRetryableErrResult; walkStep MUST surface that as a workflow-level
// error so downstream renderers print flow_failed. Without this, the
// silent-success failure mode persists past Fix A.
func TestWalkStep_NonRetryableResult_FailsWorkflow(t *testing.T) {
	step := helperMakeStepWithActions("", nil, 1)
	parsed := helperMakeStepFlow(t, "nrfail", "", step)

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(
		dag.ActionResults{dag.NonRetryableErrResult{Idx: 0, Err: errorsNew("HTTP 404 GET https://x/y: not found: non-retryable")}},
		nil,
	)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName:    "nrfail",
		ContentHash: "h",
		InitState:   map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	wfErr := env.GetWorkflowError()
	require.Error(t, wfErr,
		"Fix B-2: NonRetryableErrResult in activity results MUST surface as workflow error")
	require.Contains(t, wfErr.Error(), "HTTP 404",
		"original wrapped error message must propagate to workflow level for renderer attribution")
}

// errorsNew is a 2-line helper that avoids importing "errors" at the top
// of this file (which already has a heavy import block). Renamed so it
// doesn't collide with the stdlib name in IDE auto-import.
func errorsNew(msg string) error { return errImpl(msg) }

type errImpl string

func (e errImpl) Error() string { return string(e) }

// TestToTemporalRetryPolicy: nil input returns nil; populated input
// converts each field correctly.
func TestToTemporalRetryPolicy(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		require.Nil(t, toTemporalRetryPolicy(nil))
	})
	t.Run("populated maps fields", func(t *testing.T) {
		src := &dag.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 1.5,
			MaxAttempts:        7,
			NonRetryableErrors: []string{"BAR", "BAZ"},
		}
		got := toTemporalRetryPolicy(src)
		require.NotNil(t, got)
		want := &temporal.RetryPolicy{
			InitialInterval:        2 * time.Second,
			BackoffCoefficient:     1.5,
			MaximumAttempts:        7,
			NonRetryableErrorTypes: []string{"BAR", "BAZ"},
		}
		assert.Equal(t, want, got)
	})
}
