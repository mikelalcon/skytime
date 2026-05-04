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
func findCtxAccesses(src []byte, filename string, lambdaPos syntax.Position) ([]ctxAccess, error) {
	opts := defaultFileOptions()
	file, err := opts.Parse(filename, src, 0)
	if err != nil {
		return nil, err
	}

	// First pass: locate the matching lambda/def and capture its first-param
	// name + body shape. We split lambda (Body is a single Expr) and def
	// (Body is a []Stmt) into two storage variables so the second pass can
	// dispatch on whichever is set.
	var (
		firstParamName string
		targetBody     syntax.Expr // for *syntax.LambdaExpr
		targetStmts    []syntax.Stmt // for *syntax.DefStmt
	)
	syntax.Walk(file, func(n syntax.Node) bool {
		switch fn := n.(type) {
		case *syntax.LambdaExpr:
			if positionsEqual(fn.Lambda, lambdaPos) && len(fn.Params) > 0 {
				firstParamName = paramName(fn.Params[0])
				targetBody = fn.Body
			}
		case *syntax.DefStmt:
			if positionsEqual(fn.Def, lambdaPos) && len(fn.Params) > 0 {
				firstParamName = paramName(fn.Params[0])
				targetStmts = fn.Body
			}
		}
		return true
	})

	if firstParamName == "" {
		// No matching lambda/def found, OR matched node had no params.
		// Both cases are "nothing to validate" — return empty.
		return nil, nil
	}

	// Second pass: walk the matched body collecting every DotExpr whose X is
	// the first-param Ident. Pitfall #9: dot.Name is *Ident, the string is
	// dot.Name.Name. Note: dot.NamePos is consistently <invalid> in
	// go.starlark.net's current syntax tree; use dot.Name.NamePos (the
	// Ident's position) instead — that one is properly populated.
	var accesses []ctxAccess
	collect := func(n syntax.Node) bool {
		if dot, ok := n.(*syntax.DotExpr); ok {
			if id, ok := dot.X.(*syntax.Ident); ok && id.Name == firstParamName {
				accesses = append(accesses, ctxAccess{
					Pos:      dot.Name.NamePos,
					AttrName: dot.Name.Name,
				})
			}
		}
		return true
	}
	if targetBody != nil {
		syntax.Walk(targetBody, collect)
	}
	for _, stmt := range targetStmts {
		syntax.Walk(stmt, collect)
	}
	return accesses, nil
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
//   *syntax.Ident                   — plain `x`
//   *syntax.BinaryExpr (Op=EQ)      — defaulted `x = expr`
//   *syntax.UnaryExpr (* / **)      — `*args` / `**kwargs`
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
