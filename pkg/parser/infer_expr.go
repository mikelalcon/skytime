package parser

import "go.starlark.net/syntax"

// inferType statically infers the TypeInfo of a Starlark expression
// against a typed state schema. Implemented in plan 02 of Phase 04.2;
// this Wave 0 stub returns TypeOpaque{} for every input so plan 01's
// RED tests fail with the deterministic "expected scalar, got opaque"
// shape instead of a nil-deref. The signature is locked: plan 02 drops
// in the real body without touching call sites.
//
// schema is the state visible at the call site (flow.Inputs typed via
// typeFromHint, plus any prior script.OutputAlias / item-var entries).
// firstParam is the lambda's first parameter name — typically "ctx" but
// not enforced (a future locked-vocab change could rename it). The
// implementation walks AST nodes (not evaluated values); literals,
// `firstParam`.X attribute access (resolved against schema), Starlark
// binary operators (per-operand-type rules), the locked-20 lambda-time
// builtins (known return types), and `a if c else b` recursive arm
// equality each map to a concrete TypeInfo. Anything outside that
// vocabulary collapses to TypeOpaque so the branch-equality validator
// (plan 03) defers strict checks instead of misreporting.
func inferType(e syntax.Expr, schema stateSchema, firstParam string) TypeInfo {
	_ = e
	_ = schema
	_ = firstParam
	return TypeOpaque{}
}
