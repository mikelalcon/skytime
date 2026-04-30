package interpreter

import (
	"errors"
	"fmt"

	"go.starlark.net/starlark"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
)

// defaultMaxConcurrency is D3-13's fallback fan-out cap. for_each_parallel
// without an explicit max_concurrency kwarg uses this value.
const defaultMaxConcurrency = 10

// walkForEach fans out fe.Steps across the resolved items list, bounded by
// fe.MaxConcurrency (default 10 per D3-13). Per-branch state isolation via
// state.scoped(item_var, item) (D3-15). Results aggregate in input order
// (D3-16); on a non-retryable error from any branch, sibling branches are
// cancelled (D3-14) and the original error is bubbled.
func (i *interpreter) walkForEach(ctx workflow.Context, fe *dag.ForEachParallel) error {
	items, err := i.resolveForEachItems(ctx, fe)
	if err != nil {
		return err
	}
	n := len(items)
	if n == 0 {
		return nil
	}

	maxConc := fe.MaxConcurrency
	if maxConc <= 0 {
		maxConc = defaultMaxConcurrency
	}
	if maxConc > n {
		maxConc = n
	}

	// Cancellable child context — D3-14: cancel siblings on non-retryable.
	childCtx, cancel := workflow.WithCancel(ctx)
	defer cancel()

	// Semaphore via buffered workflow.Channel (deterministic; native chan
	// forbidden inside workflow code per workflowcheck).
	sem := workflow.NewBufferedChannel(childCtx, maxConc)
	// Per-branch completion channel — one send per branch.
	done := workflow.NewBufferedChannel(childCtx, n)

	// Result slot per index (D3-16: input-order regardless of completion order).
	branchErrs := make([]error, n)

	for idx := 0; idx < n; idx++ {
		idx := idx
		item := items[idx]

		// Acquire semaphore (deterministic blocking on the workflow channel).
		sem.Send(childCtx, struct{}{})

		workflow.Go(childCtx, func(branchCtx workflow.Context) {
			defer sem.Receive(branchCtx, nil) // release
			defer done.Send(branchCtx, idx)

			scopedState := i.state.scoped(fe.ItemVar, item)
			branchInterp := *i // shallow copy
			branchInterp.state = scopedState
			if berr := branchInterp.walkBody(branchCtx, fe.Steps); berr != nil {
				branchErrs[idx] = berr
				if isNonRetryable(berr) { // D3-14
					cancel()
				}
			}
		})
	}

	// Barrier on all branches.
	for completed := 0; completed < n; completed++ {
		var idx int
		done.Receive(ctx, &idx)
	}

	// Aggregate: first non-retryable wins; otherwise first non-nil retryable.
	if aerr := aggregateBranchErrors(branchErrs); aerr != nil {
		return fmt.Errorf("for_each_parallel at %s: %w", fe.Pos, aerr)
	}
	return nil
}

// resolveForEachItems returns the items list. Either ItemsLambdaID is set
// (lambda producer) or ItemsLiteral is set (parser-converted Go values).
// At most one is set (dag.ForEachParallel.Validate enforces); if both are
// nil, returns an empty slice (treated as zero-iteration by walkForEach).
func (i *interpreter) resolveForEachItems(ctx workflow.Context, fe *dag.ForEachParallel) ([]any, error) {
	if fe.ItemsLambdaID != "" {
		val, err := i.evalLambda(ctx, fe.ItemsLambdaID)
		if err != nil {
			return nil, err
		}
		return starlarkIterableToGo(val)
	}
	// ItemsLiteral case — pure data already.
	return fe.ItemsLiteral, nil
}

// starlarkIterableToGo converts a Starlark iterable (list/tuple) into []any.
// Used by resolveForEachItems when items= is produced by a lambda. Sealed:
// only handles things that satisfy starlark.Iterable; other values produce
// a typed error.
func starlarkIterableToGo(v starlark.Value) ([]any, error) {
	seq, ok := v.(starlark.Iterable)
	if !ok {
		return nil, fmt.Errorf("for_each_parallel.items lambda returned non-iterable %s", v.Type())
	}
	iter := seq.Iterate()
	defer iter.Done()
	var out []any
	var elem starlark.Value
	for iter.Next(&elem) {
		g, err := bridge.FromStarlarkValue(elem)
		if err != nil {
			return nil, fmt.Errorf("for_each_parallel.items: convert element: %w", err)
		}
		out = append(out, g)
	}
	return out, nil
}

// isNonRetryable reports whether err should trigger sibling-cancellation
// per D3-14. A *temporal.ApplicationError with NonRetryable() == true
// qualifies; canceled errors do not (they're a CONSEQUENCE of cancellation,
// not a CAUSE).
func isNonRetryable(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.NonRetryable()
	}
	return false
}

// aggregateBranchErrors picks the first non-retryable error if any;
// otherwise the first non-nil error (input order). Returns nil if all
// branches succeeded.
func aggregateBranchErrors(errs []error) error {
	for _, e := range errs {
		if e != nil && isNonRetryable(e) {
			return e
		}
	}
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
