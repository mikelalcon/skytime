package testing

import (
	"fmt"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/bridge"
)

// MockResult is the sealed sum produced by ok/err/nonretryable builders
// (D5-C2). The router type-switches on the concrete type.
//
// The seal is an unexported method; downstream packages cannot fabricate
// a new kind by accident. Adding a kind requires editing this file.
type MockResult interface{ isMockResult() }

// MockOk carries the raw Starlark value the lambda passed to
// ok(value=...). The router converts the value to map[string]any via
// bridge.FromStarlarkValue and assembles MockOperationOutput before
// emitting OkResult.
type MockOk struct{ Value starlark.Value }

func (MockOk) isMockResult() {}

// MockErr is a retryable error: the workflow's RetryPolicy fires and
// the next activity attempt re-invokes the mock callback with
// attempt+1.
type MockErr struct{ Msg string }

func (MockErr) isMockResult() {}

// MockNonRetryable is a non-retryable error: the workflow fails
// immediately. The router wraps the message with extension.ErrNonRetryable
// so the activity-side classification matches the production 4xx path.
type MockNonRetryable struct{ Msg string }

func (MockNonRetryable) isMockResult() {}

// builtinOk implements ok(value=...). value is optional; if omitted,
// MockOk.Value is starlark.None and the router emits an empty
// MockOperationOutput.
func builtinOk(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("ok: positional args not supported; use ok(value=...)")
	}
	var value starlark.Value = starlark.None
	if err := starlark.UnpackArgs("ok", nil, kwargs, "value?", &value); err != nil {
		return nil, err
	}
	return mockResultValue{Inner: MockOk{Value: value}}, nil
}

// builtinErr implements err(msg="..."). msg defaults to "" — the
// router surfaces this as a retryable error wrapping fmt.Errorf("%s", msg)
// (which Temporal's RetryPolicy treats as retryable).
func builtinErr(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("err: positional args not supported; use err(msg=...)")
	}
	var msg string
	if err := starlark.UnpackArgs("err", nil, kwargs, "msg?", &msg); err != nil {
		return nil, err
	}
	return mockResultValue{Inner: MockErr{Msg: msg}}, nil
}

// builtinNonRetryable implements nonretryable(msg="..."). Same surface
// as err but the router wraps with extension.ErrNonRetryable so the
// workflow fails immediately.
func builtinNonRetryable(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("nonretryable: positional args not supported; use nonretryable(msg=...)")
	}
	var msg string
	if err := starlark.UnpackArgs("nonretryable", nil, kwargs, "msg?", &msg); err != nil {
		return nil, err
	}
	return mockResultValue{Inner: MockNonRetryable{Msg: msg}}, nil
}

// mockResultValue wraps a MockResult so it can flow as a starlark.Value
// (the lambda's return value). The router inspects via AsMockResult.
type mockResultValue struct{ Inner MockResult }

func (m mockResultValue) String() string        { return fmt.Sprintf("mock:%T", m.Inner) }
func (m mockResultValue) Type() string          { return "mock_result" }
func (m mockResultValue) Freeze()               {}
func (m mockResultValue) Truth() starlark.Bool  { return starlark.True }
func (m mockResultValue) Hash() (uint32, error) { return 0, fmt.Errorf("mock_result is not hashable") }

// MockLambdaGlobals returns a StringDict equal to bridge.LambdaTimeGlobals()
// extended with ok/err/nonretryable. The production set
// (bridge.LambdaTimeGlobals) is NOT mutated; this returns a fresh dict
// per call. D5-C2 + D1-20 invariant.
func MockLambdaGlobals() starlark.StringDict {
	base := bridge.LambdaTimeGlobals()
	out := make(starlark.StringDict, len(base)+3)
	for k, v := range base {
		out[k] = v
	}
	out["ok"] = starlark.NewBuiltin("ok", builtinOk)
	out["err"] = starlark.NewBuiltin("err", builtinErr)
	out["nonretryable"] = starlark.NewBuiltin("nonretryable", builtinNonRetryable)
	return out
}

// AsMockResult unwraps the sentinel produced by ok/err/nonretryable.
// Returns ok=false if v is not a mockResultValue (the router uses this
// to enforce D5-C4: None / wrong return → NonRetryableErr "mock must
// return ok/err/nonretryable").
func AsMockResult(v starlark.Value) (MockResult, bool) {
	mv, ok := v.(mockResultValue)
	if !ok {
		return nil, false
	}
	return mv.Inner, true
}

// MockLambdaParseTimeBuilders returns the {ok, err, nonretryable}
// builder triple as a starlark.StringDict suitable for the parser's
// WithTestPredeclared option. Wiring rationale:
//
// Starlark's resolver binds free variables in lambda bodies AT
// PARSE-OF-FILE TIME. mock_fn = lambda kwargs, attempt: ok(value={...})
// references `ok` as a free variable — the resolver checks the
// predeclared dict for `ok` and errors with "undefined: ok" if it's
// missing.
//
// At workflow-execute time the same closure runs against
// MockLambdaGlobals() (which includes the same builders, so behavior
// is identical). Splitting parse-time and runtime registration is
// what keeps the production lambda env (D1-20) untouched: production
// parses don't pass these builders to the parser.
//
// Returns a fresh dict per call so callers can extend it without
// affecting other parsers.
func MockLambdaParseTimeBuilders() starlark.StringDict {
	return starlark.StringDict{
		"ok":           starlark.NewBuiltin("ok", builtinOk),
		"err":          starlark.NewBuiltin("err", builtinErr),
		"nonretryable": starlark.NewBuiltin("nonretryable", builtinNonRetryable),
	}
}
