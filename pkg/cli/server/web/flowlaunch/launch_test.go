package flowlaunch_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/cli/server/web/flowlaunch"
)

// fakeRun is a minimal client.WorkflowRun implementation. Only GetID is
// exercised by flowlaunch.Execute on the success path.
type fakeRun struct {
	id string
}

func (r fakeRun) GetID() string                                                                  { return r.id }
func (r fakeRun) GetRunID() string                                                               { return "" }
func (r fakeRun) Get(_ context.Context, _ any) error                                             { return nil }
func (r fakeRun) GetWithOptions(_ context.Context, _ any, _ client.WorkflowRunGetOptions) error  { return nil }

// fakeClient embeds client.Client so the compiler accepts it as one;
// only ExecuteWorkflow is wired. Calling any other method nil-pans
// intentionally — flowlaunch.Execute must not touch them.
type fakeClient struct {
	client.Client
	run client.WorkflowRun
	err error
}

func (f *fakeClient) ExecuteWorkflow(
	_ context.Context,
	_ client.StartWorkflowOptions,
	_ any,
	_ ...any,
) (client.WorkflowRun, error) {
	return f.run, f.err
}

func TestExecute_StatusOK(t *testing.T) {
	fc := &fakeClient{run: fakeRun{id: "wf-abc"}}

	id, st, err := flowlaunch.Execute(
		context.Background(),
		fc,
		"skytime",
		"my_flow",
		"abc123",
		map[string]any{"k": "v"},
		flowlaunch.Options{
			WorkflowID:              "requested-id",
			ReusePolicy:             enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
			ErrorWhenAlreadyStarted: true,
		},
	)

	require.NoError(t, err)
	require.Equal(t, flowlaunch.StatusOK, st)
	require.Equal(t, "wf-abc", id)
}

func TestExecute_DuplicateError(t *testing.T) {
	dup := &serviceerror.WorkflowExecutionAlreadyStarted{Message: "already started"}
	fc := &fakeClient{err: dup}

	id, st, err := flowlaunch.Execute(
		context.Background(),
		fc,
		"skytime",
		"my_flow",
		"abc123",
		nil,
		flowlaunch.Options{
			WorkflowID:              "want-id",
			ReusePolicy:             enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
			ErrorWhenAlreadyStarted: true,
		},
	)

	require.Equal(t, flowlaunch.StatusDuplicate, st)
	require.Equal(t, "want-id", id)
	require.Error(t, err)

	var target *serviceerror.WorkflowExecutionAlreadyStarted
	require.True(t, errors.As(err, &target),
		"errors.As must succeed against *serviceerror.WorkflowExecutionAlreadyStarted; got %T (%v)", err, err)
}

func TestExecute_DispatchFailed(t *testing.T) {
	boom := errors.New("boom")
	fc := &fakeClient{err: boom}

	id, st, err := flowlaunch.Execute(
		context.Background(),
		fc,
		"skytime",
		"my_flow",
		"abc123",
		nil,
		flowlaunch.Options{
			WorkflowID:              "want-id",
			ReusePolicy:             enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
			ErrorWhenAlreadyStarted: true,
		},
	)

	require.Equal(t, flowlaunch.StatusDispatchFailed, st)
	require.Equal(t, "", id)
	require.Error(t, err)
	require.True(t, errors.Is(err, boom),
		"errors.Is must unwrap to the original boom error; got %v", err)
	msg := err.Error()
	require.Contains(t, msg, "execute")
	require.Contains(t, msg, "my_flow")
	require.True(t, strings.Contains(msg, "boom"),
		"wrapped error must contain underlying %q; got %q", "boom", msg)
}

func TestBuildWorkflowInput_ShapesInput(t *testing.T) {
	got := flowlaunch.BuildWorkflowInput("flow_x", "hash123", map[string]any{"k": "v"})

	require.Equal(t, "flow_x", got.FlowName)
	require.Equal(t, "hash123", got.ContentHash)
	require.Equal(t, map[string]any{"k": "v"}, got.InitState)
}
