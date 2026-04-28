package dag

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resultTestOutput is a test-local OperationOutput-implementing type used
// by OkResult-construction tests below.
type resultTestOutput struct {
	Value string
}

func (resultTestOutput) IsOperationOutput() {}

// TestActionResult_SealedSum verifies — at compile time AND at runtime —
// that each of the four kinds satisfies the dag.ActionResult interface and
// that ActionIndex() returns the Idx field.
//
// The compile-time `var _ ActionResult = ...{}` declarations in result.go
// already enforce the seal at build time; this test exercises the seal at
// runtime so a regression would fail the test suite, not just the compile.
func TestActionResult_SealedSum(t *testing.T) {
	cases := []struct {
		name   string
		result ActionResult
		want   int
	}{
		{"ok", OkResult{Idx: 0, Output: resultTestOutput{Value: "x"}}, 0},
		{"retryable", RetryableErrResult{Idx: 1, Err: errors.New("transient")}, 1},
		{"nonretryable", NonRetryableErrResult{Idx: 2, Err: errors.New("terminal")}, 2},
		{"skipped", SkippedResult{Idx: 3, Reason: "precondition"}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.result.ActionIndex())
		})
	}
}

// TestOkResult_HoldsOperationOutput verifies the OkResult/OperationOutput
// composition: an OkResult constructed with a typed Output round-trips
// through the type switch.
func TestOkResult_HoldsOperationOutput(t *testing.T) {
	out := resultTestOutput{Value: "abc"}
	r := OkResult{Idx: 3, Output: out}
	require.Equal(t, 3, r.ActionIndex())

	// Type switch — the Phase 3 interpreter pattern.
	got, ok := r.Output.(resultTestOutput)
	require.True(t, ok, "Output must round-trip back to resultTestOutput, got %T", r.Output)
	assert.Equal(t, "abc", got.Value)
}

// TestSkippedResult_Constructible verifies D2-02: SkippedResult is defined
// for type-spine completeness, even though no v1 code path emits it.
// Constructibility (zero-value + populated) is the assertion; a future
// emission path will add behavior tests.
func TestSkippedResult_Constructible(t *testing.T) {
	r := SkippedResult{Idx: 0, Reason: "x"}
	assert.Equal(t, 0, r.ActionIndex())
	assert.Equal(t, "x", r.Reason)

	// Zero value also constructible; ActionIndex returns the zero Idx.
	var z SkippedResult
	assert.Equal(t, 0, z.ActionIndex())
	assert.Empty(t, z.Reason)
}

// TestActionResult_TypeSwitchExhaustive documents the type-switch pattern
// Phase 3's interpreter will use. If a new kind is added in the future,
// this test should be updated to cover it (and the default branch will
// catch the unknown variant in the meantime).
func TestActionResult_TypeSwitchExhaustive(t *testing.T) {
	results := []ActionResult{
		OkResult{Idx: 0, Output: resultTestOutput{}},
		RetryableErrResult{Idx: 1, Err: errors.New("e")},
		NonRetryableErrResult{Idx: 2, Err: errors.New("e")},
		SkippedResult{Idx: 3, Reason: "r"},
	}
	seen := make(map[string]bool)
	for _, r := range results {
		switch r.(type) {
		case OkResult:
			seen["ok"] = true
		case RetryableErrResult:
			seen["retryable"] = true
		case NonRetryableErrResult:
			seen["nonretryable"] = true
		case SkippedResult:
			seen["skipped"] = true
		default:
			t.Fatalf("unexpected ActionResult kind: %T", r)
		}
	}
	assert.Len(t, seen, 4, "every kind must be exercised by the type switch")
}
