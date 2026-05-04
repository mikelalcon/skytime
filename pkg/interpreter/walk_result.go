package interpreter

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
)

// bindResultToState assembles the per-key value lambdas of n into a
// Starlark dict (frozen for replay-determinism), converts the dict to
// its Go representation, and writes the result under outputAlias in
// workflow state.
//
// n.Keys provides source-insertion order — we iterate the slice, NOT
// `for k := range n.Values`. Go map iteration order is randomized;
// Temporal replay requires byte-equal command histories on a second
// execution; therefore the dict's SetKey order MUST be deterministic
// (D3-23 + Phase 04.2 RESEARCH §Pitfall 5).
//
// Emits a single `result_bound` slog event before returning (the
// outer walkIfCond's deferred step_complete{kind=if_cond} fires
// AFTER this returns — the renderer thus sees:
//
//	step_dispatch{kind=if_cond} →
//	branch{branch=then|else} →
//	(nested step events from leading body) →
//	result_bound{alias,keys,path} →
//	step_complete{kind=if_cond}
//
// INTRP-03: ZERO Temporal history events. evalLambda runs the per-key
// value lambda inline (CPU-only). bridge.FromStarlarkValue is a pure
// function. i.state.setOutput mutates the in-memory state map. No
// workflow.ExecuteActivity, no SideEffect, no Sleep.
func (i *interpreter) bindResultToState(ctx workflow.Context, outputAlias string, n *dag.Result) error {
	logger := workflow.GetLogger(ctx)

	out := starlark.NewDict(len(n.Keys))
	for _, k := range n.Keys {
		captured := n.Values[k]
		if captured == nil {
			// Defensive — parser guarantees Values[k] is set for every
			// k in Keys. A nil here means the parse-time invariant
			// broke; surface as a workflow error rather than a panic.
			return fmt.Errorf("result_bind: nil captured lambda for key %q at %s", k, n.Pos)
		}
		val, err := i.evalLambda(ctx, captured.ID)
		if err != nil {
			return fmt.Errorf("result_bind: evaluating value for key %q at %s: %w", k, n.Pos, err)
		}
		if err := out.SetKey(starlark.String(k), val); err != nil {
			return fmt.Errorf("result_bind: SetKey for key %q at %s: %w", k, n.Pos, err)
		}
	}
	out.Freeze() // replay-deterministic; subsequent reads return identical bytes

	goVal, err := bridge.FromStarlarkValue(out)
	if err != nil {
		return fmt.Errorf("result_bind: starlark→go conversion at %s: %w", n.Pos, err)
	}
	i.state.setOutput(outputAlias, goVal)

	logger.Info("skytime",
		"event", "result_bound",
		"alias", outputAlias,
		"keys", n.Keys,
		"path", i.currentPath(),
	)
	return nil
}
