package parser

import "go.starlark.net/resolve"

// init explicitly enables lambda support (DSL-10 / D-10).
//
// As of go.starlark.net 2026-03-26, resolve.AllowLambda is documented as
// obsolete (true by default), but DSL-10 mandates the explicit assignment as
// a documentation contract — lambdas are the *only* legal expression-evaluation
// surface in Skytime, and removing this line would obscure that intent. A
// future Starlark major version may revive the flag in a different sense; the
// explicit assignment leaves a code-search anchor for that audit.
//
// We also pass an explicit *syntax.FileOptions to ExecFileOptions per parse
// (see parser.defaultFileOptions) so per-file behavior is forward-compatible
// should the resolve package be refactored.
func init() {
	resolve.AllowLambda = true
}
