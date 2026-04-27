package parser

import (
	"go.starlark.net/starlark"
)

// makeLoad is wired in task 3. Stubbed in task 1 to a no-op resolver that
// always errors: load() is unsupported until task 3 lands. The stub keeps
// the package compiling so task 1's scaffolding tests run.
func (p *Parser) makeLoad() func(*starlark.Thread, string) (starlark.StringDict, error) {
	return func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
		return nil, &loadStubErr{module: module}
	}
}

// loadStubErr is the placeholder returned by the task-1 stub. Replaced in
// task 3 with a real *dag.ParseError-returning resolver.
type loadStubErr struct {
	module string
}

func (e *loadStubErr) Error() string { return "load not yet wired (task 3): " + e.module }
