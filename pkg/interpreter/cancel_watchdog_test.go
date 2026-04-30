package interpreter

// White-box test: package is `interpreter` (not `interpreter_test`) so the
// unexported makeCancelChannel symbol is directly callable. The firewall
// meta-test in firewall_test.go intentionally stays `package interpreter_test`
// because it does AST analysis from outside.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// TestMakeCancelChannel_ClosesOnWorkflowCancel exercises the happy path:
// inside a Temporal test workflow, makeCancelChannel returns a native
// channel that closes when its bound workflow context is cancelled.
// Cancellation is triggered by calling cancel() on a workflow.WithCancel-
// derived context after a small workflow.Sleep. The test then sleeps
// again briefly to let the bridge's workflow.Go reader observe Done() and
// run the sync.Once-guarded close, then verifies the native channel is
// closed via a non-blocking select.
//
// This is the critical determinism seam (D3-21, RESEARCH §"Pattern 6"):
// workflow.Channel → native chan struct{} bridging via a workflow.Go
// reader that closes the native channel inside a sync.Once guard.
//
// Test design note: the bridge's reader runs in a workflow.Go coroutine,
// so the SDK scheduler must be given a chance to dispatch it. We use a
// post-cancel workflow.Sleep to yield; the deterministic test environment
// fast-forwards timers, so the sleep duration is irrelevant — any
// non-zero duration triggers a coroutine scheduling pass.
func TestMakeCancelChannel_ClosesOnWorkflowCancel(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) (bool, error) {
		// Derive a child context we can cancel deliberately.
		childCtx, cancel := workflow.WithCancel(ctx)
		ch := makeCancelChannel(childCtx)

		// Yield to let the bridge's workflow.Go reader register on
		// childCtx.Done(). Without this initial yield the reader hasn't
		// started its Receive call yet.
		_ = workflow.Sleep(ctx, time.Microsecond)

		// Trigger cancellation.
		cancel()

		// Yield again so the bridge reader observes Done() firing and
		// runs its sync.Once close.
		_ = workflow.Sleep(ctx, time.Microsecond)

		// Non-blocking select: the bridged native channel must be closed.
		select {
		case <-ch:
			return true, nil
		default:
			return false, nil
		}
	}

	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError(), "workflow must complete cleanly after cancel propagates to native channel")

	var closed bool
	require.NoError(t, env.GetWorkflowResult(&closed))
	require.True(t, closed, "native channel must be closed after workflow ctx cancel")
}

// TestMakeCancelChannel_StaysOpenIfNotCancelled: a workflow that calls
// makeCancelChannel and immediately exits cleanly leaves the bridged
// channel open during the workflow's lifetime. We can't easily observe
// "stays open forever" but we CAN assert that within the workflow body,
// before completion, the channel is not yet closed (verified via a
// non-blocking select with default).
func TestMakeCancelChannel_StaysOpenIfNotCancelled(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) (bool, error) {
		ch := makeCancelChannel(ctx)
		// Yield once so the workflow.Go reader inside makeCancelChannel
		// has a chance to schedule. workflow.Sleep(0) is the canonical
		// "let other coroutines run" pattern.
		_ = workflow.Sleep(ctx, time.Microsecond)

		// Non-blocking read — channel should NOT be ready yet.
		select {
		case <-ch:
			return true, nil // would indicate the channel closed unexpectedly
		default:
			return false, nil
		}
	}

	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var closedEarly bool
	require.NoError(t, env.GetWorkflowResult(&closedEarly))
	require.False(t, closedEarly, "channel must NOT close while workflow ctx is alive")
}

// TestMakeCancelChannel_IndependentChannels verifies that two channels
// produced by two makeCancelChannel calls are independent — cancellation
// of one parent's child context does NOT close the other's native chan.
// The sync.Once guard around close(ch) is per-channel, not shared, so
// each call gets its own once + chan struct{}.
func TestMakeCancelChannel_IndependentChannels(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) ([2]bool, error) {
		// Two independent child contexts; cancel only the first.
		ctx1, cancel1 := workflow.WithCancel(ctx)
		ctx2, _ := workflow.WithCancel(ctx)

		ch1 := makeCancelChannel(ctx1)
		ch2 := makeCancelChannel(ctx2)

		// Yield so both bridge readers register on their respective Done()s.
		_ = workflow.Sleep(ctx, time.Microsecond)

		// Cancel only ctx1.
		cancel1()

		// Yield so bridge reader for ctx1 observes Done() and closes ch1.
		_ = workflow.Sleep(ctx, time.Microsecond)

		var ch1Closed, ch2Closed bool
		select {
		case <-ch1:
			ch1Closed = true
		default:
		}
		select {
		case <-ch2:
			ch2Closed = true
		default:
		}
		return [2]bool{ch1Closed, ch2Closed}, nil
	}

	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var states [2]bool
	require.NoError(t, env.GetWorkflowResult(&states))
	require.True(t, states[0], "ch1 must close after ctx1 cancellation")
	require.False(t, states[1], "ch2 must NOT close — cancellation of ctx1 is independent")
}
