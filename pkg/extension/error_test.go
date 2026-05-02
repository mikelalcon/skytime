package extension

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrNonRetryable_Sentinel pins the message of the sentinel so any
// accidental rename is caught.
func TestErrNonRetryable_Sentinel(t *testing.T) {
	require.NotNil(t, ErrNonRetryable, "sentinel must be initialized")
	assert.Equal(t, "non-retryable", ErrNonRetryable.Error())
}

// TestErrNonRetryable_IsErrorsIsCompatible verifies that extensions that
// wrap ErrNonRetryable via fmt.Errorf("...: %w", ErrNonRetryable) produce
// errors that errors.Is can detect. This is the Quick 260502-onc Fix A
// retry-classification contract: the activity (pkg/activity/execute_batch.go
// isRetryable) checks errors.Is(err, ErrNonRetryable) to decide between
// NonRetryable (contract failure — no retry) and Retryable (transient).
func TestErrNonRetryable_IsErrorsIsCompatible(t *testing.T) {
	// fmt.Errorf("...: %w", ...) is the documented extension pattern.
	wrapped := fmt.Errorf("HTTP 404 GET /repos/x/y: not found: %w", ErrNonRetryable)
	require.Error(t, wrapped)
	assert.True(t, errors.Is(wrapped, ErrNonRetryable),
		"errors.Is must walk %%w wrappers — Fix A retry classification depends on this")

	// Doubly wrapped (e.g., extension wraps once, activity wraps for
	// log context): errors.Is still finds the sentinel.
	doubleWrapped := fmt.Errorf("dispatching action: %w", wrapped)
	assert.True(t, errors.Is(doubleWrapped, ErrNonRetryable),
		"errors.Is must walk multiple %%w layers")

	// Negative: a plain string-equal error is NOT considered the sentinel.
	plain := errors.New("non-retryable")
	assert.False(t, errors.Is(plain, ErrNonRetryable),
		"errors.Is must compare identity, not message — only fmt.Errorf %%w wraps qualify")
}
