package bridge

import (
	"context"
	"fmt"
	"log/slog"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// DefaultMaxExecutionSteps is the D-22 default Starlark step budget per
// CallLambda invocation. Override with CallOptions.MaxExecutionSteps.
const DefaultMaxExecutionSteps uint64 = 10_000_000

// CallOptions configures one CallLambda invocation. Defaults are tuned for
// D-22 (MaxExecutionSteps) and D-21 (PrintSink). All fields are optional —
// CallOptions{} is a valid argument.
type CallOptions struct {
	// MaxExecutionSteps caps the Starlark step budget for this single call.
	// Zero means "use DefaultMaxExecutionSteps" (10_000_000 per D-22). Any
	// positive value is taken verbatim.
	MaxExecutionSteps uint64

	// PrintSink receives lambda print() output. When nil, output routes to
	// Logger (or slog.Default() if Logger is also nil) at Info level.
	// Phase 3 wires this to workflow.GetLogger.
	PrintSink func(ctx context.Context, msg string)

	// Logger is the slog destination when PrintSink is nil. Falls back to
	// slog.Default(). D-06: library accepts *slog.Logger.
	Logger *slog.Logger

	// Cancel is a channel that signals "interrupt this lambda." When it
	// fires, a watchdog goroutine calls thread.Cancel and CallLambda
	// returns an error from the Starlark runtime. Nil-safe — when nil, no
	// watchdog runs. Phase 3 wires this from workflow.Context.Done().
	Cancel <-chan struct{}
}

// CallLambda evaluates a captured lambda with the given Go state map. It
// always allocates a FRESH *starlark.Thread (Pitfall #1 — never reused
// across calls), sets MaxExecutionSteps (D-22), routes Print (D-21), and
// honors an optional Cancel channel via a watchdog goroutine.
//
// Phase 1 is testable standalone with a hand-built CapturedLambda + state
// map. Phase 3's interpreter wraps this with workflow.GetLogger as the sink
// and workflow.Context.Done() as the Cancel source. The Phase 3 wrapping
// uses workflow.Go for the watchdog (this Phase 1 implementation uses a
// native goroutine, which is fine because CallLambda runs inside the
// activity, not inside the workflow goroutine).
//
// The returned starlark.Value is the lambda's result. Phase 3's interpreter
// converts it back to Go via FromStarlarkValue when it needs to update
// workflow state.
func CallLambda(ctx context.Context, captured *dag.CapturedLambda, state map[string]any, opts CallOptions) (starlark.Value, error) {
	// Apply defaults — D-22 step budget, D-21 print routing.
	if opts.MaxExecutionSteps == 0 {
		opts.MaxExecutionSteps = DefaultMaxExecutionSteps
	}
	if opts.PrintSink == nil {
		logger := opts.Logger
		if logger == nil {
			logger = slog.Default()
		}
		opts.PrintSink = func(ctx context.Context, msg string) {
			logger.InfoContext(ctx, "starlark print", "msg", msg, "lambda_id", captured.ID)
		}
	}

	// Convert Go state to *starlarkstruct.Struct for dot access (DSL-09).
	stateStruct, err := ToStarlarkStruct(state)
	if err != nil {
		return nil, err
	}

	// FRESH thread per call (Pitfall #1). Naming the thread with the lambda
	// ID makes Starlark's stack traces self-locating during Phase 3
	// debugging.
	thread := &starlark.Thread{
		Name: "skytime-lambda:" + captured.ID,
		Print: func(_ *starlark.Thread, msg string) {
			opts.PrintSink(ctx, msg)
		},
	}
	thread.SetMaxExecutionSteps(opts.MaxExecutionSteps)

	// Watchdog: if Cancel fires, signal the Starlark thread to stop. The
	// done channel + defer close ensures the goroutine exits whether the
	// call returns normally or via cancellation. Phase 3 will replace this
	// goroutine with workflow.Go when wiring workflow.Context.Done().
	if opts.Cancel != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-opts.Cancel:
				thread.Cancel("cancelled by caller")
			case <-done:
			}
		}()
	}

	// Call the lambda with the state struct as the single positional arg.
	// Lambdas authored via the DSL are by convention `lambda ctx: ...`.
	val, err := starlark.Call(thread, captured.Fn, starlark.Tuple{stateStruct}, nil)
	if err != nil {
		// B6 (D4.1-08): preserve the inner-fail() callsite. starlark.Call
		// returns *starlark.EvalError on `fail("...")` from inside the
		// lambda body; its Error() method renders ONLY the bare reason
		// (e.g. "fail: oops") — the per-frame position lives in the
		// CallStack. We re-wrap here with the deepest user-source
		// frame's Position so callers (e.g. interpreter walkStep
		// dispatching action_fn / block_fn) see "<file>:<line>:<col>:
		// fail: oops" — pinning the exact line where the user wrote
		// fail() rather than just the lambda definition line or the
		// synthetic <builtin> position.
		//
		// CallStack layout (per go.starlark.net/starlark/eval.go:215):
		//   - The slice is "outermost first": CallStack[0] is the
		//     outermost frame, CallStack[len-1] is the innermost.
		//   - CallStack.At(i) returns CallStack[len-1-i] — i.e. At(0) is
		//     the innermost frame.
		//   - The innermost frame for fail("...") is the <builtin>
		//     frame for the fail builtin itself, NOT the user's
		//     callsite. Frames carrying a user .star file are one or
		//     more steps further out.
		//
		// We therefore walk the slice from innermost (len-1) toward
		// outermost (0), skipping frames whose filename is "<builtin>"
		// or empty (synthetic). The first user-source frame we hit is
		// the .star location we want to surface. If none is found we
		// fall through with the original error so we never lose
		// information.
		if ee, ok := err.(*starlark.EvalError); ok && len(ee.CallStack) > 0 {
			for i := len(ee.CallStack) - 1; i >= 0; i-- {
				fr := ee.CallStack[i]
				fname := fr.Pos.Filename()
				if fname == "" || fname == "<builtin>" {
					continue
				}
				return val, fmt.Errorf("%s: %w", fr.Pos, ee)
			}
		}
		return val, err
	}
	return val, nil
}
