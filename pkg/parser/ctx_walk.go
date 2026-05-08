package parser

import (
	"go.starlark.net/syntax"
)

// ctxAccess is one ctx.<attr> reference found inside a lambda body. Pos is
// dot.NamePos (the attribute name's position, used for error attribution);
// AttrName is the string after the dot (e.g., "repo_name" in
// `ctx.repo_name`).
type ctxAccess struct {
	Pos      syntax.Position
	AttrName string
}

// findCtxAccesses re-parses src to recover the AST, locates the lambda or
// def whose keyword position equals lambdaPos, and returns every
// <firstParam>.<attr> access in its body.
//
// Implementation note (04-RESEARCH §Pattern 3 critical finding):
// *starlark.Function does NOT retain its AST after compilation. The captured
// lambda's Fn pointer is the runtime closure; the AST is reachable only by
// re-parse of the original source bytes. This function is invoked from the
// finalize pass with src = parser.FileBytes()[filename], so re-parse is
// O(file_bytes) and runs once per validate / once per parse — cheap.
//
// Position match strategy: walk every *syntax.LambdaExpr and *syntax.DefStmt
// in the re-parsed File and pick the one whose keyword position equals
// lambdaPos. starlark-go's *Function.Position() returns the keyword position
// (verified via pkg/parser/lambda_capture.go's `pos := fn.Position()`), so
// the match condition is LambdaExpr.Lambda == lambdaPos OR DefStmt.Def ==
// lambdaPos. Two lambdas on the same line are distinguishable because their
// Col fields differ (Pitfall #1 — TestCtxWalk_TwoLambdasSameLine covers).
//
// Returns ([], nil) when no matching lambda is found in the re-parsed file —
// defensive; callers treat absent lambdas as "nothing to check" (a missing
// lambda would have errored earlier via lambda capture).
//
// Phase 7 refactor (Plan 07-03): findCtxAccesses now delegates to
// findFreeVarAccesses (req_walk.go) after extracting the lambda's first
// positional parameter name via firstParamNameAt. The two-pass cost is
// bounded — each pass is O(file_bytes), and the parser-time finalize chain
// already pays one re-parse per lambda. The generalization lets the
// trigger req-walker share the same AST traversal without duplicating it.
func findCtxAccesses(src []byte, filename string, lambdaPos syntax.Position) ([]ctxAccess, error) {
	firstParam, err := firstParamNameAt(src, filename, lambdaPos)
	if err != nil {
		return nil, err
	}
	if firstParam == "" {
		// No matching lambda found OR the matched lambda has no params.
		// Both cases are "nothing to validate" — preserve pre-refactor
		// semantics (return empty, no error).
		return nil, nil
	}
	fv, err := findFreeVarAccesses(src, filename, lambdaPos, firstParam)
	if err != nil {
		return nil, err
	}
	out := make([]ctxAccess, len(fv))
	for i, a := range fv {
		out[i] = ctxAccess{Pos: a.Pos, AttrName: a.AttrName}
	}
	return out, nil
}

// firstParamNameAt re-parses src and returns the name of the FIRST
// positional parameter of the lambda/def whose keyword position equals
// lambdaPos. Returns ("", nil) when no matching lambda is found OR when
// the matched lambda has no params.
//
// Extracted from findCtxAccesses during the Phase 7 Plan 07-03 refactor
// so the ctx-walker can use the lambda's first-param convention while
// the new req-walker (req_walk.go::findFreeVarAccesses) takes the
// expected name as a parameter.
func firstParamNameAt(src []byte, filename string, lambdaPos syntax.Position) (string, error) {
	opts := defaultFileOptions()
	file, err := opts.Parse(filename, src, 0)
	if err != nil {
		return "", err
	}
	var name string
	syntax.Walk(file, func(n syntax.Node) bool {
		if name != "" {
			return false
		}
		switch fn := n.(type) {
		case *syntax.LambdaExpr:
			if positionsEqual(fn.Lambda, lambdaPos) && len(fn.Params) > 0 {
				name = paramName(fn.Params[0])
			}
		case *syntax.DefStmt:
			if positionsEqual(fn.Def, lambdaPos) && len(fn.Params) > 0 {
				name = paramName(fn.Params[0])
			}
		}
		return true
	})
	return name, nil
}

// positionsEqual compares two syntax.Position values on (Filename, Line,
// Col). The Pos's Filename returned by syntax.Walk on a re-parsed File
// matches what the parser stored on dag.CapturedLambda.Pos because both
// re-parse and original parse used the same filename argument.
func positionsEqual(a, b syntax.Position) bool {
	return a.Filename() == b.Filename() && a.Line == b.Line && a.Col == b.Col
}

// paramName extracts the parameter name from a parameter Expr. For
// LambdaExpr/DefStmt, the param shape is one of (per go.starlark.net docs):
//
//	*syntax.Ident                   — plain `x`
//	*syntax.BinaryExpr (Op=EQ)      — defaulted `x = expr`
//	*syntax.UnaryExpr (* / **)      — `*args` / `**kwargs`
//
// For Phase 4's check we only need plain Ident or defaulted Ident — *args /
// **kwargs are vanishingly rare for `ctx`-style first params, but the helper
// handles them defensively (returning the name without the prefix). Returns
// empty string for unrecognized shapes.
func paramName(e syntax.Expr) string {
	switch p := e.(type) {
	case *syntax.Ident:
		return p.Name
	case *syntax.BinaryExpr:
		if id, ok := p.X.(*syntax.Ident); ok {
			return id.Name
		}
	case *syntax.UnaryExpr:
		if id, ok := p.X.(*syntax.Ident); ok {
			return id.Name
		}
	}
	return ""
}
