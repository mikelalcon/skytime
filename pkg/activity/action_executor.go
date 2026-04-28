package activity

import (
	"context"
	"fmt"
	"reflect"

	"go.temporal.io/sdk/temporal"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// errTypeKwargsDecode is the *temporal.ApplicationError Type returned when
// kwargs reflective decode fails inside runAction. Non-retryable: the parser
// is supposed to validate kwargs at parse time, so a decode failure at the
// activity is a contract bug, not a transient condition.
const errTypeKwargsDecode = "KwargsDecode"

// runAction executes one action from the batch. It performs:
//
//  1. Dispatch lookup — defense in depth on top of validateBatch.
//  2. Credential resolve via the per-worker cache. bypass = attemptFn(ctx) > 1
//     mirrors D2-11. Resolve errors are CLASSIFIED here (D2-12).
//  3. Per-action timeout via context.WithTimeout (D2-15) when the op
//     declares OperationSpec.DefaultTimeout > 0.
//  4. Kwargs decode from *starlark.Dict to the op's typed struct via
//     extension.DecodeKwargsFromDict (the runtime-path decoder added in
//     Plan 02-01 Task 4). KwargsType==nil means "op accepts any-typed args".
//  5. The actual OperationFunc call.
//
// Op errors (step 5) are returned UNWRAPPED — the ExecuteBatch caller
// classifies them by inspecting whether they're already a
// *temporal.ApplicationError (passthrough) or a generic error (treated
// as retryable — see isRetryable in execute_batch.go).
func (a *Activity) runAction(ctx context.Context, idx int, ref *dag.ActionRef) (dag.OperationOutput, error) {
	spec, ok := a.dispatch[ref.Kind_]
	if !ok {
		// Should be unreachable after validateBatch, but defense in depth —
		// runAction is sometimes invoked via codepaths that don't go through
		// the batch-level gate (e.g., direct unit test exercise).
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("unknown operation %q at action index %d", ref.Kind_, idx),
			errTypeUnknownOperation,
			nil,
		)
	}

	// Resolve credential (cache + retry-bypass). bypass=true if the
	// current attempt is a retry — D2-11. ExecuteBatch's batch-level loop
	// invalidates entries before we enter runAction, so bypass=true here
	// just skips the cache READ — the cache WRITE still warms the entry
	// for any subsequent action in the same retried batch (idempotent
	// invalidate-then-warm sequence).
	bypass := a.attemptFn(ctx) > 1
	cred, err := a.cache.resolve(ctx, ref.CredentialID, bypass)
	if err != nil {
		return nil, classifyResolveError(err) // D2-12
	}

	// Per-action timeout (D2-15). DefaultTimeout==0 means "no per-action
	// ceiling — only the activity-level StartToCloseTimeout applies."
	callCtx := ctx
	var cancel context.CancelFunc
	if spec.DefaultTimeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, spec.DefaultTimeout)
		defer cancel()
	}

	// Decode kwargs into the op's typed struct via extension.DecodeKwargsFromDict
	// (added in Plan 02-01 Task 4). KwargsType==nil → no decode, op receives nil.
	args, err := decodeActionRefKwargs(ref, spec)
	if err != nil {
		// Kwargs decode failure is a contract bug — the parser SHOULD
		// have validated kwargs at parse time. Non-retryable.
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("action %d kwargs decode: %v", idx, err),
			errTypeKwargsDecode,
			err,
		)
	}

	// Call the operation. Errors from spec.Func are returned UNWRAPPED;
	// the ExecuteBatch caller classifies them via isRetryable.
	return spec.Func(callCtx, args, cred)
}

// decodeActionRefKwargs decodes ref.Kwargs (a *starlark.Dict) into a new
// instance of spec.KwargsType using extension.DecodeKwargsFromDict (added in
// Plan 02-01 Task 4). Returns the decoded struct as `any` so OperationFunc
// can type-assert (`args.(MyArgs)`).
//
// If spec.KwargsType is nil (test fixtures with no schema), returns nil — the
// op is expected to accept any-typed args.
//
// Why DecodeKwargsFromDict and not UnpackOperationKwargs? UnpackOperationKwargs
// is the parse-time entry point — it takes []starlark.Tuple and a syntax.Position
// for parse-error attribution. By the time the activity runs, ref.Kwargs is a
// frozen *starlark.Dict and there's no syntax.Position to thread.
// DecodeKwargsFromDict is the runtime-path companion that owns the
// Dict→Tuple conversion + a zero syntax.Position; runAction wraps the
// resulting error with action-index attribution above this comment.
func decodeActionRefKwargs(ref *dag.ActionRef, spec extension.OperationSpec) (any, error) {
	if spec.KwargsType == nil {
		return nil, nil
	}
	args := reflect.New(spec.KwargsType).Interface()
	if err := extension.DecodeKwargsFromDict(spec.Name, ref.Kwargs, args); err != nil {
		return nil, err
	}
	// Return a value (not a pointer) for ergonomics — the op's args
	// type-assertion uses `args.(MyArgs)`, not `*MyArgs`.
	return reflect.ValueOf(args).Elem().Interface(), nil
}
