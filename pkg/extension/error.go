package extension

import "errors"

// ErrNonRetryable is the sentinel extension OperationFuncs wrap when a
// failure should NOT be retried by Temporal — typically a permanent
// logical failure (4xx HTTP, contract violation, malformed input that
// somehow bypassed parse-time validation, etc.).
//
// Wrap pattern (mirrors ErrUnknownCredential — see pkg/extension/handler.go):
//
//	return nil, fmt.Errorf("HTTP %d %s %s: %s: %w",
//	    resp.StatusCode, method, url, snippet(body), extension.ErrNonRetryable)
//
// The activity layer (pkg/activity/execute_batch.go isRetryable) checks
// errors.Is(err, ErrNonRetryable) and surfaces the failure as a
// NonRetryable temporal.ApplicationError. Plain unwrapped errors from
// OperationFuncs continue to default to retryable per Phase 2's
// transient-failure assumption (D2-13).
//
// Why a sentinel and not a typed error: extensions live OUTSIDE the
// temporal-firewall (only pkg/{activity,interpreter,worker,cli} may
// import go.temporal.io/sdk/*), so they cannot construct a
// *temporal.ApplicationError directly. The sentinel + classify-side
// wrap is the established Phase 2 pattern (ErrUnknownCredential →
// classifyResolveError); ErrNonRetryable extends it to OperationFunc
// errors.
var ErrNonRetryable = errors.New("non-retryable")
