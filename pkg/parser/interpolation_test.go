package parser

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// =============================================================================
// Task 1 — scanInterpolation state-machine tests (D4.1-04)
//
// The scanner walks already-unquoted string content tracking ${...}
// markers. State machine (RESEARCH §Pitfall 2):
//   - text: literal scanning; $$ → literal $; ${ → enter expr.
//   - expr: bracket-counting expression mode; tracks string-literal sub-mode.
//   - expr-str: inside a "..." or '...' literal in expr; honors \ escapes.
//
// Errors are *dag.ParseError with verbatim substrings for each failure
// mode (the messages are what consultants see in their .star errors).
// =============================================================================

// userPos is a fixed user-source position used to seed scanner errors.
// The scanner adjusts positions by counting newlines inside `raw` so the
// returned expr Pos reflects the offset of each ${ within the literal.
func userPos() syntax.Position {
	name := "user.star"
	return syntax.MakePosition(&name, 10, 5)
}

// TestScanInterpolation_NoMarkers: a string with no ${...} returns
// HasInterpolation=false. Parts may be empty (caller short-circuits and
// stores the literal string as-is).
func TestScanInterpolation_NoMarkers(t *testing.T) {
	got, err := scanInterpolation("just a plain string", userPos())
	require.NoError(t, err)
	require.False(t, got.HasInterpolation, "no markers → HasInterpolation must be false")
}

// TestScanInterpolation_SingleMarker: "before${ctx.x}after" → 3 parts in
// order (text, expr, text). HasInterpolation=true.
func TestScanInterpolation_SingleMarker(t *testing.T) {
	got, err := scanInterpolation("before${ctx.x}after", userPos())
	require.NoError(t, err)
	require.True(t, got.HasInterpolation)
	require.Len(t, got.Parts, 3)
	assert.Equal(t, "text", got.Parts[0].Kind)
	assert.Equal(t, "before", got.Parts[0].Text)
	assert.Equal(t, "expr", got.Parts[1].Kind)
	assert.Equal(t, "ctx.x", got.Parts[1].Expr)
	assert.Equal(t, "text", got.Parts[2].Kind)
	assert.Equal(t, "after", got.Parts[2].Text)
}

// TestScanInterpolation_MultipleMarkers: "a${ctx.x}b${ctx.y}c" → 5 parts
// in order.
func TestScanInterpolation_MultipleMarkers(t *testing.T) {
	got, err := scanInterpolation("a${ctx.x}b${ctx.y}c", userPos())
	require.NoError(t, err)
	require.True(t, got.HasInterpolation)
	require.Len(t, got.Parts, 5)
	assert.Equal(t, []string{"text", "expr", "text", "expr", "text"},
		[]string{got.Parts[0].Kind, got.Parts[1].Kind, got.Parts[2].Kind, got.Parts[3].Kind, got.Parts[4].Kind})
	assert.Equal(t, "a", got.Parts[0].Text)
	assert.Equal(t, "ctx.x", got.Parts[1].Expr)
	assert.Equal(t, "b", got.Parts[2].Text)
	assert.Equal(t, "ctx.y", got.Parts[3].Expr)
	assert.Equal(t, "c", got.Parts[4].Text)
}

// TestScanInterpolation_EscapeDoubleDollar: doubled $$ produces literal
// "${...}" — no interpolation, the entire string collapses to one text
// part with HasInterpolation=false.
func TestScanInterpolation_EscapeDoubleDollar(t *testing.T) {
	got, err := scanInterpolation("$${literal}", userPos())
	require.NoError(t, err)
	require.False(t, got.HasInterpolation, "$$ escape → HasInterpolation must be false")
	require.Len(t, got.Parts, 1)
	assert.Equal(t, "text", got.Parts[0].Kind)
	assert.Equal(t, "${literal}", got.Parts[0].Text)
}

// TestScanInterpolation_EmptyExprError: empty ${} is rejected with the
// substring "empty interpolation".
func TestScanInterpolation_EmptyExprError(t *testing.T) {
	_, err := scanInterpolation("${}", userPos())
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.True(t, strings.Contains(pe.Error(), "empty interpolation"),
		"error must contain 'empty interpolation', got: %v", pe.Error())
}

// TestScanInterpolation_UnterminatedError: a ${ with no matching } is
// rejected with the substring "unterminated interpolation".
func TestScanInterpolation_UnterminatedError(t *testing.T) {
	_, err := scanInterpolation("/repos/${ctx.repo", userPos())
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.True(t, strings.Contains(pe.Error(), "unterminated interpolation"),
		"error must contain 'unterminated interpolation', got: %v", pe.Error())
}

// TestScanInterpolation_MultilineError: a ${...} containing a newline is
// rejected with the substring "multi-line interpolation".
func TestScanInterpolation_MultilineError(t *testing.T) {
	_, err := scanInterpolation("/repos/${\nctx.repo}", userPos())
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.True(t, strings.Contains(pe.Error(), "multi-line interpolation"),
		"error must contain 'multi-line interpolation', got: %v", pe.Error())
}

// TestScanInterpolation_NestedBraces: "${ctx.foo({\"k\":1})}" — bracket-
// counting honors string-literal mode (the {} inside "k":1 is text, not a
// depth bump on the OUTER ${}).
func TestScanInterpolation_NestedBraces(t *testing.T) {
	got, err := scanInterpolation(`${ctx.foo({"k":1})}`, userPos())
	require.NoError(t, err)
	require.True(t, got.HasInterpolation)
	require.Len(t, got.Parts, 1)
	assert.Equal(t, "expr", got.Parts[0].Kind)
	assert.Equal(t, `ctx.foo({"k":1})`, got.Parts[0].Expr)
}

// TestScanInterpolation_StringInsideExpr: "${ctx.s.replace(\"}\", \"X\")}" —
// the } inside the string literal does NOT terminate the expression.
func TestScanInterpolation_StringInsideExpr(t *testing.T) {
	got, err := scanInterpolation(`${ctx.s.replace("}", "X")}`, userPos())
	require.NoError(t, err)
	require.True(t, got.HasInterpolation)
	require.Len(t, got.Parts, 1)
	assert.Equal(t, "expr", got.Parts[0].Kind)
	assert.Equal(t, `ctx.s.replace("}", "X")`, got.Parts[0].Expr)
}

// TestScanInterpolation_DollarSignAlone: a bare $ not followed by { is
// literal — no error, no interpolation.
func TestScanInterpolation_DollarSignAlone(t *testing.T) {
	got, err := scanInterpolation("price: $5", userPos())
	require.NoError(t, err)
	require.False(t, got.HasInterpolation)
	require.Len(t, got.Parts, 1)
	assert.Equal(t, "text", got.Parts[0].Kind)
	assert.Equal(t, "price: $5", got.Parts[0].Text)
}
