package dag

import (
	"errors"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
)

// Compile-time assertions: both error types implement error and the
// Position-bearing interface. These would fail to compile if the surface
// drifts; explicit so reviewers see the contract.
var (
	_ error                                   = (*ParseError)(nil)
	_ error                                   = (*ValidationError)(nil)
	_ interface{ Position() syntax.Position } = (*ParseError)(nil)
	_ interface{ Position() syntax.Position } = (*ValidationError)(nil)
)

// validPos returns a syntax.Position that satisfies Position.IsValid().
// IsValid requires a non-empty filename; line/col may be zero.
func validPos(t *testing.T) syntax.Position {
	t.Helper()
	// MakePosition is the documented constructor.
	pos := syntax.MakePosition(strPtr("flow.star"), 12, 7)
	require.True(t, pos.IsValid(), "constructed position must be valid")
	return pos
}

func strPtr(s string) *string { return &s }

// --- ParseError --------------------------------------------------------------

func TestParseError_ImplementsError(t *testing.T) {
	var e error = &ParseError{Msg: "x"}
	require.Error(t, e)
}

func TestParseError_ExposesPosition(t *testing.T) {
	pos := validPos(t)
	pe := &ParseError{Pos: pos, Msg: "boom"}
	got := pe.Position()
	assert.Equal(t, pos, got, "Position() returns the embedded position")
}

func TestParseError_ErrorWithValidPos(t *testing.T) {
	pos := validPos(t)
	pe := &ParseError{Pos: pos, Msg: "missing required 'name'"}
	got := pe.Error()
	re := regexp.MustCompile(`^[^:]+:\d+:\d+: missing required 'name'$`)
	assert.Regexp(t, re, got, "Error() formats <file>:<line>:<col>: <msg> when Pos is valid")
}

func TestParseError_ErrorWithoutPos(t *testing.T) {
	pe := &ParseError{Msg: "no position"}
	assert.Equal(t, "no position", pe.Error(), "Error() returns just Msg when Pos is invalid (zero)")
}

func TestParseError_UnwrapWithErrorsAs(t *testing.T) {
	innerType := &sentinelInner{label: "inner"}
	pe := &ParseError{Msg: "outer", Wrapped: innerType}

	var target *sentinelInner
	require.True(t, errors.As(pe, &target), "errors.As must walk Unwrap to reach inner type")
	assert.Equal(t, "inner", target.label)
}

// --- ValidationError ---------------------------------------------------------

func TestValidationError_ImplementsError(t *testing.T) {
	var e error = &ValidationError{Msg: "x"}
	require.Error(t, e)
}

func TestValidationError_ExposesPosition(t *testing.T) {
	pos := validPos(t)
	ve := &ValidationError{Pos: pos, Flow: "f", Step: "s", Msg: "boom"}
	assert.Equal(t, pos, ve.Position())
}

func TestValidationError_ErrorWithValidPos(t *testing.T) {
	pos := validPos(t)
	ve := &ValidationError{Pos: pos, Flow: "approve_pr", Step: "create_issue", Msg: "missing required 'title'"}
	// D4-04 (Phase 4): when Flow and/or Step are set, Error() now renders
	// the [flow > step > action] bracket. Update the regex to expect the
	// bracket. The pure no-bracket fallback is covered by the
	// "none set" subtest in TestValidationError_FormatWithAction.
	re := regexp.MustCompile(`^[^:]+:\d+:\d+ \[approve_pr > create_issue\]: missing required 'title'$`)
	assert.Regexp(t, re, ve.Error())
}

func TestValidationError_ErrorWithoutPos(t *testing.T) {
	ve := &ValidationError{Msg: "no position"}
	assert.Equal(t, "no position", ve.Error())
}

func TestValidationError_UnwrapWithErrorsAs(t *testing.T) {
	inner := &sentinelInner{label: "inner-v"}
	ve := &ValidationError{Msg: "outer", Wrapped: inner}

	var target *sentinelInner
	require.True(t, errors.As(ve, &target))
	assert.Equal(t, "inner-v", target.label)
}

// TestValidationError_FormatWithAction covers D4-04: the Action field plus
// the [flow > step > action] bracket rendering. Six subtests cover every
// combination the CLI's error renderer (D4-18) and existing Phase 1/2/3
// callers can produce. Legacy callers leaving Action="" land in the
// no-action subtest below; their output is unchanged.
func TestValidationError_FormatWithAction(t *testing.T) {
	pos := syntax.MakePosition(strPtr("flows/x.star"), 10, 5)
	require.True(t, pos.IsValid(), "constructed position must be valid")

	t.Run("all three set with valid pos", func(t *testing.T) {
		e := &ValidationError{
			Pos:    pos,
			Flow:   "my_flow",
			Step:   "step_2",
			Action: "github.create_issue",
			Msg:    "missing kwarg",
		}
		require.Equal(t,
			"flows/x.star:10:5 [my_flow > step_2 > github.create_issue]: missing kwarg",
			e.Error(),
		)
	})

	t.Run("flow only with valid pos", func(t *testing.T) {
		e := &ValidationError{
			Pos:  pos,
			Flow: "my_flow",
			Msg:  "foo",
		}
		require.Equal(t, "flows/x.star:10:5 [my_flow]: foo", e.Error())
	})

	t.Run("flow plus action no step with valid pos", func(t *testing.T) {
		e := &ValidationError{
			Pos:    pos,
			Flow:   "my_flow",
			Action: "github.create_issue",
			Msg:    "foo",
		}
		require.Equal(t,
			"flows/x.star:10:5 [my_flow > github.create_issue]: foo",
			e.Error(),
		)
	})

	t.Run("none set with valid pos drops bracket entirely", func(t *testing.T) {
		e := &ValidationError{
			Pos: pos,
			Msg: "bare msg",
		}
		require.Equal(t, "flows/x.star:10:5: bare msg", e.Error())
	})

	t.Run("invalid pos with flow set drops position prefix", func(t *testing.T) {
		e := &ValidationError{
			Flow: "my_flow",
			Msg:  "bare msg",
		}
		require.Equal(t, "[my_flow]: bare msg", e.Error())
	})

	t.Run("invalid pos and none set falls back to msg", func(t *testing.T) {
		e := &ValidationError{Msg: "bare msg"}
		require.Equal(t, "bare msg", e.Error())
	})
}

// --- helpers -----------------------------------------------------------------

type sentinelInner struct{ label string }

func (s *sentinelInner) Error() string { return "sentinel:" + s.label }
