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
func (i *interpreter) walkScript(ctx workflow.Context, n *dag.Script) error {
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
