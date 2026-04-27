package parser

import (
	"fmt"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// validateFreeVars enforces D-19: a lambda may close over free variables
// only when they bind to module-level (top-of-file scope) names in the
// owning file. Module-level def helpers and constants are allowed; locals
// inside an enclosing def body are not.
//
// Returns a frozen StringDict snapshot of the captured free vars on success;
// returns *dag.ParseError pointing at the offending binding on failure.
//
// Note (Pitfall #5): a `def helper(x): ...` declaration at column 1 IS a
// module-level binding even though the function's body is indented; we
// inspect the BINDING position (where the name is bound), not the function's
// body or call sites. starlark.Function.FreeVar returns a starlark.Binding
// whose Pos.Col == 1 for module-level bindings.
func (p *Parser) validateFreeVars(fn *starlark.Function, ownerFile string) (starlark.StringDict, error) {
	out := starlark.StringDict{}
	for i := 0; i < fn.NumFreeVars(); i++ {
		binding, value := fn.FreeVar(i)
		if !isModuleLevelBinding(ownerFile, binding) {
			return nil, &dag.ParseError{
				Pos: binding.Pos,
				Msg: fmt.Sprintf("lambda captures non-module-level variable %q (free vars must reference module-level constants/functions only)", binding.Name),
			}
		}
		out[binding.Name] = value
	}
	out.Freeze()
	return out, nil
}

// isModuleLevelBinding returns true when the binding's source position is
// the top-of-file scope of the owning file. Top-of-file in starlark-go's
// resolver lands at column 1 — locals inside a def body have higher
// indentation (col >= 5 for `    var = ...`).
//
// Cross-file note: a binding loaded via load() points at the original
// definition in the load target. starlark-go's resolver currently exposes
// only the binding's Pos (not the load() callsite), so a load-imported
// helper's free-var attribution would point at the helper file, not the
// importing file. Phase 1's same-file rule keeps this simple — fixtures
// that exercise load() use the imported helper as a whole-builtin (e.g.
// `shared_step()`) rather than capturing it inside a lambda.
func isModuleLevelBinding(filename string, binding starlark.Binding) bool {
	return binding.Pos.Filename() == filename && binding.Pos.Col == 1
}
