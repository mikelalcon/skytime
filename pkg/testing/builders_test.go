package testing

import (
	"strings"
	"testing"

	"go.starlark.net/starlark"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/bridge"
)

// TestMockLambdaEnv_IsLambdaTimePlusBuilders is THE named test for
// D5-C2 (cited in VALIDATION.md per-task map).
func TestMockLambdaEnv_IsLambdaTimePlusBuilders(t *testing.T) {
	base := bridge.LambdaTimeGlobals()
	mock := MockLambdaGlobals()

	// mock = base ∪ {ok, err, nonretryable}
	for k := range base {
		_, ok := mock[k]
		assert.True(t, ok, "MockLambdaGlobals missing base key %q", k)
	}
	for _, k := range []string{"ok", "err", "nonretryable"} {
		_, ok := mock[k]
		assert.True(t, ok, "MockLambdaGlobals missing builder %q", k)
	}
	assert.Equal(t, len(base)+3, len(mock), "MockLambdaGlobals should be base ∪ {ok,err,nonretryable} exactly")
}

// TestMockLambdaEnv_DoesNotMutateProduction — pin D1-20 invariant.
// Calling MockLambdaGlobals() must NOT shrink/grow the production
// lambdaTimeGlobals dict.
func TestMockLambdaEnv_DoesNotMutateProduction(t *testing.T) {
	snapshot := bridge.LambdaTimeGlobals()
	sizeBefore := len(snapshot)
	_ = MockLambdaGlobals()
	sizeAfter := len(bridge.LambdaTimeGlobals())
	assert.Equal(t, sizeBefore, sizeAfter, "MockLambdaGlobals must not mutate production lambda env")
}

// TestBuilders_OkCarriesValue evaluates `ok(value={"login":"octocat"})`
// in a fresh thread under MockLambdaGlobals and asserts the returned
// MockOk.Value round-trips through bridge.FromStarlarkValue.
func TestBuilders_OkCarriesValue(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	src := `ok(value={"login":"octocat"})`
	v, err := starlark.Eval(thread, "test.star", src, MockLambdaGlobals())
	require.NoError(t, err)
	res, ok := AsMockResult(v)
	require.True(t, ok)
	okRes, isOk := res.(MockOk)
	require.True(t, isOk)
	require.NotNil(t, okRes.Value)

	goVal, gErr := bridge.FromStarlarkValue(okRes.Value)
	require.NoError(t, gErr)
	m, isMap := goVal.(map[string]any)
	require.True(t, isMap)
	assert.Equal(t, "octocat", m["login"])
}

// TestBuilders_OkOmittedValue — ok() without value= returns MockOk
// whose Value is starlark.None.
func TestBuilders_OkOmittedValue(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	v, err := starlark.Eval(thread, "test.star", `ok()`, MockLambdaGlobals())
	require.NoError(t, err)
	res, ok := AsMockResult(v)
	require.True(t, ok)
	okRes, isOk := res.(MockOk)
	require.True(t, isOk)
	assert.Equal(t, starlark.None, okRes.Value)
}

// TestBuilders_ErrCarriesMsg — err(msg="transient") returns MockErr.
func TestBuilders_ErrCarriesMsg(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	v, err := starlark.Eval(thread, "test.star", `err(msg="transient")`, MockLambdaGlobals())
	require.NoError(t, err)
	res, _ := AsMockResult(v)
	e, ok := res.(MockErr)
	require.True(t, ok)
	assert.Equal(t, "transient", e.Msg)
}

// TestBuilders_ErrEmptyMsg — err() without msg returns MockErr with
// empty Msg.
func TestBuilders_ErrEmptyMsg(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	v, err := starlark.Eval(thread, "test.star", `err()`, MockLambdaGlobals())
	require.NoError(t, err)
	res, _ := AsMockResult(v)
	e, ok := res.(MockErr)
	require.True(t, ok)
	assert.Equal(t, "", e.Msg)
}

// TestBuilders_ErrWrongTypeRejected — err(msg=42) raises a Starlark
// EvalError citing wrong-type.
func TestBuilders_ErrWrongTypeRejected(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	_, err := starlark.Eval(thread, "test.star", `err(msg=42)`, MockLambdaGlobals())
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "string") || strings.Contains(err.Error(), "msg"),
		"expected wrong-type error mentioning msg/string, got: %s", err)
}

// TestBuilders_NonRetryableCarriesMsg — nonretryable(msg="bad input")
// returns MockNonRetryable.
func TestBuilders_NonRetryableCarriesMsg(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	v, err := starlark.Eval(thread, "test.star", `nonretryable(msg="bad input")`, MockLambdaGlobals())
	require.NoError(t, err)
	res, _ := AsMockResult(v)
	nr, ok := res.(MockNonRetryable)
	require.True(t, ok)
	assert.Equal(t, "bad input", nr.Msg)
}

// TestBuilders_AsMockResultRejectsNonSentinel — AsMockResult must
// return ok=false for plain Starlark values (None, ints, strings).
// This is the load-bearing piece of D5-C4 ("mock must return
// ok/err/nonretryable") — the router uses ok=false to flag bad mocks.
func TestBuilders_AsMockResultRejectsNonSentinel(t *testing.T) {
	cases := []struct {
		name string
		v    starlark.Value
	}{
		{"none", starlark.None},
		{"int", starlark.MakeInt(42)},
		{"string", starlark.String("nope")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := AsMockResult(tc.v)
			assert.False(t, ok)
		})
	}
}

// TestBuilders_PositionalArgsRejected — all three builders refuse
// positional args. Forces explicit kwarg style for readability.
func TestBuilders_PositionalArgsRejected(t *testing.T) {
	for _, src := range []string{
		`ok({"x":1})`,
		`err("transient")`,
		`nonretryable("bad")`,
	} {
		thread := &starlark.Thread{Name: "test"}
		_, err := starlark.Eval(thread, "test.star", src, MockLambdaGlobals())
		require.Error(t, err, "positional-arg call %q must error", src)
	}
}
