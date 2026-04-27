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
// lambda-time globals have NO overlap in keys for the DSL primitive set.
// The parse-time env contains DSL primitives and (later) extension factories;
// the lambda-time env contains only the D-20 strict subset.
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
	// shadow them. The two dicts have disjoint membership for the keys
	// each is meant to host.
	for _, k := range lambdaKeys {
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

// keysOf is a tiny helper: returns the sorted key set of a StringDict.
func keysOf(d starlark.StringDict) []string {
	out := make([]string, 0, len(d))
	for k := range d {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
