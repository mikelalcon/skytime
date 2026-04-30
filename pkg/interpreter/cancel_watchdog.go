package interpreter

import (
	"sync"

	"go.temporal.io/sdk/workflow"
)

// makeCancelChannel returns a native <-chan struct{} that closes when
// ctx is cancelled. The bridge coroutine runs deterministically under
// workflow.Go, so the close happens at the same logical workflow time
// on replay. Closing a Go channel is a constant-time deterministic
// operation; reads from the returned channel inside bridge.CallLambda's
// native watchdog are safe because that watchdog is not workflow code
// (lives in pkg/bridge, no workflow.Context import).
//
// D3-21: this is the only legal interaction between workflow.Context and
// *starlark.Thread. The reconciliation rationale (why a native chan is
// OK here despite the no-native-go rule) is documented in
// .planning/phases/03-lambda-serialization-decision-interpreter-worker/
// 03-RESEARCH.md §"Pattern 6: Cancellation Watchdog (THE TRICKY PART)".
//
// Lifecycle: each call to makeCancelChannel spawns ONE workflow.Go
// coroutine that lives for the rest of the workflow run. If 1000
// bridge.CallLambda invocations each get their own channel, that's 1000
// long-lived coroutines. For v1 this is acceptable: workflows under v1
// don't have unbounded lambda evals.
//
// Fallback if integration tests in plan 03-03 surface flakiness, the
// fallback is to remove this watchdog and rely on a pre-eval ctx.Err()
// check inside the interpreter walkers (accepting up to MaxExecutionSteps
// latency for mid-evaluation cancellation). RESEARCH.md §"Pattern 6 risk
// acknowledgment" has the full rationale; the alternative wraps each
// bridge.CallLambda invocation with workflow.WithCancel and cancels after
// CallLambda returns, eliminating the dangling coroutine.
//
// Note on cleanup: the spawned coroutine reads ctx.Done() until the
// workflow exits. On exit, ctx is cancelled, the read unblocks, the
// sync.Once-guarded close(ch) runs, the coroutine returns. No leak.
//
// Idempotency: close(ch) is wrapped in sync.Once so even an unexpected
// double-fire of Done().Receive (which should fire exactly once on
// cancel per Temporal's contract) cannot panic. Using sync.Once here
// is a defense-in-depth measure (per blocker fix W9); the SDK's
// cancellation contract guarantees single-fire, so the Once is
// belt-and-suspenders.
func makeCancelChannel(ctx workflow.Context) <-chan struct{} {
	ch := make(chan struct{})
	var once sync.Once
	closer := func() {
		once.Do(func() { close(ch) })
	}
	workflow.Go(ctx, func(bctx workflow.Context) {
		// Done() returns a workflow.Channel; Receive blocks until the
		// workflow context is cancelled. Pitfall #1: this MUST be inside
		// workflow.Go — calling it from the main workflow goroutine
		// would block the whole walker (lambda eval is synchronous on
		// the main goroutine and never yields to the SDK scheduler).
		bctx.Done().Receive(bctx, nil)
		closer()
	})
	return ch
}
