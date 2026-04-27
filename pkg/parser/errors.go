package parser

import (
	"errors"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// wrapStarlarkError converts go.starlark.net error types into typed
// *dag.ParseError so callers can errors.As / errors.Is / Position() them per
// D-04. PARSE-05: every malformed input must surface as *dag.ParseError
// rather than a raw starlark error or a panic.
//
// Unwrap-first: when our parser builtins (or the load resolver) returned a
// typed *dag.ParseError or *dag.ValidationError, Starlark's interpreter may
// have wrapped it (e.g. wrappedError{"cannot load X: ...", cause}). We
// unwrap to surface the ORIGINAL typed error directly — callers' errors.As
// chain still works either way, but the displayed Error() message is the
// clean typed-error format without Starlark's wrapping prefix.
func wrapStarlarkError(err error) error {
	if err == nil {
		return nil
	}
	// Unwrap-first: surface our typed errors directly.
	var pe *dag.ParseError
	if errors.As(err, &pe) {
		return pe
	}
	var ve *dag.ValidationError
	if errors.As(err, &ve) {
		return ve
	}

	// Starlark eval error: walk CallStack to find the .star-side frame.
	var evalErr *starlark.EvalError
	if errors.As(err, &evalErr) {
		var pos syntax.Position
		if len(evalErr.CallStack) > 0 {
			// CallStack[0] is the bottom (oldest) frame; the .At helper
			// also exists for symmetry. Use index 0 as the .star-side
			// reference point — Phase 4's CLI may render the full
			// backtrace separately.
			pos = evalErr.CallStack.At(0).Pos
		}
		return &dag.ParseError{
			Pos:     pos,
			Msg:     evalErr.Msg,
			Wrapped: err,
		}
	}

	// Starlark syntax error: position is on the value itself.
	var syntaxErr syntax.Error
	if errors.As(err, &syntaxErr) {
		return &dag.ParseError{
			Pos:     syntaxErr.Pos,
			Msg:     syntaxErr.Msg,
			Wrapped: err,
		}
	}

	// Starlark resolve errors (multi-error wrapper) — fall through with no position.
	return &dag.ParseError{
		Msg:     err.Error(),
		Wrapped: err,
	}
}
