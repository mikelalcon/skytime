package activity

import (
	"errors"
	"fmt"

	"go.temporal.io/sdk/temporal"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// Error type strings used by validateBatch's *temporal.ApplicationError
// returns. Exported as package-private constants so test files in the same
// package can match exactly without string-fragment brittleness.
const (
	errTypeEmptyBatch         = "EmptyBatch"
	errTypeBatchTooLarge      = "BatchTooLarge"
	errTypeUnknownOperation   = "UnknownOperation"
	errTypeMissingIdempotent  = "MissingIdempotent"
	errTypeMixedIdempotency   = "MixedIdempotency"
	errTypeMultiNonIdempotent = "MultiNonIdempotent"
)

// validateBatch is the activity-side defense-in-depth gate. The parser
// (02-01's lintMixedIdempotency + lintBlockSize) is the primary gate;
// this re-checks every invariant at the activity boundary so a
// hypothetical parser bug or a hand-built batch in tests can't sneak
// through and corrupt non-idempotent state via retry.
//
// Every failure path returns a NonRetryable temporal.ApplicationError —
// these are configuration/contract violations, never transient. Temporal
// will not retry; the caller (Phase 3 interpreter) sees a permanent
// failure and the workflow handles it per its RetryPolicy + on-error
// logic.
//
// Decisions: D2-05 (mixed-idempotency reject), D2-06 (single-action
// non-idempotent allowed), D2-07 (block-size cap default 50).
func (a *Activity) validateBatch(batch []*dag.ActionRef) error {
	if len(batch) == 0 {
		return temporal.NewNonRetryableApplicationError(
			"empty batch",
			errTypeEmptyBatch,
			errors.New("ExecuteBatch received an empty []*dag.ActionRef"),
		)
	}
	if len(batch) > a.maxBlockSize {
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("batch size %d exceeds maximum %d", len(batch), a.maxBlockSize),
			errTypeBatchTooLarge,
			nil,
		)
	}

	var seenIdempotent, seenNonIdempotent bool
	for _, ref := range batch {
		spec, ok := a.dispatch[ref.Kind_]
		if !ok {
			return temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("unknown operation %q (not in dispatch table)", ref.Kind_),
				errTypeUnknownOperation,
				nil,
			)
		}
		if spec.Idempotent == nil {
			return temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("operation %q has no Idempotent declaration (registry contract violation)", ref.Kind_),
				errTypeMissingIdempotent,
				nil,
			)
		}
		if *spec.Idempotent {
			seenIdempotent = true
		} else {
			seenNonIdempotent = true
		}
	}

	if seenIdempotent && seenNonIdempotent {
		return temporal.NewNonRetryableApplicationError(
			"batch mixes idempotent and non-idempotent operations (parser bug — D2-05 should reject at parse time)",
			errTypeMixedIdempotency,
			nil,
		)
	}
	if seenNonIdempotent && len(batch) > 1 {
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("batch contains non-idempotent operation but has multiple actions (%d) (parser bug — D2-06 should split at parse time)", len(batch)),
			errTypeMultiNonIdempotent,
			nil,
		)
	}
	return nil
}
