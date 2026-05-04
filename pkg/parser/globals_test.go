package parser

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/bridge"
)

// TestParseTimeGlobals_NakedPrimitives pins PARSE-01: all six DSL primitives
// are bare keys in the parse-time globals dict (NOT namespaced under
// "skytime." or "dsl.") and each value is a *starlark.Builtin.
func TestParseTimeGlobals_NakedPrimitives(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)

	g, err := newParseTimeGlobals(p, &starlark.Thread{Name: "test"})
	require.NoError(t, err)

	expected := []string{
		"flow", "step", "if_cond", "script", "for_each_parallel", "call_flow",
	}
	for _, name := range expected {
		val, ok := g[name]
		require.True(t, ok, "PARSE-01: %q must be a top-level (naked) global, no namespacing", name)
		_, isBuiltin := val.(*starlark.Builtin)
		assert.True(t, isBuiltin, "%q must be a *starlark.Builtin, got %T", name, val)
	}

	// PARSE-01 negative: no namespaced versions.
	for _, badName := range []string{"skytime.flow", "dsl.flow", "skytime", "dsl"} {
		_, present := g[badName]
		assert.False(t, present, "PARSE-01 forbids namespaced primitives; found %q", badName)
	}
}

// TestParseAndLambdaGlobalsAreDistinct pins PARSE-03: parse-time globals and
// lambda-time globals have NO overlap in keys for the DSL primitive set,
// EXCEPT for the deliberately dual-named `fail` builtin (D4.2-05). The
// parse-time env contains DSL primitives and (later) extension factories;
// the lambda-time env contains only the D-20 strict subset.
//
// `fail` is the one allowed overlap: top-level `fail("msg")` emits a
// *dag.Fail node at parse time; lambda-time `fail("msg")` raises a
// *starlark.EvalError at runtime. Both produce the same observable
// surface (NonRetryableErr at the .star callsite); the two predeclared
// envs are mutually exclusive (top-level vs inside-lambda body) so
// Starlark resolves correctly per call site. See pkg/parser/doc.go.
func TestParseAndLambdaGlobalsAreDistinct(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)

	parseG, err := newParseTimeGlobals(p, &starlark.Thread{Name: "test"})
	require.NoError(t, err)
	lambdaG := bridge.LambdaTimeGlobals()

	require.NotEmpty(t, parseG, "parse-time globals must contain the 6 DSL primitives at minimum")
	require.NotEmpty(t, lambdaG, "lambda-time globals must contain the D-20 subset")

	parseKeys := keysOf(parseG)
	lambdaKeys := keysOf(lambdaG)

	// The 6 DSL primitives appear ONLY in parse-time globals.
	dslPrimitives := []string{
		"flow", "step", "if_cond", "script", "for_each_parallel", "call_flow",
	}
	for _, k := range dslPrimitives {
		assert.Contains(t, parseG, k, "parse-time globals must include %q", k)
		_, inLambda := lambdaG[k]
		assert.False(t, inLambda, "lambda-time globals MUST NOT include %q (PARSE-03 / D-20)", k)
	}

	// Conversely, every D-20 key (e.g. "len", "range") is NOT in parse-time
	// globals — those are intrinsics Starlark provides everywhere; we don't
	// shadow them. EXCEPT `fail` (D4.2-05 dual-name): both environments
	// register `fail` under the same name; the parse-time entry returns a
	// *nodeValue wrapping *dag.Fail, the lambda-time entry is the standard
	// Starlark fail builtin from starlark.Universe.
	for _, k := range lambdaKeys {
		if k == "fail" {
			continue // documented dual-name exception
		}
		_, inParse := parseG[k]
		assert.False(t, inParse,
			"parse-time globals must not include lambda-time key %q (PARSE-03 distinctness)", k)
	}

	t.Logf("parse-time keys: %v", parseKeys)
	t.Logf("lambda-time keys: %v", lambdaKeys)
}

// TestParseTimeGlobals_ExtensionRegistered: a registered extension is bound
// in the parse-time globals under its Name(). Initialize must return
// HasAttrs (D-08); minimalExtension returns starlark.None which fails the
// gate — exercise that path.
func TestParseTimeGlobals_NotAttributeBearingRejected(t *testing.T) {
	p, err := NewParser(WithExtensions(&minimalExtension{name: "demo"}))
	require.NoError(t, err)

	_, gerr := newParseTimeGlobals(p, &starlark.Thread{Name: "test"})
	require.Error(t, gerr, "Initialize returning starlark.None should fail the HasAttrs gate")
	assert.Contains(t, gerr.Error(), "not attribute-bearing")
}

// TestRegister_InvalidatesGlobalsCache verifies that calling Register()
// AFTER NewParser invalidates the cached parseTimeGlobals — the lazy-init
// path must rebuild on next parse to include the new extension.
func TestRegister_InvalidatesGlobalsCache(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)

	// Manually populate the cache to a non-nil sentinel.
	p.parseTimeGlobals = starlark.StringDict{"sentinel": starlark.None}
	require.NotNil(t, p.parseTimeGlobals)

	// Register an extension. Even though our minimal extension returns a
	// non-attribute-bearing value (so a real parse would fail at globals
	// init), Register itself must succeed and clear the cache.
	err = p.Register(&minimalExtension{name: "demo2"})
	require.NoError(t, err)
	assert.Nil(t, p.parseTimeGlobals,
		"Register() must invalidate parseTimeGlobals so next parse rebuilds with the new extension")
}

// TestFailBuiltinDualEnv pins D4.2-05: the name `fail` is registered in
// BOTH the parse-time and lambda-time predeclared environments, but as
// DISTINCT *starlark.Builtin entries with different underlying Func
// closures. Both share the surface name "fail" (per Builtin.Name()) so
// Starlark resolution finds the right builtin in each scope, but the
// parse-time entry returns a *nodeValue while the lambda-time entry
// delegates to starlark.Universe and raises EvalError.
func TestFailBuiltinDualEnv(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)

	parseG, err := newParseTimeGlobals(p, &starlark.Thread{Name: "test"})
	require.NoError(t, err)
	lambdaG := bridge.LambdaTimeGlobals()

	parseFail, ok := parseG["fail"]
	require.True(t, ok, "parse-time globals must register fail")
	parseBuiltin, ok := parseFail.(*starlark.Builtin)
	require.True(t, ok, "parse-time fail must be a *starlark.Builtin; got %T", parseFail)
	assert.Equal(t, "fail", parseBuiltin.Name())

	lambdaFail, ok := lambdaG["fail"]
	require.True(t, ok, "lambda-time globals must register fail")
	lambdaBuiltin, ok := lambdaFail.(*starlark.Builtin)
	require.True(t, ok, "lambda-time fail must be a *starlark.Builtin; got %T", lambdaFail)
	assert.Equal(t, "fail", lambdaBuiltin.Name())

	// Distinct underlying functions — pointer comparison via the
	// CallInternal method's identity is the cleanest signal that the
	// two builtins are not the same object. Compare *Builtin pointers.
	assert.NotSame(t, parseBuiltin, lambdaBuiltin,
		"parse-time and lambda-time fail must be distinct *starlark.Builtin instances")
}

// keysOf is a tiny helper: returns the sorted key set of a StringDict.
func keysOf(d starlark.StringDict) []string {
	out := make([]string, 0, len(d))
	for k := range d {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
