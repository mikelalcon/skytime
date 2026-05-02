package activity

import (
	"context"
	"errors"
	"fmt"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// ExecuteBatch is the single Temporal activity entry point. Phase 3's
// worker bootstrap registers it via:
//
//	impl, _ := activity.New(dispatch, handler)
//	w.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})
//
// Per-action result semantics (D2-13/D2-14):
//   - Retryable failure mid-batch: short-circuit, return (nil, err).
//     Temporal retries the WHOLE batch. Safe because Policy D
//     (D2-05/D2-06) guarantees the batch is idempotent.
//   - Non-retryable failure mid-batch: return the FULL []ActionResult
//     containing OkResult for prior successes, NonRetryableErrResult at
//     the failing index, SkippedResult{Reason: ...} for actions after.
//     Returned error is NIL — Temporal does NOT retry; the interpreter
//     consumes the result list and decides workflow-level outcome.
//
// Cancellation cooperation (D2-16): ctx.Err() is checked at the top of every
// per-action iteration. If cancelled, the loop emits SkippedResult
// placeholders for the unrun indexes and returns (results, nil) — graceful
// stop is not an error per the cancellation contract used in
// TestExecuteBatch_CancellationStopsBetweenActions.
//
// ACT-01: this is the single Temporal activity that dispatches all
// extension I/O — extensions never register their own activities.
func (a *Activity) ExecuteBatch(ctx context.Context, batch []*dag.ActionRef) ([]dag.ActionResult, error) {
	// Defense in depth: parser linter (D2-05/D2-07) rejects bad batches at
	// parse time; this re-checks at runtime so a parser bug or hand-built
	// batch can't sneak through.
	if err := a.validateBatch(batch); err != nil {
		return nil, err
	}

	// Cache bypass on retry (D2-11): drop entries for every cred ID in
	// this batch BEFORE the per-action loop, so each action sees a fresh
	// resolve through the bypass-aware path. attemptFn is the unexported
	// seam — production reads activity.GetInfo(ctx).Attempt; tests can
	// inject a stub via withAttemptFunc.
	if a.attemptFn(ctx) > 1 {
		seenIDs := make(map[string]struct{}, len(batch))
		for _, ref := range batch {
			if ref.CredentialID != "" {
				seenIDs[ref.CredentialID] = struct{}{}
			}
		}
		for id := range seenIDs {
			a.cache.invalidate(id)
		}
		// Best-effort retry-attempt log. activity.GetLogger requires a real
		// activity context; outside TestActivityEnvironment + unit-test
		// invocations the call panics. Wrap in a recover so unit-test
		// invocations of ExecuteBatch (no activity wiring) don't blow up.
		func() {
			defer func() { _ = recover() }()
			activity.GetLogger(ctx).Info("retry attempt — credential cache invalidated",
				"attempt", a.attemptFn(ctx),
				"credential_ids", len(seenIDs),
			)
		}()
	}

	results := make([]dag.ActionResult, 0, len(batch))
	for idx, ref := range batch {
		// Cooperative cancellation check: if the workflow / test cancelled
		// the activity context before this action started, emit Skipped
		// placeholders for this and remaining indexes and return without
		// surfacing an error (cancellation is graceful, not a failure).
		if err := ctx.Err(); err != nil {
			for j := idx; j < len(batch); j++ {
				results = append(results, dag.SkippedResult{
					Idx:    j,
					Reason: "activity cancelled",
				})
			}
			return results, nil
		}

		out, err := a.runAction(ctx, idx, ref)
		if err != nil {
			if isRetryable(err) {
				// D2-13: short-circuit, return error to Temporal.
				return nil, err
			}
			// D2-14: non-retryable, return all results so far +
			// NonRetryableErrResult at the failing index + SkippedResult
			// placeholders for actions after.
			results = append(results, dag.NonRetryableErrResult{Idx: idx, Err: err})
			for j := idx + 1; j < len(batch); j++ {
				results = append(results, dag.SkippedResult{
					Idx:    j,
					Reason: fmt.Sprintf("action %d failed non-retryably", idx),
				})
			}
			return results, nil // nil error → Temporal does NOT retry.
		}

		results = append(results, dag.OkResult{Idx: idx, Output: out})

		// Heartbeat between actions (D2-16). 1-indexed for human readability:
		// "action 1 of 3 done", "action 2 of 3 done", "action 3 of 3 done".
		// realHeartbeatEmitter routes to activity.RecordHeartbeat which
		// no-ops outside an activity context (best-effort fire-and-forget),
		// so unit-test invocations of ExecuteBatch are safe.
		a.emitter.emit(ctx, BatchProgress{Action: idx + 1, Total: len(batch)})
	}
	return results, nil
}

// isRetryable inspects an error returned from runAction. It's retryable
// unless wrapped via temporal.NewNonRetryableApplicationError. Plain
// errors (errors.New("...")) are treated as retryable by default —
// transient-failure assumption.
//
// Decision reference: D2-13 / D2-14. ExecuteBatch's mid-batch handler reads
// this and either short-circuits (retryable → return error to Temporal) or
// records a NonRetryableErrResult and returns (results, nil).
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return !appErr.NonRetryable()
	}

	// Fix A (quick 260502-onc): extensions outside the temporal
	// firewall (e.g., pkg/extension/builtin/http) cannot construct a
	// *temporal.ApplicationError directly. The extension.ErrNonRetryable
	// sentinel is the contract: any error wrapping it via fmt.Errorf
	// %w is treated as non-retryable. Plain wrapped errors continue
	// to default retryable per the transient-failure assumption.
	if errors.Is(err, extension.ErrNonRetryable) {
		return false
	}

	// Default: retryable.
	return true
}
