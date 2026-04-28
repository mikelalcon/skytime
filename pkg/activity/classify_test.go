package activity

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// TestClassifyResolveError_TableDriven covers D2-12: errors wrapping
// ErrUnknownCredential become non-retryable; everything else (transient
// backend, context cancellation, deadline exceeded) becomes retryable.
// nil input passes through as nil (callers should rely on this).
func TestClassifyResolveError_TableDriven(t *testing.T) {
	cases := []struct {
		name             string
		input            error
		wantNil          bool
		wantNonRetry     bool
		wantTypeContains string
	}{
		{
			name:    "nil_passthrough",
			input:   nil,
			wantNil: true,
		},
		{
			name:             "unknown_credential_direct",
			input:            extension.ErrUnknownCredential,
			wantNonRetry:     true,
			wantTypeContains: "UnknownCredential",
		},
		{
			name:             "unknown_credential_wrapped",
			input:            fmt.Errorf("%w: id=missing", extension.ErrUnknownCredential),
			wantNonRetry:     true,
			wantTypeContains: "UnknownCredential",
		},
		{
			name:             "unknown_credential_double_wrapped",
			input:            fmt.Errorf("resolve failed: %w", fmt.Errorf("%w: id=missing", extension.ErrUnknownCredential)),
			wantNonRetry:     true,
			wantTypeContains: "UnknownCredential",
		},
		{
			name:             "transient_backend",
			input:            errors.New("backend timeout"),
			wantNonRetry:     false,
			wantTypeContains: "CredentialResolveFailed",
		},
		{
			name:             "context_canceled",
			input:            context.Canceled,
			wantNonRetry:     false,
			wantTypeContains: "CredentialResolveFailed",
		},
		{
			name:             "context_deadline_exceeded",
			input:            context.DeadlineExceeded,
			wantNonRetry:     false,
			wantTypeContains: "CredentialResolveFailed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyResolveError(tc.input)
			if tc.wantNil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			var appErr *temporal.ApplicationError
			require.True(t, errors.As(got, &appErr), "result must be *temporal.ApplicationError")
			require.Equal(t, tc.wantNonRetry, appErr.NonRetryable(),
				"%s: NonRetryable mismatch (got %v)", tc.name, appErr.NonRetryable())
			require.Contains(t, appErr.Type(), tc.wantTypeContains,
				"%s: error type %q should contain %q", tc.name, appErr.Type(), tc.wantTypeContains)
		})
	}
}

// TestClassifyResolveError_UnknownCredentialIsNonRetryable is the focused
// assertion the plan calls out separately — duplicates a row of the table
// test for readability when triaging failures from CI.
func TestClassifyResolveError_UnknownCredentialIsNonRetryable(t *testing.T) {
	err := fmt.Errorf("%w: %s", extension.ErrUnknownCredential, "missing")
	got := classifyResolveError(err)
	require.NotNil(t, got)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(got, &appErr))
	require.True(t, appErr.NonRetryable())
}

// TestClassifyResolveError_OtherErrorsAreRetryable mirrors the focused
// assertion for the retryable path.
func TestClassifyResolveError_OtherErrorsAreRetryable(t *testing.T) {
	err := errors.New("transient backend down")
	got := classifyResolveError(err)
	require.NotNil(t, got)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(got, &appErr))
	require.False(t, appErr.NonRetryable())
}
