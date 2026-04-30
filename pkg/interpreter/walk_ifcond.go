package interpreter

import (
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// walkIfCond evaluates the condition lambda inline (zero history events,
// INTRP-03) and walks the appropriate Then/Else branch. Truthiness uses
// starlark.Value.Truth() — Starlark's standard rules apply (False, 0,
// empty string/list/dict are falsy; everything else truthy). Static
// validation in Phase 4 may require condition lambdas to return a bool.
func (i *interpreter) walkIfCond(ctx workflow.Context, n *dag.IfCond) error {
	val, err := i.evalLambda(ctx, n.LambdaID)
	if err != nil {
		return err
	}
	cond := bool(val.Truth())

	branch := n.Then
	if !cond {
		branch = n.Else
	}
	if len(branch) == 0 {
		return nil
	}
	if err := i.walkBody(ctx, branch); err != nil {
		return fmt.Errorf("if_cond at %s: %w", n.Pos, err)
	}
	return nil
}
