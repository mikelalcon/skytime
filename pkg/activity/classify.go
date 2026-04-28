package activity

import (
	"errors"

	"go.temporal.io/sdk/temporal"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// Error type strings used in the *temporal.ApplicationError emitted by
// classifyResolveError. Exported as constants (lowercase package-private,
// uppercase value) so tests can match exactly without string-fragment
// brittleness.
const (
	// errTypeUnknownCredential marks a non-retryable failure where the
	// credential ID is not registered with the handler — a configuration
	// bug; retrying won't help. D2-12.
	errTypeUnknownCredential = "UnknownCredential"

	// errTypeCredentialResolveFailed marks a retryable failure where the
	// handler reached its backend but the call failed transiently
	// (network, deadline, partial outage). D2-12.
	errTypeCredentialResolveFailed = "CredentialResolveFailed"
)

// classifyResolveError maps a CredentialHandler.Resolve error to a Temporal
// ApplicationError per D2-12:
//
//   - errors.Is(err, extension.ErrUnknownCredential) → NonRetryable
//     (configuration bug; retrying won't help)
//   - any other non-nil error → Retryable
//   - nil → nil (passthrough)
//
// Use temporal.NewNonRetryableApplicationError for the non-retryable path so
// Temporal's RetryPolicy.NonRetryableErrorTypes can short-circuit cleanly.
// Inspection from a caller is via:
//
//	var appErr *temporal.ApplicationError
//	if errors.As(err, &appErr) { ... appErr.NonRetryable() ... }
//
// (RESEARCH §"Anti-Patterns" — the deprecated IsRetryableError path is
// documented as broken in v1.42; use NonRetryable() instead.)
func classifyResolveError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, extension.ErrUnknownCredential) {
		return temporal.NewNonRetryableApplicationError(
			err.Error(),
			errTypeUnknownCredential,
			err,
		)
	}
	return temporal.NewApplicationError(
		err.Error(),
		errTypeCredentialResolveFailed,
	)
}
