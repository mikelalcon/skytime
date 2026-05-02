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

	if werr := i.walkBody(ctx, branch); werr != nil {
		return fmt.Errorf("if_cond at %s: %w", n.Pos, werr)
	}
	return nil
}
