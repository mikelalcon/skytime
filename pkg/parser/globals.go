package parser

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarktest"
)

// newParseTimeGlobals builds the parse-time predeclared environment.
//
// The returned StringDict contains:
//   - The six naked DSL primitives (PARSE-01: NOT namespaced — `flow(...)`
//     not `skytime.flow(...)`): flow, step, if_cond, script,
//     for_each_parallel, call_flow.
//   - One entry per registered extension, keyed by Extension.Name(). The
//     value is whatever Initialize returned — typically a *starlarkstruct.Module
//     whose attributes are the extension's operation factories.
//
// PARSE-03 / D-20: this dict is INTENTIONALLY DISTINCT from
// bridge.LambdaTimeGlobals(). The parser's environment is the richer one
// (load(), extension factories, DSL primitives); lambda-time uses a strict
// 20-key subset that runs at workflow execute time.
//
// CRITICAL LIFECYCLE (D-08): Extension.Initialize is called ONCE per parser
// at globals-build time — NOT once per Starlark invocation. The returned
// starlark.Value (typically *starlarkstruct.Module) is bound directly as the
// global keyed by ext.Name(). The .star author writes:
//
//	gh = github.endpoint("admin")               # attribute lookup + call → sub-Module
//	gh.create_issue(repo="x", title="y")        # attribute lookup + call → *dag.ActionRef
//
// `github` is a namespace value with attributes; `endpoint` is one such
// attribute (a *starlark.Builtin); the call returns a credential-aware
// sub-Module whose attributes (`create_issue`, etc.) close over the credential
// ID. This model — instead of treating `github(...)` as the factory call —
// matches the user authoring example in CONTEXT.md D-08.
//
// The Initialize result MUST satisfy starlark.HasAttrs (so `<name>.<op>`
// works at parse time). We enforce this with a clear error rather than letting
// Starlark surface "value has no attributes" later.
func newParseTimeGlobals(p *Parser, thread *starlark.Thread) (starlark.StringDict, error) {
	g := starlark.StringDict{
		"flow":              starlark.NewBuiltin("flow", p.builtinFlow),
		"step":              starlark.NewBuiltin("step", p.builtinStep),
		"if_cond":           starlark.NewBuiltin("if_cond", p.builtinIfCond),
		"script":            starlark.NewBuiltin("script", p.builtinScript),
		"for_each_parallel": starlark.NewBuiltin("for_each_parallel", p.builtinForEachParallel),
		"call_flow":         starlark.NewBuiltin("call_flow", p.builtinCallFlow),

		// D4.2-02: top-level result(value={...}) emits *dag.Result. Only
		// legal as the LAST node of an expression-mode if_cond branch
		// (validation lives in plan 04.2-03 finalize pass).
		"result": starlark.NewBuiltin("result", p.builtinResult),

		// D4.2-05: top-level fail("msg") emits *dag.Fail. SAME NAME as
		// the lambda-time fail (pkg/bridge/lambda_globals.go:73 →
		// starlark.Universe["fail"]) — predeclared envs are mutually
		// exclusive (top-level vs inside-lambda body), so Starlark
		// resolves correctly. Both produce the same observable surface
		// (NonRetryableErr at the .star callsite). See pkg/parser/doc.go
		// for full dual-semantics documentation.
		"fail": starlark.NewBuiltin("fail", p.builtinFail),
	}

	for name, ext := range p.registry.All() {
		modVal, err := ext.Initialize(thread, nil)
		if err != nil {
			return nil, fmt.Errorf("initialize extension %q: %w", name, err)
		}
		// HasAttrs gate: D-08 requires the extension namespace value to be
		// attribute-bearing. starlark.None and similar non-attribute values
		// would break `<name>.<op>` at parse time with a confusing error;
		// fail at registration with a clear message instead.
		if _, ok := modVal.(starlark.HasAttrs); !ok {
			return nil, fmt.Errorf(
				"extension %q: Initialize returned %s which is not attribute-bearing "+
					"(must be *starlarkstruct.Module or any starlark.HasAttrs per D-08)",
				name, modVal.Type())
		}
		g[name] = modVal
	}

	// Phase 5 (D5-A3 + RESEARCH Investigation 9): inject the `tester`
	// namespace value when the parser is in test mode. The builder
	// closure was wired via parser.WithTestModule (Plan 01) and is
	// supplied by pkg/cli/test.go (Plan 06) so pkg/parser does not
	// import pkg/testing — cleanest cycle break.
	//
	// Plan 04 additionally injects starlarktest.LoadAssertModule()
	// globals here.
	if p.testMode {
		if p.testModuleBuilder == nil {
			return nil, fmt.Errorf("WithTestMode set but WithTestModule was not provided; both are required")
		}
		if _, exists := g["tester"]; exists {
			return nil, fmt.Errorf("test-mode global collision: %q", "tester")
		}
		g["tester"] = p.testModuleBuilder(p, thread)

		// Phase 5 D5-C2: mock_fn lambda bodies reference
		// ok/err/nonretryable as free variables. Starlark's resolver
		// binds free variables at parse-of-file time, so those names
		// MUST be visible in the parse-time predeclared env even
		// though the builders are only INVOKED at workflow execute
		// time. The closures themselves are stateless wrappers; they
		// produce mockResultValue sentinels that the router unwraps.
		//
		// Production parses (no test mode) NEVER see these names —
		// the production lambda env (D1-20 lambdaTimeGlobals) is
		// unchanged.
		//
		// The builders live in pkg/testing.MockLambdaGlobals(); we
		// pull them via WithTestPredeclared which avoids a parser →
		// testing import cycle (cli/test.go wires it).
		for name, val := range p.testPredeclared {
			if _, collide := g[name]; collide {
				return nil, fmt.Errorf("test-mode global collision: %q", name)
			}
			g[name] = val
		}

		// Phase 5 Plan 04 D5-F1 + TEST-05: inject assert.* surfaces
		// from go.starlark.net/starlarktest. Failures land on the
		// active starlarktest.Reporter, which the runner
		// (pkg/testing/reporter.go::runOneTest) binds to the
		// per-subtest *testing.T.
		//
		// Library default: assert.* accumulates failures within a
		// single def test_*() (D5-F2). Reporter is per-thread, set
		// per subtest; failures land on the active subtest's
		// *testing.T.Error.
		assertGlobals, assertErr := starlarktest.LoadAssertModule()
		if assertErr != nil {
			return nil, fmt.Errorf("starlarktest.LoadAssertModule: %w", assertErr)
		}
		for name, val := range assertGlobals {
			if _, collide := g[name]; collide {
				return nil, fmt.Errorf("test-mode global collision: %q (parse-time global already defined)", name)
			}
			g[name] = val
		}
	}
	return g, nil
}
