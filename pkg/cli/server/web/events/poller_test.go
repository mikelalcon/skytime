package events

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeListClient is the test stand-in for the narrowed listClient
// interface. The List methods replay queued results (one per call) and
// return empty responses after exhaustion. Setting openErr / closedErr
// replays the error every call.
type fakeListClient struct {
	openResults   []*workflowservice.ListOpenWorkflowExecutionsResponse
	closedResults []*workflowservice.ListClosedWorkflowExecutionsResponse
	openErr       error
	closedErr     error
	openIdx       int
	closedIdx     int
}

func (f *fakeListClient) ListOpenWorkflow(_ context.Context, _ *workflowservice.ListOpenWorkflowExecutionsRequest) (*workflowservice.ListOpenWorkflowExecutionsResponse, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	if f.openIdx >= len(f.openResults) {
		return &workflowservice.ListOpenWorkflowExecutionsResponse{}, nil
	}
	r := f.openResults[f.openIdx]
	f.openIdx++
	return r, nil
}

func (f *fakeListClient) ListClosedWorkflow(_ context.Context, _ *workflowservice.ListClosedWorkflowExecutionsRequest) (*workflowservice.ListClosedWorkflowExecutionsResponse, error) {
	if f.closedErr != nil {
		return nil, f.closedErr
	}
	if f.closedIdx >= len(f.closedResults) {
		return &workflowservice.ListClosedWorkflowExecutionsResponse{}, nil
	}
	r := f.closedResults[f.closedIdx]
	f.closedIdx++
	return r, nil
}

// makeInfo constructs a minimal WorkflowExecutionInfo for tests.
func makeInfo(id string, status enumspb.WorkflowExecutionStatus, historyLen int64) *workflowpb.WorkflowExecutionInfo {
	return &workflowpb.WorkflowExecutionInfo{
		Execution:     &commonpb.WorkflowExecution{WorkflowId: id, RunId: "run-" + id},
		Type:          &commonpb.WorkflowType{Name: "SkytimeWorkflow"},
		StartTime:     timestamppb.New(time.Now()),
		Status:        status,
		HistoryLength: historyLen,
	}
}

// quietLogger discards all output so test runs stay clean.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPoller_EmitsWorkflowStarted(t *testing.T) {
	b := NewBroadcaster(nil)
	defer b.Shutdown()
	_, ch, unsub := b.Subscribe()
	defer unsub()

	fc := &fakeListClient{
		openResults: []*workflowservice.ListOpenWorkflowExecutionsResponse{{
			Executions: []*workflowpb.WorkflowExecutionInfo{makeInfo("wf-1", enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, 3)},
		}},
	}
	p := newPollerInternal(fc, b, PollerConfig{PollInterval: 10 * time.Hour}, quietLogger())
	p.tick(context.Background())

	select {
	case ev := <-ch:
		require.Equal(t, "workflow_started", ev.Name)
		ws, ok := ev.Payload.(WorkflowState)
		require.True(t, ok, "payload must be WorkflowState")
		require.Equal(t, "wf-1", ws.WorkflowID)
		require.Equal(t, "running", ws.Status)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive workflow_started event")
	}
}

func TestPoller_EmitsStatusChanged(t *testing.T) {
	b := NewBroadcaster(nil)
	defer b.Shutdown()
	_, ch, unsub := b.Subscribe()
	defer unsub()

	fc := &fakeListClient{
		openResults: []*workflowservice.ListOpenWorkflowExecutionsResponse{{
			Executions: []*workflowpb.WorkflowExecutionInfo{makeInfo("wf-1", enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, 3)},
		}},
		closedResults: []*workflowservice.ListClosedWorkflowExecutionsResponse{
			{}, // first tick: empty closed
			{Executions: []*workflowpb.WorkflowExecutionInfo{makeInfo("wf-1", enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED, 8)}},
		},
	}
	p := newPollerInternal(fc, b, PollerConfig{PollInterval: 10 * time.Hour}, quietLogger())
	p.tick(context.Background()) // workflow_started
	// Drain the started event so we can isolate the next.
	<-ch
	p.tick(context.Background()) // workflow_status_changed

	select {
	case ev := <-ch:
		require.Equal(t, "workflow_status_changed", ev.Name)
		ws, ok := ev.Payload.(WorkflowState)
		require.True(t, ok, "payload must be WorkflowState")
		require.Equal(t, "completed", ws.Status)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive workflow_status_changed event")
	}
}

func TestPoller_EmitsReplayed_HistoryLengthJump(t *testing.T) {
	b := NewBroadcaster(nil)
	defer b.Shutdown()
	_, ch, unsub := b.Subscribe()
	defer unsub()

	fc := &fakeListClient{
		openResults: []*workflowservice.ListOpenWorkflowExecutionsResponse{
			{Executions: []*workflowpb.WorkflowExecutionInfo{makeInfo("wf-1", enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, 5)}},
			{Executions: []*workflowpb.WorkflowExecutionInfo{makeInfo("wf-1", enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, 100)}}, // delta = 95 > threshold 50
		},
	}
	p := newPollerInternal(fc, b, PollerConfig{PollInterval: 10 * time.Hour, ReplayHistoryThreshold: 50}, quietLogger())
	p.tick(context.Background()) // started
	<-ch
	p.tick(context.Background()) // replayed

	select {
	case ev := <-ch:
		require.Equal(t, "workflow_replayed", ev.Name)
		ws, ok := ev.Payload.(WorkflowState)
		require.True(t, ok, "payload must be WorkflowState")
		require.Equal(t, 1, ws.ReplayCount)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive workflow_replayed event")
	}
}

func TestPoller_FetchError_DoesNotCrash(t *testing.T) {
	b := NewBroadcaster(nil)
	defer b.Shutdown()
	_, ch, unsub := b.Subscribe()
	defer unsub()

	fc := &fakeListClient{openErr: errors.New("temporal down")}
	p := newPollerInternal(fc, b, PollerConfig{PollInterval: 10 * time.Hour}, quietLogger())
	require.NotPanics(t, func() { p.tick(context.Background()) })

	// No event should arrive.
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event after fetch error: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPoller_MergeOpenClosed_Dedupes(t *testing.T) {
	b := NewBroadcaster(nil)
	defer b.Shutdown()
	_, ch, unsub := b.Subscribe()
	defer unsub()

	fc := &fakeListClient{
		openResults: []*workflowservice.ListOpenWorkflowExecutionsResponse{{
			Executions: []*workflowpb.WorkflowExecutionInfo{makeInfo("wf-dup", enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, 3)},
		}},
		closedResults: []*workflowservice.ListClosedWorkflowExecutionsResponse{{
			Executions: []*workflowpb.WorkflowExecutionInfo{makeInfo("wf-dup", enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED, 9)},
		}},
	}
	p := newPollerInternal(fc, b, PollerConfig{PollInterval: 10 * time.Hour}, quietLogger())
	p.tick(context.Background())

	// Should produce ONE workflow_started with the COMPLETED state (closed wins).
	select {
	case ev := <-ch:
		require.Equal(t, "workflow_started", ev.Name)
		ws, ok := ev.Payload.(WorkflowState)
		require.True(t, ok, "payload must be WorkflowState")
		require.Equal(t, "completed", ws.Status, "closed should win on duplicate")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive event")
	}

	// No second event for the same ID.
	select {
	case ev := <-ch:
		t.Fatalf("unexpected second event: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}

	// CurrentSnapshot returns one workflow.
	snap := p.CurrentSnapshot()
	require.Len(t, snap, 1)
	require.Equal(t, "wf-dup", snap[0].WorkflowID)
	require.Equal(t, "completed", snap[0].Status)
}
