package bridge

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// TestLambdaTimeGlobalsLocked is the API stability gate. D-20 specifies
// EXACTLY this 20-key set; any future addition or removal MUST update this
// expected list, which forces the contributor to make a deliberate decision
// before changing the lambda-time surface.
//
// The sorted-equal pattern means a key swap fails with a clean diff.
func TestLambdaTimeGlobalsLocked(t *testing.T) {
	expected := []string{
		// Type constructors / coercions (8)
		"len", "str", "int", "float", "bool", "list", "dict", "tuple",
		// Failure (1)
		"fail",
		// Frozen-collection iteration helpers (11)
		"enumerate", "zip", "range", "sorted", "reversed",
		"min", "max", "sum", "any", "all", "abs",
	}
	require.Len(t, expected, 20, "D-20 declares exactly 20 lambda-time globals")

	actual := make([]string, 0, len(lambdaTimeGlobals))
	for k := range lambdaTimeGlobals {
		actual = append(actual, k)
	}
	sort.Strings(expected)
	sort.Strings(actual)

	require.Equal(t, expected, actual,
		"lambda-time globals changed — D-20 requires explicit decision before adding/removing keys")
}

// TestLambdaTimeGlobals_ForbiddenAbsent asserts the negative space: a
// representative set of forbidden builtins must NOT appear in
// lambdaTimeGlobals. Catches drift if a future contributor adds time/random
// without thinking.
func TestLambdaTimeGlobals_ForbiddenAbsent(t *testing.T) {
	forbidden := []string{
		"print",   // routed via thread.Print per D-21, not as a builtin
		"set",     // determinism risk; off-by-default in Starlark
		"time",    // non-deterministic
		"random",  // non-deterministic
		"getattr", // dynamic attr lookup defeats freeze audit
		"load",    // parse-time only
		"os",      // I/O escape hatch
		"open",    // I/O
		"read",    // I/O
		"write",   // I/O
		"now",     // time alias
		"uuid",    // non-deterministic
		"http",    // I/O
	}
	for _, name := range forbidden {
		t.Run(name, func(t *testing.T) {
			_, present := lambdaTimeGlobals[name]
			assert.False(t, present,
				"lambdaTimeGlobals must not contain forbidden builtin %q", name)
		})
	}
}

// TestLambdaTimeGlobals_AllValuesNonNil verifies every entry has a non-nil
// value. starlark.Universe lookup returns nil for absent names; this catches
// typos in the locked list.
func TestLambdaTimeGlobals_AllValuesNonNil(t *testing.T) {
	for k, v := range lambdaTimeGlobals {
		assert.NotNil(t, v, "lambdaTimeGlobals[%q] is nil — typo in Universe lookup?", k)
	}
}

// TestLambdaTimeGlobals_ReturnedCopy verifies LambdaTimeGlobals() returns a
// COPY — mutating the returned dict must not affect the package-private
// lambdaTimeGlobals. This is what plan 05's parser relies on when comparing
// its own parseTimeGlobals against the lambda-time set.
func TestLambdaTimeGlobals_ReturnedCopy(t *testing.T) {
	c1 := LambdaTimeGlobals()
	c1["__test_only_added_to_copy__"] = starlark.None

	c2 := LambdaTimeGlobals()
	_, present := c2["__test_only_added_to_copy__"]
	assert.False(t, present,
		"LambdaTimeGlobals() must return a copy; mutating it must not affect the next call")

	// Sanity: the second copy still has all 20 keys.
	assert.Len(t, c2, 20)
}

// TestLambdaTimeGlobals_IntegrationSanity verifies that the locked subset
// actually works as a predeclared environment for a representative lambda.
// Uses go.starlark.net's ExecFileOptions with lambdaTimeGlobals and
// successfully evaluates a small snippet that exercises sorted, len, and a
// lambda definition.
func TestLambdaTimeGlobals_IntegrationSanity(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	opts := &syntax.FileOptions{}
	src := `
result = sorted([3, 1, 2])
length = len(result)
double = lambda x: x * 2
doubled = double(7)
`
	globals, err := starlark.ExecFileOptions(opts, thread, "test.star", src, LambdaTimeGlobals())
	require.NoError(t, err)

	// Sanity-check the values landed.
	require.Contains(t, globals, "result")
	require.Contains(t, globals, "length")
	require.Contains(t, globals, "doubled")
}

// TestLambdaTimeGlobals_PrintNotPredeclared is the negative-test for D-20:
// print is NOT bound in our predeclared lambdaTimeGlobals dict. The
// language-level Universe still provides print as an intrinsic builtin (we
// cannot remove that without forking go.starlark.net), but Starlark's print
// always routes through thread.Print — so even though `print(...)` is
// callable, D-21's contract is honored by setting thread.Print at
// CallLambda time. This test pins the dict-membership invariant.
func TestLambdaTimeGlobals_PrintNotPredeclared(t *testing.T) {
	_, present := lambdaTimeGlobals["print"]
	assert.False(t, present,
		"lambdaTimeGlobals must not bind print; D-21 routes print via thread.Print, not as a predeclared builtin")
}

// TestLambdaTimeGlobals_PrintRoutesViaThread documents the D-21 contract:
// even though Universe['print'] is unavoidably callable, its output flows
// through thread.Print. This is what bridge.CallLambda relies on to
// translate print() output into logger events.
func TestLambdaTimeGlobals_PrintRoutesViaThread(t *testing.T) {
	var captured []string
	thread := &starlark.Thread{
		Name: "test",
		Print: func(_ *starlark.Thread, msg string) {
			captured = append(captured, msg)
		},
	}
	opts := &syntax.FileOptions{}
	_, err := starlark.ExecFileOptions(opts, thread, "test.star",
		`print("hello from lambda")`, LambdaTimeGlobals())
	require.NoError(t, err)
	require.Len(t, captured, 1)
	assert.Equal(t, "hello from lambda", captured[0])
}

// TestLambdaTimeGlobals_SumWorks verifies the locally-implemented `sum`
// builtin (go.starlark.net's Universe does not export `sum`, but D-20 locks
// it into the lambda-time surface).
func TestLambdaTimeGlobals_SumWorks(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	opts := &syntax.FileOptions{}
	src := `
total = sum([1, 2, 3, 4])
total_with_start = sum([1, 2, 3], 10)
empty = sum([])
floats = sum([1.0, 2.5, 3.5])
`
	globals, err := starlark.ExecFileOptions(opts, thread, "test.star", src, LambdaTimeGlobals())
	require.NoError(t, err)

	totalInt, ok := globals["total"].(starlark.Int)
	require.True(t, ok)
	gotTotal, _ := totalInt.Int64()
	assert.Equal(t, int64(10), gotTotal)

	tws, ok := globals["total_with_start"].(starlark.Int)
	require.True(t, ok)
	gotTws, _ := tws.Int64()
	assert.Equal(t, int64(16), gotTws)

	empty, ok := globals["empty"].(starlark.Int)
	require.True(t, ok)
	gotEmpty, _ := empty.Int64()
	assert.Equal(t, int64(0), gotEmpty)

	floats, ok := globals["floats"].(starlark.Float)
	require.True(t, ok)
	assert.Equal(t, 7.0, float64(floats))
}

// TestLambdaTimeGlobals_TimeNotAvailable confirms the time module is absent.
func TestLambdaTimeGlobals_TimeNotAvailable(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	opts := &syntax.FileOptions{}
	src := `t = time.now()`
	_, err := starlark.ExecFileOptions(opts, thread, "test.star", src, LambdaTimeGlobals())
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "time") || strings.Contains(err.Error(), "undefined"),
		"got: %v", err)
}

// TestFail_LambdaTime_StillRaises is the Phase 04.2 D4.2-05 regression
// guard (Pitfall 4): adding a parse-time fail() builtin to
// pkg/parser/globals.go MUST NOT alter the lambda-time fail behavior.
// The two predeclared environments are mutually exclusive — Starlark
// resolves names per the active env — so a `lambda ctx: fail("nope")`
// body uses starlark.Universe["fail"] (re-exported via lambdaTimeGlobals)
// and raises a *starlark.EvalError immediately when called.
//
// This test pins lambda-time fail by:
//  1. Compiling `f = lambda ctx: fail("nope")` against LambdaTimeGlobals().
//  2. Calling the resulting lambda via standard Starlark machinery.
//  3. Asserting the call returns *starlark.EvalError with "nope" in
//     the message.
//
// If a future change to pkg/parser/globals.go accidentally re-bound the
// lambda-time fail (e.g., by sharing the same builtin instance), the
// returned error type or message would diverge and this test would fail.
func TestFail_LambdaTime_StillRaises(t *testing.T) {
	thread := &starlark.Thread{Name: "test-init"}
	opts := &syntax.FileOptions{}
	globals, err := starlark.ExecFileOptions(opts, thread, "test.star",
		`f = lambda ctx: fail("nope")`, LambdaTimeGlobals())
	require.NoError(t, err, "init exec must compile lambda body")

	fn, ok := globals["f"].(*starlark.Function)
	require.True(t, ok, "globals[f] must be *starlark.Function; got %T", globals["f"])

	callThread := &starlark.Thread{Name: "test-call"}
	// ctx isn't used inside the lambda body, but we still pass a valid
	// dummy positional arg.
	_, callErr := starlark.Call(callThread, fn, starlark.Tuple{starlark.None}, nil)
	require.Error(t, callErr, "lambda-time fail must raise an error when called")

	// Concrete type must be *starlark.EvalError — the lambda-time fail
	// path's contract. If parse-time fail wiring leaks, the type would
	// drift to *dag.ParseError or similar.
	var evalErr *starlark.EvalError
	require.ErrorAs(t, callErr, &evalErr,
		"lambda-time fail must raise *starlark.EvalError; got %T: %v", callErr, callErr)
	assert.Contains(t, evalErr.Error(), "nope",
		"error message must include the user's fail() argument")
}

// TestTriggerTimeGlobalsLocked is the API stability gate for the Phase-7
// trigger-time predeclared env (D-07-01). EXACTLY 22 keys: 20 from the
// locked lambdaTimeGlobals + json (starlarkjson.Module) + time
// (starlarktime.Module). Any future addition or removal MUST update this
// test, forcing a deliberate decision before changing the surface.
func TestTriggerTimeGlobalsLocked(t *testing.T) {
	require.Len(t, triggerTimeGlobals, 22,
		"D-07-01 declares exactly 22 trigger-time globals (lambdaTimeGlobals + json + time)")

	// Every lambdaTimeGlobals key must be present with the same value
	// pointer. Catches accidental rebinding.
	for k, v := range lambdaTimeGlobals {
		tv, ok := triggerTimeGlobals[k]
		require.True(t, ok,
			"lambdaTimeGlobals key %q missing from triggerTimeGlobals", k)
		assert.Equal(t, v, tv,
			"triggerTimeGlobals[%q] differs from lambdaTimeGlobals[%q]", k, k)
	}

	// json + time must be the canonical Module values from go.starlark.net.
	jsonVal, ok := triggerTimeGlobals["json"]
	require.True(t, ok, "triggerTimeGlobals must contain key %q", "json")
	require.NotNil(t, jsonVal, "triggerTimeGlobals[json] must be non-nil")

	timeVal, ok := triggerTimeGlobals["time"]
	require.True(t, ok, "triggerTimeGlobals must contain key %q", "time")
	require.NotNil(t, timeVal, "triggerTimeGlobals[time] must be non-nil")
}

// TestTriggerTimeGlobals_ReturnedCopy verifies TriggerTimeGlobals() returns
// a COPY — mutating the returned dict must not affect the package-private
// triggerTimeGlobals or subsequent calls.
func TestTriggerTimeGlobals_ReturnedCopy(t *testing.T) {
	c1 := TriggerTimeGlobals()
	c1["__test_only_added_to_copy__"] = starlark.None

	c2 := TriggerTimeGlobals()
	_, present := c2["__test_only_added_to_copy__"]
	assert.False(t, present,
		"TriggerTimeGlobals() must return a copy; mutating it must not affect the next call")

	// Sanity: the second copy still has all 22 keys.
	assert.Len(t, c2, 22)
}

// TestTriggerTimeGlobals_JSONEncodeWorks proves json.Module is wired
// correctly: a tiny script `result = json.encode({"a": 1})` evaluates
// against TriggerTimeGlobals() and produces the expected JSON string.
func TestTriggerTimeGlobals_JSONEncodeWorks(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	opts := &syntax.FileOptions{}
	src := `result = json.encode({"a": 1})`
	globals, err := starlark.ExecFileOptions(opts, thread, "test.star", src, TriggerTimeGlobals())
	require.NoError(t, err)
	resVal, ok := globals["result"].(starlark.String)
	require.True(t, ok, "json.encode must return a starlark.String; got %T", globals["result"])
	assert.Equal(t, `{"a":1}`, string(resVal),
		"json.encode({\"a\":1}) must produce the canonical JSON form")
}

// TestTriggerTimeGlobals_TimeNowCallable proves time.Module is wired
// correctly: time.now() is callable. The returned value is non-deterministic
// (wall clock), so we don't assert on it — just that the call succeeds.
func TestTriggerTimeGlobals_TimeNowCallable(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	opts := &syntax.FileOptions{}
	src := `result = time.now()`
	globals, err := starlark.ExecFileOptions(opts, thread, "test.star", src, TriggerTimeGlobals())
	require.NoError(t, err, "time.now() must be callable in trigger-time env")
	require.Contains(t, globals, "result")
	assert.NotNil(t, globals["result"])
}

// TestTriggerTimeGlobals_LambdaSubsetExact catches drift if a future
// contributor changes one of the 20 lambdaTimeGlobals entries without
// updating triggerTimeGlobals (or vice versa). Each lambdaTimeGlobals
// key must appear in triggerTimeGlobals with the same value pointer.
func TestTriggerTimeGlobals_LambdaSubsetExact(t *testing.T) {
	for k, v := range lambdaTimeGlobals {
		tv, ok := triggerTimeGlobals[k]
		require.True(t, ok,
			"lambdaTimeGlobals key %q missing from triggerTimeGlobals", k)
		assert.Equal(t, v, tv,
			"triggerTimeGlobals[%q] differs from lambdaTimeGlobals[%q]", k, k)
	}
}
