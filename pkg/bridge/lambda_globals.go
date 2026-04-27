package bridge

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// sumBuiltin implements `sum(iterable, start=0)` — standard Python/Starlark
// semantics. go.starlark.net's Universe does NOT export `sum` (verified
// against the published runtime), but D-20 locks `sum` into the lambda-time
// surface so we implement it locally as a deterministic *starlark.Builtin.
//
// Accepts: any iterable yielding starlark.Int and/or starlark.Float; optional
// start kwarg (also Int or Float; defaults to Int(0)).
// Returns: Int when all addends and start are Int; Float when any addend or
// start is Float (matches Python promotion rules).
var sumBuiltin = starlark.NewBuiltin("sum", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var iter starlark.Iterable
	var start starlark.Value = starlark.MakeInt(0)
	if err := starlark.UnpackArgs("sum", args, kwargs, "iterable", &iter, "start?", &start); err != nil {
		return nil, err
	}

	acc := start
	it := iter.Iterate()
	defer it.Done()
	var v starlark.Value
	for it.Next(&v) {
		next, err := starlark.Binary(syntax.PLUS, acc, v)
		if err != nil {
			return nil, fmt.Errorf("sum: %w", err)
		}
		acc = next
	}
	return acc, nil
})

// lambdaTimeGlobals is the STRICT subset of Starlark Universe builtins
// available inside lambdas at workflow execution time (D-20). LOCKED IN
// PHASE 1; never expanded without an explicit decision logged in PROJECT.md
// Key Decisions and an update to TestLambdaTimeGlobalsLocked.
//
// The 20 entries below are the totality of D-20:
//
//   Type constructors / coercions (8): len, str, int, float, bool, list, dict, tuple
//   Failure (1):                       fail
//   Iteration helpers (11):            enumerate, zip, range, sorted, reversed,
//                                      min, max, sum, any, all, abs
//
// EXPLICITLY EXCLUDED by D-20: time.*, random.*, all I/O, getattr (dynamic
// lookup), set() (Starlark's built-in set is off-by-default and stays off),
// load() (parse-time only). print() is NOT in the dict — D-21 routes it via
// thread.Print at CallLambda time so any print(...) call becomes a logger
// event instead of a builtin name lookup.
//
// Comparison/arithmetic operators and struct attribute access are language
// primitives — they do not need to appear in the predeclared dict.
var lambdaTimeGlobals = func() starlark.StringDict {
	sd := starlark.StringDict{
		// Type constructors / coercions
		"len":   starlark.Universe["len"],
		"str":   starlark.Universe["str"],
		"int":   starlark.Universe["int"],
		"float": starlark.Universe["float"],
		"bool":  starlark.Universe["bool"],
		"list":  starlark.Universe["list"],
		"dict":  starlark.Universe["dict"],
		"tuple": starlark.Universe["tuple"],

		// Short-circuit failure
		"fail": starlark.Universe["fail"],

		// Frozen-collection iteration helpers
		"enumerate": starlark.Universe["enumerate"],
		"zip":       starlark.Universe["zip"],
		"range":     starlark.Universe["range"],
		"sorted":    starlark.Universe["sorted"],
		"reversed":  starlark.Universe["reversed"],
		"min":       starlark.Universe["min"],
		"max":       starlark.Universe["max"],
		// sum is NOT in starlark.Universe — implemented locally as sumBuiltin.
		"sum": sumBuiltin,
		"any":       starlark.Universe["any"],
		"all":       starlark.Universe["all"],
		"abs":       starlark.Universe["abs"],
	}
	// Freeze the dict; values pulled from starlark.Universe are already frozen
	// at the language level (Universe is initialized once at module init), so
	// this only locks the dict's own membership.
	sd.Freeze()
	return sd
}()

// LambdaTimeGlobals returns a copy of the locked lambda-time globals.
// Plan 05 (parser) calls this to assert that the parser's parse-time globals
// are a strict superset of (or otherwise distinct from) the lambda-time
// globals, enforcing PARSE-03's two-environment split.
//
// Phase 3's interpreter does not need to call this — bridge.CallLambda
// uses the package-private lambdaTimeGlobals directly.
//
// Returns a fresh copy each call so callers can mutate it freely without
// affecting the locked source of truth.
func LambdaTimeGlobals() starlark.StringDict {
	out := make(starlark.StringDict, len(lambdaTimeGlobals))
	for k, v := range lambdaTimeGlobals {
		out[k] = v
	}
	return out
}
