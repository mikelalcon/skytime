package parser

import (
	"fmt"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// captureLambda extracts a *starlark.Function, computes its D-18 stable ID,
// and registers it in the parser session's lambda table. Free-var validation
// (D-19) is handled by validateFreeVars in linter.go.
//
// The kwargName argument is used in error messages — when an if_cond's
// `cond` kwarg is something other than a function (e.g., a string literal),
// the error mentions the kwarg name so consultants can locate the bug.
func (p *Parser) captureLambda(thread *starlark.Thread, kwargName string, val starlark.Value) (*dag.CapturedLambda, error) {
	fn, ok := val.(*starlark.Function)
	if !ok {
		return nil, &dag.ParseError{
			Pos: callerPosition(thread),
			Msg: fmt.Sprintf("kwarg %q must be a lambda or function, got %s", kwargName, val.Type()),
		}
	}
	pos := fn.Position()

	fileBytes, ok := p.fileBytes[pos.Filename()]
	if !ok || fileBytes == nil {
		// Defensive — should always be present after exec because parse()
		// caches src bytes for filename and load.go caches each loaded
		// file's bytes. If it isn't, the lambda came from an
		// unrecognized source — surface a clear error rather than
		// computing an ID against a nil hash input.
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: fmt.Sprintf("internal: file bytes for %q not cached (lambda came from an untracked file?)", pos.Filename()),
		}
	}
	id := dag.ComputeLambdaID(fileBytes, pos)

	freeVars, err := p.validateFreeVars(fn, pos.Filename())
	if err != nil {
		return nil, err
	}

	captured := &dag.CapturedLambda{
		ID:       id,
		Fn:       fn,
		Pos:      pos,
		FreeVars: freeVars,
	}
	p.lambdas[id] = captured
	return captured, nil
}
