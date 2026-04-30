package interpreter

import (
	"context"
	"fmt"

	"go.starlark.net/starlark"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/bridge"
)

// evalLambda evaluates a captured lambda by ID against the current state.
// Pure CPU on the workflow goroutine — zero history events (INTRP-03).
//
// The cancellation channel is the bridge between workflow.Context.Done()
// and the bridge's native-goroutine watchdog (D3-21). Print routes through
// workflow.GetLogger (D3-22).
//
// Returns a non-retryable application error of type "LambdaNotFound" when
// the registered ID is missing — typically a registry-boot bug.
func (i *interpreter) evalLambda(ctx workflow.Context, lambdaID string) (starlark.Value, error) {
	captured, ok := i.parsed.Lambdas[lambdaID]
	if !ok {
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("lambda %s not found in flow %s@%s", lambdaID, i.flow.Name, i.contentHash),
			"LambdaNotFound", nil,
		)
	}

	cancelCh := makeCancelChannel(ctx)
	printSink := func(_ context.Context, msg string) {
		// D3-22: route Starlark print to workflow.GetLogger.
		workflow.GetLogger(ctx).Info("[skytime/print] "+msg, "lambda_id", captured.ID)
	}

	// Pass a stdlib context.Context that does NOT carry workflow.Context
	// (no-context-bleed invariant). context.Background() is fine — the
	// bridge only uses it for slog logging fallback when PrintSink is set
	// the bridge does not call the logger fallback path.
	val, err := bridge.CallLambda(context.Background(), captured, i.state.snapshot(), bridge.CallOptions{
		PrintSink: printSink,
		Cancel:    cancelCh,
		// Logger left zero — bridge falls back to slog.Default. Phase 3's
		// workflow-side logging happens via the PrintSink; bridge.Logger
		// is only used for non-print diagnostics.
	})
	if err != nil {
		// Wrap with lambda position for Starlark-callsite-aware error.
		return nil, fmt.Errorf("lambda %s @ %s: %w", lambdaID, captured.Pos, err)
	}
	return val, nil
}
