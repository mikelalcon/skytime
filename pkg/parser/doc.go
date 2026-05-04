// Package parser implements Starlark→dag.Flow parsing with sandboxed load(),
// lambda capture, and reflection-based kwarg validation. May not import
// temporal.
//
// Dual-name builtins: "fail"
//
// The name "fail" is registered in TWO predeclared environments:
//
//  1. PARSE-TIME (pkg/parser/globals.go::newParseTimeGlobals): used at
//     the top-level of a .star file and inside flow()/step()/if_cond()/
//     script() bodies. fail("msg") at parse time returns a *dag.Fail
//     node that the runtime walker evaluates as a NonRetryableError.
//
//  2. LAMBDA-TIME (pkg/bridge/lambda_globals.go): the standard Starlark
//     fail() builtin (starlark.Universe["fail"]). Available inside
//     `lambda` bodies; raises a *starlark.EvalError immediately at
//     workflow execute time.
//
// Both produce the same observable surface — a NonRetryableError with
// the user's message at the .star callsite. The two predeclared envs
// are mutually exclusive (Starlark resolves names per the active env),
// so re-using the name is safe by design. See D4.2-05 + Phase 04.2
// RESEARCH §Pitfall 4.
package parser
