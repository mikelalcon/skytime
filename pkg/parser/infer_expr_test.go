package parser

// Wave 0 RED scaffolding for D4.2-10 inferType pass (plan 02 fills the
// body; today inferType returns TypeOpaque{} for everything). Tests
// assert the EXPECTED post-implementation behavior; they fail loudly
// against the stub until plan 02 ships the real inference.
//
// Test groups follow the Type Inference Decision Table in RESEARCH.md:
//   - literal scalars (int / float / bool / string / none)
//   - ctx.<name> attribute access against a typed schema
//   - binary operators with per-operand-type rules
//   - locked-vocab calls with known return types
//   - shapes outside the vocabulary collapse to TypeOpaque

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
)

// parseInferExpr is the test helper: parses a Starlark expression source
// string and returns the resulting *syntax.Expr ready to feed inferType.
// Mirrors the convenience pattern in pkg/parser/interpolation.go which
// already uses syntax.ParseExpr for desugar-expression validation.
func parseInferExpr(t *testing.T, src string) syntax.Expr {
	t.Helper()
	expr, err := syntax.ParseExpr("infer_test.star", src, 0)
	require.NoError(t, err, "expression must parse: %s", src)
	return expr
}

// TestInferType_LiteralScalars: 42 → int, 1.5 → float, "x" → string,
// True → bool, None → none. RED against the TypeOpaque{} stub.
func TestInferType_LiteralScalars(t *testing.T) {
	schema := newStateSchema()
	cases := []struct {
		src  string
		want TypeInfo
	}{
		{"42", TypeScalar{Kind: "int"}},
		{"1.5", TypeScalar{Kind: "float"}},
		{`"x"`, TypeScalar{Kind: "string"}},
		{"True", TypeScalar{Kind: "bool"}},
		{"None", TypeScalar{Kind: "none"}},
	}
	for _, c := range cases {
		got := inferType(parseInferExpr(t, c.src), schema, "ctx")
		require.True(t, Equal(got, c.want),
			"RED until plan 02: src=%q got=%v want=%v", c.src, got, c.want)
	}
}

// TestInferType_CtxAccess_FromStateSchema: ctx.<name> resolves to the
// schema entry's TypeInfo; missing name → opaque. RED.
func TestInferType_CtxAccess_FromStateSchema(t *testing.T) {
	schema := newStateSchema()
	schema.add("a", TypeScalar{Kind: "int"})

	got := inferType(parseInferExpr(t, "ctx.a"), schema, "ctx")
	require.True(t, Equal(got, TypeScalar{Kind: "int"}),
		"RED until plan 02: ctx.a should resolve to int; got %v", got)

	got2 := inferType(parseInferExpr(t, "ctx.missing"), schema, "ctx")
	require.True(t, Equal(got2, TypeOpaque{}),
		"RED until plan 02: ctx.missing should be opaque; got %v", got2)
}

// TestInferType_BinaryOperator_PerOperandRules: 1+2 → int, 1.0+2.0 →
// float, "a"+"b" → string, 1+1.0 → opaque (NO LUB), 5/2 → float,
// 5//2 → int, 1<2 → bool. RED.
func TestInferType_BinaryOperator_PerOperandRules(t *testing.T) {
	schema := newStateSchema()
	cases := []struct {
		src  string
		want TypeInfo
	}{
		{"1+2", TypeScalar{Kind: "int"}},
		{"1.0+2.0", TypeScalar{Kind: "float"}},
		{`"a"+"b"`, TypeScalar{Kind: "string"}},
		{"1+1.0", TypeOpaque{}}, // strict no-LUB
		{"5/2", TypeScalar{Kind: "float"}},
		{"5//2", TypeScalar{Kind: "int"}},
		{"1<2", TypeScalar{Kind: "bool"}},
	}
	for _, c := range cases {
		got := inferType(parseInferExpr(t, c.src), schema, "ctx")
		require.True(t, Equal(got, c.want),
			"RED until plan 02: src=%q got=%v want=%v", c.src, got, c.want)
	}
}

// TestInferType_LockedVocabCalls_KnownReturnTypes: int(x) → int,
// float(x) → float, str(x) → string, bool(x) → bool, len(x) → int,
// abs(1) → int, abs(1.5) → float, sorted([1,2]) → list[int],
// any(x) → bool, all(x) → bool. RED.
func TestInferType_LockedVocabCalls_KnownReturnTypes(t *testing.T) {
	schema := newStateSchema()
	schema.add("x", TypeScalar{Kind: "int"})
	cases := []struct {
		src  string
		want TypeInfo
	}{
		{"int(ctx.x)", TypeScalar{Kind: "int"}},
		{"float(ctx.x)", TypeScalar{Kind: "float"}},
		{"str(ctx.x)", TypeScalar{Kind: "string"}},
		{"bool(ctx.x)", TypeScalar{Kind: "bool"}},
		{"len(ctx.x)", TypeScalar{Kind: "int"}},
		{"abs(1)", TypeScalar{Kind: "int"}},
		{"abs(1.5)", TypeScalar{Kind: "float"}},
		{"sorted([1,2])", TypeList{Element: TypeScalar{Kind: "int"}}},
		{"any(ctx.x)", TypeScalar{Kind: "bool"}},
		{"all(ctx.x)", TypeScalar{Kind: "bool"}},
	}
	for _, c := range cases {
		got := inferType(parseInferExpr(t, c.src), schema, "ctx")
		require.True(t, Equal(got, c.want),
			"RED until plan 02: src=%q got=%v want=%v", c.src, got, c.want)
	}
}

// TestInferType_OpaqueShapesReturnOpaque: anything outside the locked
// vocabulary (user-defined helper, comprehension, range) collapses to
// TypeOpaque so the branch-equality validator defers cleanly. The stub
// already returns Opaque universally, so these expectations PASS today;
// plan 02's real inference must continue to return Opaque for these
// shapes (it's a regression guard for the inverse: do NOT misreport
// these as concrete).
func TestInferType_OpaqueShapesReturnOpaque(t *testing.T) {
	schema := newStateSchema()
	schema.add("xs", TypeList{Element: TypeScalar{Kind: "int"}})
	cases := []string{
		"helper(ctx.xs)",
		"[x for x in ctx.xs]",
		"range(10)",
	}
	for _, src := range cases {
		got := inferType(parseInferExpr(t, src), schema, "ctx")
		require.True(t, Equal(got, TypeOpaque{}),
			"src=%q must collapse to opaque; got %v", src, got)
	}
}
