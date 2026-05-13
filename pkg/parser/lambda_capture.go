package parser

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

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

// captureLambdaAtPosition is the variant of captureLambda used by the
// D4.1-01 interpolation desugarer to register a synthesized lambda. It
// differs from captureLambda in three ways:
//
//  1. The caller supplies the *starlark.Function directly (already
//     compiled from synthetic source). No type-assertion gate.
//  2. The caller supplies userPos (the user's source position, opening
//     ${ for interpolation) — used for Pos and for D-18 ID hashing
//     against the USER file's bytes. Cosmetic edits to the template
//     string change the ID via the user file's content hash, matching
//     the D-18 deployment property.
//  3. The caller supplies bodyPos (the synthetic-file location where the
//     lambda body actually lives) — populated on CapturedLambda.BodyPos
//     so D4-02's findCtxAccesses re-parses the synthetic file to walk
//     the AST.
//
// Synthesized lambdas have NO free variables — `${ctx.foo}` references
// only the lambda's own ctx parameter. Setting FreeVars to an empty,
// frozen StringDict here matches what validateFreeVars would return for
// a lambda with no closures and skips the D-19 module-level binding
// check (which would mis-attribute the synthetic file as the owning
// file; see RESEARCH §Pitfall 3).
//
//  4. disambiguator (NEW, quick 260512-w7c) — when non-empty, appended
//     as ":"+disambiguator to the base D-18 ID. Used by the kwargs-
//     desugarer (D4.1-05) to distinguish multiple ${...} kwargs that
//     share a single openPos (the parent ActionRef's call position).
//     Empty string preserves the historical ID format and is what every
//     other caller (flow.name, step.name, script.id, fail.msg, log.msg,
//     result.value) passes. Without this disambiguator, two kwargs on
//     the same factory call (e.g. owner+repo both ${ctx.rp.*}) would
//     hash to identical IDs and p.lambdas[id]=captured would clobber
//     the first with the second — both kwargs would then point at the
//     second lambda's body at workflow-resolve time.
func (p *Parser) captureLambdaAtPosition(fn *starlark.Function, userPos, bodyPos syntax.Position, disambiguator string) (*dag.CapturedLambda, error) {
	fileBytes, ok := p.fileBytes[userPos.Filename()]
	if !ok || fileBytes == nil {
		return nil, &dag.ParseError{
			Pos: userPos,
			Msg: fmt.Sprintf("internal: file bytes for %q not cached", userPos.Filename()),
		}
	}
	id := dag.ComputeLambdaID(fileBytes, userPos)
	if disambiguator != "" {
		id = id + ":" + disambiguator
	}
	freeVars := starlark.StringDict{}
	freeVars.Freeze()
	captured := &dag.CapturedLambda{
		ID:       id,
		Fn:       fn,
		Pos:      userPos,
		BodyPos:  bodyPos,
		FreeVars: freeVars,
	}
	p.lambdas[id] = captured
	return captured, nil
}

// captureLambdaWithArity wraps captureLambda with arity enforcement
// (D-07-05 layer 2). Used by builtinTrigger for map and idempotency_key
// lambdas — both must accept exactly one positional parameter (convention:
// req). Existing callers of captureLambda (script, if_cond, action_fn,
// etc.) accept variable arity by design and continue to use captureLambda
// directly.
//
// Arity check details:
//   - *args / **kwargs are rejected (would dilute the single-req contract).
//   - Defaulted positional (e.g. lambda req=None: ...) is rejected — Phase
//     7's lambda runs with a real req, never with the default.
//   - Plain positional with arity == expectedArity is accepted.
//
// Errors are *dag.ParseError with the lambda's Position so consultants
// land at the lambda definition.
func (p *Parser) captureLambdaWithArity(thread *starlark.Thread, kwargName string, val starlark.Value, expectedArity int) (*dag.CapturedLambda, error) {
	captured, err := p.captureLambda(thread, kwargName, val)
	if err != nil {
		return nil, err
	}
	fn := captured.Fn
	numParams := fn.NumParams()
	if numParams != expectedArity {
		return nil, &dag.ParseError{
			Pos: captured.Pos,
			Msg: fmt.Sprintf("kwarg %q lambda must accept exactly %d positional parameter(s) (convention: req); got %d",
				kwargName, expectedArity, numParams),
		}
	}
	// Reject defaulted positional: each param's default must be nil
	// (go.starlark.net signature: ParamDefault(i int) Value, returns nil
	// when the parameter has no default).
	for i := 0; i < numParams; i++ {
		if fn.ParamDefault(i) != nil {
			return nil, &dag.ParseError{
				Pos: captured.Pos,
				Msg: fmt.Sprintf("kwarg %q lambda parameter %d must not have a default value (single-positional req only)", kwargName, i),
			}
		}
	}
	if fn.HasVarargs() {
		return nil, &dag.ParseError{
			Pos: captured.Pos,
			Msg: fmt.Sprintf("kwarg %q lambda must not accept *args (single-positional req only)", kwargName),
		}
	}
	if fn.HasKwargs() {
		return nil, &dag.ParseError{
			Pos: captured.Pos,
			Msg: fmt.Sprintf("kwarg %q lambda must not accept **kwargs (single-positional req only)", kwargName),
		}
	}
	return captured, nil
}
