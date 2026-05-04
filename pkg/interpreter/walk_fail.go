package interpreter

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// raiseFail resolves n's message (literal or lambda-evaluated) and
// returns a non-retryable application error carrying the source
// position. The caller (walkIfCond) returns this; the named-return +
// defer in walkIfCond emits step_complete with status=err,
// summary=<the wrapped error string> for renderer visibility (the
// existing defer is what renders the red ✗ in the static + live
// renderers — quick 260502-onc pattern).
//
// Message resolution semantics (mirrors stepDisplayLabel in
// walk_step.go for Step.NameFn):
//   - n.MessageFn nil: use literal n.Message verbatim
//   - n.MessageFn non-nil + eval succeeds + result is starlark.String:
//     use the resolved string
//   - n.MessageFn non-nil + eval fails OR result wrong type: fall
//     back to literal n.Message (preserves the raise; only the
//     display attribute degrades — display safety must not crash
//     the workflow; the failure semantics still raise)
//
// INTRP-03: ZERO Temporal history events. evalLambda is CPU-only;
// temporal.NewNonRetryableApplicationError just constructs an error
// value (no SDK side-effect).
func (i *interpreter) raiseFail(ctx workflow.Context, n *dag.Fail) error {
	msg := n.Message
	if n.MessageFn != nil {
		val, evalErr := i.evalLambda(ctx, n.MessageFn.ID)
		if evalErr == nil {
			if s, ok := val.(starlark.String); ok {
				msg = string(s)
			}
			// wrong-type fallthrough: keep literal n.Message
		}
		// eval-error fallthrough: keep literal n.Message
	}
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("fail %s: %s", n.Pos, msg),
		"FailNode",
		nil,
	)
}
