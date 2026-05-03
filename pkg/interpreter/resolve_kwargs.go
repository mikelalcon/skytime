package interpreter

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// resolveKwargs walks ref.Kwargs and replaces every *dag.StarlarkLambda
// value with the result of evalLambda. Returns a NEW frozen
// *starlark.Dict suitable for passing as the activity's per-action
// input.
//
// Determinism (RESEARCH §Pattern 4):
//
//	*starlark.Dict.Items() returns entries in insertion order — NOT
//	randomized like Go's native `range map`. workflowcheck does NOT
//	flag this iteration because it is not Go map iteration. We
//	deliberately do NOT sort keys here — sorting would diverge from
//	the user's authored kwarg order and yield identical (but pointlessly
//	reordered) activity input. Future maintainers: do not "add a sort"
//	thinking it's needed for determinism — Items() is already
//	deterministic by spec.
//
//	The synthesized lambdas always return strings (D4.1-04 unconditional
//	str() wrap); the type assertion below is defensive against direct
//	hand-built ActionRefs that smuggle non-string lambda returns.
//
// Cancellation:
//
//	Each evalLambda call honors the workflow.Context.Done() cancellation
//	watchdog (D3-21) via the existing makeCancelChannel path. A
//	long-running kwarg lambda is interruptible.
//
// Fast path:
//
//	When ref.Kwargs contains zero *StarlarkLambda values, the original
//	(un-cloned) dict is returned. The caller treats this as "no work
//	needed" — backward-compatible with Phase 1/2/3 static action paths.
//
// Returns (nil, nil) when ref or ref.Kwargs is nil.
func (i *interpreter) resolveKwargs(ctx workflow.Context, ref *dag.ActionRef) (*starlark.Dict, error) {
	if ref == nil || ref.Kwargs == nil {
		return nil, nil
	}
	// First pass: scan for any *StarlarkLambda value. If none, return
	// the original frozen dict unchanged. Avoids one allocation per
	// static action.
	hasLambda := false
	for _, item := range ref.Kwargs.Items() { // deterministic — see Determinism note above
		if _, ok := dag.UnwrapStarlarkLambda(item[1]); ok {
			hasLambda = true
			break
		}
	}
	if !hasLambda {
		return ref.Kwargs, nil
	}

	// Second pass: build a new dict, evaluating lambdas in insertion
	// order and copying non-lambda values verbatim.
	out := starlark.NewDict(ref.Kwargs.Len())
	for _, item := range ref.Kwargs.Items() { // deterministic — see Determinism note above
		key, value := item[0], item[1]
		if captured, isLambda := dag.UnwrapStarlarkLambda(value); isLambda {
			resolved, err := i.evalLambda(ctx, captured.ID)
			if err != nil {
				return nil, fmt.Errorf("resolveKwargs: kwarg %q: %w", kwargKeyString(key), err)
			}
			sv, ok := resolved.(starlark.String)
			if !ok {
				return nil, fmt.Errorf("resolveKwargs: kwarg %q: lambda returned %s, expected string",
					kwargKeyString(key), resolved.Type())
			}
			value = sv
		}
		if err := out.SetKey(key, value); err != nil {
			return nil, fmt.Errorf("resolveKwargs: kwarg %q: %w", kwargKeyString(key), err)
		}
	}
	out.Freeze()
	return out, nil
}

// kwargKeyString returns the Go string for a starlark.String kwarg key,
// or the value's String() representation as a fallback. Kwargs in
// practice are always string-keyed (D-11 schema enforcement), so the
// fallback exists only for defensive error messaging.
func kwargKeyString(key starlark.Value) string {
	if ks, ok := key.(starlark.String); ok {
		return string(ks)
	}
	return key.String()
}
