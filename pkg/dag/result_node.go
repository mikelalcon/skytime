package dag

import "go.starlark.net/syntax"

// Result is an expression-mode if_cond branch terminator (D4.2-02..04).
// Each value is a per-key CapturedLambda synthesized at parse time
// (D4.1-01 desugarer reused). Keys is the source insertion order — the
// interpreter MUST iterate Keys (not `for k := range Values`) to preserve
// replay determinism (D3-23 + Pitfall 5).
//
// Result is a LEAF: it has no body, no children. The parser rejects it
// anywhere except the LAST node of an if_cond branch whose
// OutputAlias != "".
//
// Note: Types is `map[string]any` (rather than `map[string]TypeInfo`
// directly) to avoid a `pkg/dag` → `pkg/parser` import that would break
// the existing layering. Plan 02 stores `parser.TypeInfo` values; plan 03
// validator type-asserts back. The interpreter never reads Types.
type Result struct {
	Pos    syntax.Position
	Keys   []string                   // insertion order from source dict-literal (replay-deterministic)
	Values map[string]*CapturedLambda // per-key value lambdas
	// Types is a parser-side annotation set BY plan 02's inferType pass.
	// Type is `any` here (not parser.TypeInfo) because pkg/dag MUST NOT
	// import pkg/parser. The parser stores `parser.TypeInfo`; the
	// interpreter never reads Types. Validator unwraps via type assertion.
	Types map[string]any // map[string]parser.TypeInfo at runtime; opaque to dag
}

var _ Node = (*Result)(nil)

// Kind returns the discriminator "Result".
func (*Result) Kind() string { return "Result" }

// Position returns the call-site of `result(...)`.
func (n *Result) Position() syntax.Position { return n.Pos }

func (*Result) nodeMarker() {}
