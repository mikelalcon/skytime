package interpreter

import (
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
)

// walkScript evaluates the script's lambda inline (zero history events,
// INTRP-03), converts the Starlark result back to Go via
// bridge.FromStarlarkValue, and stores it in workflow state under
// OutputAlias. Subsequent walkers see the new state key on their next
// state.snapshot() call.
//
// Quick 260502-guu Fix B: emits step_dispatch + step_complete via
// workflow.GetLogger(ctx). Named-return + defer guarantees the
// completion event fires on every return path.
func (i *interpreter) walkScript(ctx workflow.Context, n *dag.Script) (err error) {
	logger := workflow.GetLogger(ctx)
	start := workflow.Now(ctx)
	path := i.currentPath()
	idx, total := i.stepIdx, i.stepTot
	label := n.OutputAlias
	if label == "" {
		label = n.ID
	}

	logger.Info("skytime",
		"event", "step_dispatch",
		"kind", "script",
		"label", label,
		"idx", idx, "total", total, "path", path,
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
			"kind", "script",
			"label", label,
			"status", status,
			"duration_ms", workflow.Now(ctx).Sub(start).Milliseconds(),
			"idx", idx, "total", total, "path", path,
			"summary", summary,
		)
	}()

	val, err := i.evalLambda(ctx, n.LambdaID)
	if err != nil {
		return err
	}
	goVal, err := bridge.FromStarlarkValue(val)
	if err != nil {
		return fmt.Errorf("script %s at %s: convert lambda result: %w", n.ID, n.Pos, err)
	}
	i.state.setOutput(n.OutputAlias, goVal)
	return nil
}
