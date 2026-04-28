package activity

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.temporal.io/sdk/temporal"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	extensiontesting "github.com/mikelalcon/skytime/pkg/extension/testing"
)

// newValidationActivity builds an *Activity with a small dispatch map suitable
// for validation tests:
//
//	"ext.idem"     — Idempotent: true
//	"ext.idem2"    — Idempotent: true
//	"ext.nonidem"  — Idempotent: false
//	"ext.nonidem2" — Idempotent: false
//	"ext.no_idem"  — Idempotent: nil (defense-in-depth case)
func newValidationActivity(t *testing.T, opts ...Option) *Activity {
	t.Helper()
	dispatch := OperationDispatch{
		"ext.idem":     extension.OperationSpec{Name: "idem", Idempotent: extension.Ptr(true)},
		"ext.idem2":    extension.OperationSpec{Name: "idem2", Idempotent: extension.Ptr(true)},
		"ext.nonidem":  extension.OperationSpec{Name: "nonidem", Idempotent: extension.Ptr(false)},
		"ext.nonidem2": extension.OperationSpec{Name: "nonidem2", Idempotent: extension.Ptr(false)},
		"ext.no_idem":  extension.OperationSpec{Name: "no_idem", Idempotent: nil},
	}
	handler := &extensiontesting.FakeCredentialHandler{Creds: map[string]extension.Credential{}}
	a, err := New(dispatch, handler, opts...)
	require.NoError(t, err)
	return a
}

func mkValidationRef(kind string) *dag.ActionRef {
	return &dag.ActionRef{Kind_: kind, Kwargs: starlark.NewDict(0)}
}

// TestValidateBatch_EmptyBatch_NonRetryable — D2 defense in depth:
// validateBatch must reject nil and empty []*ActionRef.
func TestValidateBatch_EmptyBatch_NonRetryable(t *testing.T) {
	a := newValidationActivity(t)
	for _, batch := range [][]*dag.ActionRef{nil, {}} {
		err := a.validateBatch(batch)
		require.Error(t, err)
		var appErr *temporal.ApplicationError
		require.True(t, errors.As(err, &appErr), "expected *temporal.ApplicationError, got %T", err)
		require.True(t, appErr.NonRetryable(), "empty batch should be non-retryable")
		require.Equal(t, "EmptyBatch", appErr.Type())
	}
}

// TestValidateBatch_Oversized_NonRetryable — D2-07 default cap is 50; a 51-action
// batch must be rejected.
func TestValidateBatch_Oversized_NonRetryable(t *testing.T) {
	a := newValidationActivity(t)
	batch := make([]*dag.ActionRef, 51)
	for i := range batch {
		batch[i] = mkValidationRef("ext.idem")
	}
	err := a.validateBatch(batch)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable())
	require.Equal(t, "BatchTooLarge", appErr.Type())
	require.Contains(t, appErr.Error(), "batch size 51 exceeds maximum 50")
}

// TestValidateBatch_Oversized_Custom — WithMaxBlockSize(2) caps at 2; a 3-action
// batch must be rejected with the custom cap in the message.
func TestValidateBatch_Oversized_Custom(t *testing.T) {
	a := newValidationActivity(t, WithMaxBlockSize(2))
	batch := []*dag.ActionRef{
		mkValidationRef("ext.idem"),
		mkValidationRef("ext.idem"),
		mkValidationRef("ext.idem"),
	}
	err := a.validateBatch(batch)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable())
	require.Equal(t, "BatchTooLarge", appErr.Type())
	require.Contains(t, appErr.Error(), "batch size 3 exceeds maximum 2")
}

// TestValidateBatch_AtLimit — exactly maxBlockSize idempotent actions is OK.
func TestValidateBatch_AtLimit(t *testing.T) {
	a := newValidationActivity(t)
	batch := make([]*dag.ActionRef, 50)
	for i := range batch {
		batch[i] = mkValidationRef("ext.idem")
	}
	require.NoError(t, a.validateBatch(batch))
}

// TestValidateBatch_UnknownOp_NonRetryable — a Kind_ not present in the
// dispatch table is a configuration / parser bug. Defense in depth.
func TestValidateBatch_UnknownOp_NonRetryable(t *testing.T) {
	a := newValidationActivity(t)
	batch := []*dag.ActionRef{
		mkValidationRef("ext.idem"),
		mkValidationRef("missing.op"),
	}
	err := a.validateBatch(batch)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable())
	require.Equal(t, "UnknownOperation", appErr.Type())
	require.Contains(t, appErr.Error(), `unknown operation "missing.op"`)
}

// TestValidateBatch_MissingIdempotent_NonRetryable — Registry rejects nil
// Idempotent at registration (D-12), but defense in depth at the activity
// boundary catches a hand-built dispatch with a nil declaration.
func TestValidateBatch_MissingIdempotent_NonRetryable(t *testing.T) {
	a := newValidationActivity(t)
	batch := []*dag.ActionRef{mkValidationRef("ext.no_idem")}
	err := a.validateBatch(batch)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable())
	require.Equal(t, "MissingIdempotent", appErr.Type())
	require.Contains(t, appErr.Error(), `operation "ext.no_idem" has no Idempotent declaration`)
}

// TestValidateBatch_MixedIdempotency_NonRetryable — D2-05 Policy D. The parser
// linter is the primary gate; the activity re-checks defensively.
func TestValidateBatch_MixedIdempotency_NonRetryable(t *testing.T) {
	a := newValidationActivity(t)
	batch := []*dag.ActionRef{
		mkValidationRef("ext.idem"),
		mkValidationRef("ext.nonidem"),
	}
	err := a.validateBatch(batch)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable())
	require.Equal(t, "MixedIdempotency", appErr.Type())
	require.Contains(t, appErr.Error(), "batch mixes idempotent and non-idempotent operations")
}

// TestValidateBatch_NonIdempotentMulti_NonRetryable — D2-06: non-idempotent
// blocks must be exactly one action.
func TestValidateBatch_NonIdempotentMulti_NonRetryable(t *testing.T) {
	a := newValidationActivity(t)
	batch := []*dag.ActionRef{
		mkValidationRef("ext.nonidem"),
		mkValidationRef("ext.nonidem2"),
	}
	err := a.validateBatch(batch)
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable())
	require.Equal(t, "MultiNonIdempotent", appErr.Type())
	require.Contains(t, appErr.Error(), "non-idempotent operation")
	require.Contains(t, appErr.Error(), "multiple actions")
}

// TestValidateBatch_AllIdempotent_OK — happy path: all-idempotent batch passes.
func TestValidateBatch_AllIdempotent_OK(t *testing.T) {
	a := newValidationActivity(t)
	batch := []*dag.ActionRef{
		mkValidationRef("ext.idem"),
		mkValidationRef("ext.idem2"),
		mkValidationRef("ext.idem"),
	}
	require.NoError(t, a.validateBatch(batch))
}

// TestValidateBatch_SingleNonIdempotent_OK — D2-06 explicitly: a single-action
// batch with a non-idempotent op is allowed (homogeneous batch of 1).
func TestValidateBatch_SingleNonIdempotent_OK(t *testing.T) {
	a := newValidationActivity(t)
	batch := []*dag.ActionRef{mkValidationRef("ext.nonidem")}
	require.NoError(t, a.validateBatch(batch))
}
