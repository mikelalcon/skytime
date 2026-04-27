package parser

import (
	"errors"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// wrapStarlarkError converts go.starlark.net error types into typed
// *dag.ParseError so callers can errors.As / errors.Is / Position() them per
// D-04. Pass-through when the error is already a *dag.ParseError or
// *dag.ValidationError (the parser builtins already produced the right type).
//
// PARSE-05: every malformed input must surface as *dag.ParseError rather
// than a raw starlark error or a panic.
func wrapStarlarkError(err error) error {
	if err == nil {
		return nil
	}
	// Already a typed parser/validation error — pass through.
	var pe *dag.ParseError
	if errors.As(err, &pe) {
		return err
	}
	var ve *dag.ValidationError
	if errors.As(err, &ve) {
		return err
	}

	// Starlark eval error: walk CallStack to find the .star-side frame.
	var evalErr *starlark.EvalError
	if errors.As(err, &evalErr) {
		var pos syntax.Position
		if len(evalErr.CallStack) > 0 {
			// CallStack[0] is the bottom (most recent) frame — closest to
			// where execution actually failed. Phase 1 reports that frame's
			// position; Phase 4's CLI may render the full backtrace.
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
