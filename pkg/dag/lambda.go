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
