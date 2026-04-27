package parser

import (
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// builtinFlow / builtinStep / etc. are stubs in task 1 — task 2 wires the
// six DSL primitives to construct *dag.* node values and populate the
// parser session's flow/lambda maps.
//
// The stubs return starlark.None so the package compiles and task 1's
// scaffolding tests (TestNewParser_*, TestResolveAllowLambdaIsSet,
// TestParseTimeGlobals_NakedPrimitives, TestParseAndLambdaGlobalsAreDistinct,
// etc.) can run without reaching real DSL behavior. Any .star fixture
// referencing these stubs will not produce a usable Flow until task 2 lands.

func (p *Parser) builtinFlow(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

func (p *Parser) builtinStep(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

func (p *Parser) builtinIfCond(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

func (p *Parser) builtinScript(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

func (p *Parser) builtinForEachParallel(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

func (p *Parser) builtinCallFlow(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

// callerPosition extracts the .star call-site position from the thread's
// call stack. CRITICAL (Pitfall #3): use thread.CallFrame(1).Pos, NOT
// fn.Position() — the latter is the def site of the builtin, which is
// always the same for every call to step()/flow()/etc. We want the source
// location where the consultant *invoked* the primitive.
//
// Returns the zero-value syntax.Position if the call stack is too shallow
// (which should be impossible during normal parse). The zero position is
// IsValid() == false so D-04 error formatting falls back to message-only.
func callerPosition(thread *starlark.Thread) syntax.Position {
	if thread.CallStackDepth() < 2 {
		return syntax.Position{}
	}
	return thread.CallFrame(1).Pos
}
