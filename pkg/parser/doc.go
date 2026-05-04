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
//
// Example — dual fail() call sites:
//
// Top-level fail() (parse-time builtin → emits *dag.Fail node):
//
//	flow(
//	    name = "guard",
//	    inputs = {"user_id": "string"},
//	    steps = [
//	        if_cond(
//	            cond = lambda ctx: ctx.user_id == "",
//	            then = [fail("user_id is required")],          // <-- PARSE-TIME fail()
//	            else_ = [step(action = api.fetch_user(id = "${ctx.user_id}"))],
//	        ),
//	    ],
//	)
//
// Lambda-time fail() (starlark.Universe → raises *starlark.EvalError):
//
//	flow(
//	    name = "guard",
//	    inputs = {"user_id": "string"},
//	    steps = [
//	        script(
//	            id = "validate",
//	            fn = lambda ctx: (
//	                {"ok": True} if ctx.user_id != ""
//	                else fail("user_id is required")            // <-- LAMBDA-TIME fail()
//	            ),
//	            output_alias = "v",
//	        ),
//	    ],
//	)
//
// Both produce the same observable surface: a NonRetryableError with
// the message at the .star callsite. Choose by location:
//
//   - Top-level: when the failure is structural to the flow shape
//     (procedural-guard pattern, expression-mode terminator, etc.)
//   - Lambda-time: when the failure is data-dependent inside a
//     lambda body (validation logic, conditional raise, etc.)
//
// Top-level fail() supports `${ctx.expr}` interpolation in its
// message (via the D4.1-01 desugarer); lambda-time fail() takes a
// literal string at the call site (interpolation must be done by
// string concatenation: fail("missing " + ctx.repo)).
package parser
