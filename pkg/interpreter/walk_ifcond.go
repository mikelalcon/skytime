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
//
// Quick 260502-guu Fix B: emits step_dispatch + branch + step_complete
// via workflow.GetLogger(ctx). Named-return + defer guarantees the
// completion event fires on every return path.
//
// Nested-path convention: when entering the then/else branch, the
// stepPath is set to "<parentIdx>a" (then) or "<parentIdx>b" (else)
// for the duration of the inner walkBody. Restored on exit.
func (i *interpreter) walkIfCond(ctx workflow.Context, n *dag.IfCond) (err error) {
	logger := workflow.GetLogger(ctx)
	start := workflow.Now(ctx)
	parentIdx := i.stepIdx
	parentTot := i.stepTot
	parentPath := i.currentPath()

	logger.Info("skytime",
		"event", "step_dispatch",
		"kind", "if_cond",
		"label", "cond",
		"idx", parentIdx, "total", parentTot, "path", parentPath,
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
			"kind", "if_cond",
			"label", "cond",
			"status", status,
			"duration_ms", workflow.Now(ctx).Sub(start).Milliseconds(),
			"idx", parentIdx, "total", parentTot, "path", parentPath,
			"summary", summary,
		)
	}()

	val, err := i.evalLambda(ctx, n.LambdaID)
	if err != nil {
		return err
	}
	cond := bool(val.Truth())

	branchName := "then"
	branch := n.Then
	branchSuffix := "a"
	if !cond {
		branchName = "else"
		branch = n.Else
		branchSuffix = "b"
	}

	logger.Info("skytime",
		"event", "branch",
		"idx", parentIdx,
		"path", parentPath,
		"branch", branchName,
	)

	if len(branch) == 0 {
		return nil
	}

	// Save and override stepPath for the nested walkBody — restored on
	// exit so siblings of this if_cond render at the right level.
	savedPath := i.stepPath
	i.stepPath = fmt.Sprintf("%d%s", parentIdx, branchSuffix)
	defer func() { i.stepPath = savedPath }()

	// EXPRESSION MODE (D4.2-15): when n.OutputAlias is set, the chosen
	// branch's last node is *dag.Result (binds value) or *dag.Fail
	// (raises NonRetryableErr) per plan 03's parse-time validator.
	// Walk the leading body normally then dispatch on the last node.
	// Procedural-mode (OutputAlias == "") falls through to the
	// existing walkBody call below — verbatim today's behavior.
	//
	// Defensive: if the validator regressed and the last node is
	// neither Result nor Fail, fall through to walkBody (the runtime
	// walks it as a procedural node). This keeps malformed DAGs from
	// panicking — they will instead behave like procedural-mode and
	// never bind the alias (a parse-time error should have stopped
	// them earlier).
	if n.OutputAlias != "" {
		last := branch[len(branch)-1]
		leading := branch[:len(branch)-1]
		if werr := i.walkBody(ctx, leading); werr != nil {
			return fmt.Errorf("if_cond at %s: %w", n.Pos, werr)
		}
		switch lastNode := last.(type) {
		case *dag.Result:
			if berr := i.bindResultToState(ctx, n.OutputAlias, lastNode); berr != nil {
				return fmt.Errorf("if_cond at %s: %w", n.Pos, berr)
			}
			return nil
		case *dag.Fail:
			return i.raiseFail(ctx, lastNode)
		}
		// Defensive fallthrough — parse-time validator should reject
		// non-Result/Fail terminators; walk the entire branch as procedural.
	}

	// PROCEDURAL MODE (existing — unchanged).
	if werr := i.walkBody(ctx, branch); werr != nil {
		return fmt.Errorf("if_cond at %s: %w", n.Pos, werr)
	}
	return nil
}
