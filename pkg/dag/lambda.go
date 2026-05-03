package dag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// CapturedLambda holds a parser-captured *starlark.Function alongside the
// metadata that survives across the Phase 3 Temporal serialization boundary
// (D-18 lambda IDs, D-19 frozen free-variable map). The Fn pointer itself is
// in-memory only in Phase 1 — Phase 3 owns the serialization decision (custom
// DataConverter vs. re-parse-on-start, both keyed off ID).
type CapturedLambda struct {
	// ID is the stable lambda identifier per D-18:
	//   sha256(fileBytes)[:8] hex + ":" + line + ":" + col
	// Cosmetic edits to the file (whitespace, comments) change the prefix.
	// That is intentional — it lets Phase 3's re-parse-on-start verify file
	// content matches without needing a canonicalized AST.
	ID string

	// Fn is the *starlark.Function captured at parse time. ⚠ NOT
	// JSON-serializable; Phase 3 picks how this survives across the Temporal
	// history boundary.
	Fn *starlark.Function

	// Pos is Function.Position() — the def-site of the `lambda` keyword.
	Pos syntax.Position

	// BodyPos is the position used by AST-walking validators (D4-02
	// ctx.<name> visitor; D4.1-11 block_fn classifier) to find the
	// lambda's body in re-parsed source. For hand-written lambdas this
	// equals Pos. For synthesized lambdas (D4.1-01 interpolation
	// desugarer), Pos points at the user's source (opening ${ for error
	// attribution) while BodyPos points at the synthetic-file location
	// where the lambda body actually lives. Zero value (syntax.Position{},
	// where !pos.IsValid()) means "fall back to Pos for AST walks" —
	// callers MUST check `if cl.BodyPos.IsValid() { use BodyPos } else
	// { use Pos }`.
	BodyPos syntax.Position

	// FreeVars are the lambda's free variables, frozen at parse time. D-19:
	// only frozen module-level constants/functions are allowed. The parser
	// (plan 05) performs the validation; Phase 1 only stores the data.
	FreeVars starlark.StringDict
}

// ComputeLambdaID returns the stable lambda ID for a function located at pos
// within fileBytes. Format: sha256(fileBytes)[:4] hex (8 chars) + ":" + line
// + ":" + col. The hash prefix is over the file CONTENT, not the
// canonicalized AST — so cosmetic edits change IDs.
func ComputeLambdaID(fileBytes []byte, pos syntax.Position) string {
	sum := sha256.Sum256(fileBytes)
	return fmt.Sprintf("%s:%d:%d", hex.EncodeToString(sum[:4]), pos.Line, pos.Col)
}

// StarlarkLambda is a starlark.Value wrapper around *CapturedLambda so
// that the parser-time interpolation desugarer (D4.1-01) can store
// lambda values inside an ActionRef.Kwargs *starlark.Dict (D4.1-05).
// At workflow-resolve time, pkg/interpreter unwraps via
// UnwrapStarlarkLambda and evaluates via bridge.CallLambda.
//
// Mirrors pkg/parser/builtins.go::nodeValue's seal-and-wrap idiom but
// lives in pkg/dag because pkg/interpreter needs to unwrap WITHOUT
// importing pkg/parser (interpreter is a foundation package; parser
// is a leaf relative to it).
type StarlarkLambda struct {
	Captured *CapturedLambda
}

// Compile-time guarantee: *StarlarkLambda satisfies starlark.Value.
var _ starlark.Value = (*StarlarkLambda)(nil)

// NewStarlarkLambda wraps a CapturedLambda for storage inside a
// *starlark.Dict. Returns nil when cl is nil (defensive).
func NewStarlarkLambda(cl *CapturedLambda) *StarlarkLambda {
	if cl == nil {
		return nil
	}
	return &StarlarkLambda{Captured: cl}
}

// UnwrapStarlarkLambda returns (cl, true) when v is a *StarlarkLambda
// produced by NewStarlarkLambda; (nil, false) otherwise. The
// interpreter's resolveKwargs (D4.1-14) uses this to detect
// lambda-valued kwargs inside ActionRef.Kwargs.
func UnwrapStarlarkLambda(v starlark.Value) (*CapturedLambda, bool) {
	sl, ok := v.(*StarlarkLambda)
	if !ok || sl == nil {
		return nil, false
	}
	return sl.Captured, true
}

// String returns "CapturedLambda(<id>)" for debug output.
func (s *StarlarkLambda) String() string {
	if s == nil || s.Captured == nil {
		return "CapturedLambda(<nil>)"
	}
	return fmt.Sprintf("CapturedLambda(%s)", s.Captured.ID)
}

// Type returns the Starlark type tag.
func (s *StarlarkLambda) Type() string { return "CapturedLambda" }

// Truth marks every captured lambda as truthy — never used in user
// flows, but starlark.Value requires the method.
func (s *StarlarkLambda) Truth() starlark.Bool { return starlark.True }

// Hash refuses hashability — captured lambdas have no canonical
// equality across reparses (file-content-hash IDs change with
// cosmetic edits).
func (s *StarlarkLambda) Hash() (uint32, error) {
	return 0, fmt.Errorf("CapturedLambda is not hashable")
}

// Freeze is a no-op — CapturedLambda is treated as immutable after
// construction.
func (s *StarlarkLambda) Freeze() {}
