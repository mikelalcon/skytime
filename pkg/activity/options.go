package activity

import (
	"errors"
	"fmt"
	"time"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// DefaultCredentialCacheTTL is the default lifetime for cached credentials
// per D2-10. Five minutes balances "fresh enough for token rotation under
// retry (D2-11)" with "few enough handler calls under steady-state load".
// Override via WithCredentialCacheTTL at worker registration.
const DefaultCredentialCacheTTL = 5 * time.Minute

// DefaultMaxBlockSize is the activity-side defense-in-depth cap on actions
// per ExecuteBatch invocation, per D2-07. The parser enforces the same cap
// at parse time; the activity re-checks at runtime so a misconfigured worker
// + tampered DAG cannot blow past the limit silently.
const DefaultMaxBlockSize = 50

// Activity is the constructed-once-per-worker holder for the generic
// ExecuteBatch (added in 02-03). It closes over the dispatch table, the
// credential handler, the cache, and the testability seams (attemptFn,
// emitter). Method value `act.ExecuteBatch` is what gets registered with
// the Temporal worker.
//
// D2-18: package name is `activity` despite the import-path collision with
// go.temporal.io/sdk/activity. Consumers disambiguate via aliased imports
// (e.g., `sdkactivity "go.temporal.io/sdk/activity"`) when both are reachable
// from the same file. Phase 3's worker bootstrap is the only place this
// matters.
type Activity struct {
	dispatch     OperationDispatch
	handler      extension.CredentialHandler
	cache        *credentialCache
	cacheTTL     time.Duration
	attemptFn    attemptFunc
	emitter      heartbeatEmitter
	maxBlockSize int
}

// Option mutates an Activity at construction time. Returning an error lets
// option application fail fast on bad input (e.g., negative TTL, zero block
// size). Matches the per-parser Option signature in pkg/parser for symmetry.
type Option func(*Activity) error

// WithCredentialCacheTTL overrides the default 5-minute cache TTL (D2-10).
// Zero means "never cache" — every resolve calls the handler. Negative is
// rejected at construction time as a config bug.
func WithCredentialCacheTTL(d time.Duration) Option {
	return func(a *Activity) error {
		if d < 0 {
			return fmt.Errorf("invalid credential cache TTL %v: must be >= 0", d)
		}
		a.cacheTTL = d
		return nil
	}
}

// WithMaxBlockSize overrides the activity-side defense-in-depth block-size
// cap (D2-07). Must be >= 1; values < 1 are rejected at construction time.
//
// Coordinate any change with parser.WithMaxBlockSize: tightening the cap
// only restricts what consultant `.star` files can declare; loosening it
// past the parser cap would let parse-time blocks pass through and fail
// later at the activity. The defaults are aligned at 50.
func WithMaxBlockSize(n int) Option {
	return func(a *Activity) error {
		if n < 1 {
			return fmt.Errorf("invalid activity max block size %d: must be >= 1", n)
		}
		a.maxBlockSize = n
		return nil
	}
}

// withAttemptFunc is the UNEXPORTED seam used by 02-03 tests to inject a
// stub Attempt extractor. Production code uses defaultAttemptFunc
// unconditionally; only same-package tests touch this option.
//
// Rationale: TestActivityEnvironment.ExecuteActivity hardcodes Attempt=1
// and exposes no SetAttempt method (verified at
// sdk-go/v1.42.0/internal/internal_workflow_testsuite.go:735), so testing
// the D2-11 cache-bypass path requires bypassing activity.GetInfo entirely.
func withAttemptFunc(fn attemptFunc) Option {
	return func(a *Activity) error {
		if fn == nil {
			return errors.New("attemptFunc must not be nil")
		}
		a.attemptFn = fn
		return nil
	}
}

// withHeartbeatEmitter is the UNEXPORTED seam used by 02-03 tests to capture
// emitted BatchProgress without spinning up TestActivityEnvironment. The
// production path uses realHeartbeatEmitter which delegates to
// activity.RecordHeartbeat.
func withHeartbeatEmitter(e heartbeatEmitter) Option {
	return func(a *Activity) error {
		if e == nil {
			return errors.New("heartbeatEmitter must not be nil")
		}
		a.emitter = e
		return nil
	}
}

// New constructs an Activity ready for worker registration. Defaults come
// from D2-10 (cache TTL = 5 min) and D2-07 (max block size = 50); the
// production attemptFn and emitter close over the Temporal SDK directly.
//
// dispatch and handler are required — both must be non-nil. Option errors
// (e.g., negative TTL) are wrapped with an "activity.New: " prefix so
// callers can grep failures cleanly.
//
// Phase 3 wires this into:
//
//	impl, err := activity.New(dispatch, handler)
//	if err != nil { return err }
//	w.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})
//
// (ExecuteBatch is added in 02-03; this plan stops at the constructor.)
func New(dispatch OperationDispatch, handler extension.CredentialHandler, opts ...Option) (*Activity, error) {
	if dispatch == nil {
		return nil, errors.New("activity.New: dispatch must not be nil")
	}
	if handler == nil {
		return nil, errors.New("activity.New: handler must not be nil")
	}
	a := &Activity{
		dispatch:     dispatch,
		handler:      handler,
		cacheTTL:     DefaultCredentialCacheTTL,
		attemptFn:    defaultAttemptFunc,
		emitter:      realHeartbeatEmitter{},
		maxBlockSize: DefaultMaxBlockSize,
	}
	for _, opt := range opts {
		if err := opt(a); err != nil {
			return nil, fmt.Errorf("activity.New: %w", err)
		}
	}
	// Build the cache AFTER options so WithCredentialCacheTTL takes effect.
	a.cache = newCredentialCache(handler, a.cacheTTL)
	return a, nil
}
