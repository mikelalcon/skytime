package testing

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// makeRefAt constructs an ActionRef at a synthetic position with an
// empty Kwargs dict suitable for router input.
func makeRefAt(kind, file string, line, col int32) *dag.ActionRef {
	fn := file
	return &dag.ActionRef{
		Kind_:  kind,
		Pos:    syntax.MakePosition(&fn, line, col),
		Kwargs: starlark.NewDict(0),
	}
}

// TestRouter_DispatchesToMatchingMockLambda — VALIDATION.md per-task
// map cite. Registers a single (gh, get) → ok({"login":"octocat"})
// mock; the router callback returns OkResult with a MockOperationOutput
// carrying the dict.
func TestRouter_DispatchesToMatchingMockLambda(t *testing.T) {
	src := `
tester.mock_action(extension="gh", op="get",
    mock_fn=lambda kwargs, attempt: ok(value={"login":"octocat"}))
`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)

	attempts := NewAttemptCounter()
	cb := buildExecuteBatchCallback("users", reg, attempts, nil)

	refs := []*dag.ActionRef{makeRefAt("gh.get", "u.star", 1, 1)}
	results, err := cb(context.Background(), refs)
	require.NoError(t, err)
	require.Len(t, results, 1)

	okRes, isOk := results[0].(dag.OkResult)
	require.True(t, isOk, "expected OkResult, got %T", results[0])
	output, isMock := okRes.Output.(MockOperationOutput)
	require.True(t, isMock)
	assert.Equal(t, "octocat", output.Value["login"])
}

// TestRouter_NoMockFound_FailsFast — VALIDATION.md per-task map cite.
// D5-B2 verbatim message shape.
func TestRouter_NoMockFound_FailsFast(t *testing.T) {
	reg := NewMockRegistry()
	attempts := NewAttemptCounter()
	lookup := func(_ *dag.ActionRef) (int, int, string, bool) {
		return 0, 0, "fetch user", true
	}
	cb := buildExecuteBatchCallback("users", reg, attempts, lookup)

	refs := []*dag.ActionRef{makeRefAt("gh.delete", "users.star", 14, 5)}

	results, err := cb(context.Background(), refs)
	require.NoError(t, err)
	require.Len(t, results, 1)
	nr, isNR := results[0].(dag.NonRetryableErrResult)
	require.True(t, isNR, "expected NonRetryableErrResult, got %T", results[0])
	msg := nr.Err.Error()
	assert.Contains(t, msg, "no mock for gh.delete at")
	assert.Contains(t, msg, "users.star:14:5")
	assert.Contains(t, msg, `(step "fetch user")`)
	assert.True(t, errors.Is(nr.Err, extension.ErrNonRetryable),
		"err must wrap extension.ErrNonRetryable")
}

// TestRouter_NoMockFound_EmptyStepName — when the lookup is nil
// (unit-test path), step name renders as `""` for D5-B2 compatibility.
func TestRouter_NoMockFound_EmptyStepName(t *testing.T) {
	reg := NewMockRegistry()
	cb := buildExecuteBatchCallback("u", reg, NewAttemptCounter(), nil)
	refs := []*dag.ActionRef{makeRefAt("gh.delete", "x.star", 5, 1)}
	results, err := cb(context.Background(), refs)
	require.NoError(t, err)
	require.Len(t, results, 1)
	nr, _ := results[0].(dag.NonRetryableErrResult)
	assert.Contains(t, nr.Err.Error(), `(step "")`)
}

// TestRouter_MockReturnsErr_RetryableErrResult — err(msg=...) maps to
// RetryableErrResult; Temporal RetryPolicy fires on the workflow side.
func TestRouter_MockReturnsErr_RetryableErrResult(t *testing.T) {
	src := `tester.mock_action(extension="gh", op="get",
        mock_fn=lambda kwargs, attempt: err(msg="transient"))`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
	cb := buildExecuteBatchCallback("u", reg, NewAttemptCounter(), nil)
	refs := []*dag.ActionRef{makeRefAt("gh.get", "u.star", 1, 1)}
	results, _ := cb(context.Background(), refs)
	re, isRetryable := results[0].(dag.RetryableErrResult)
	require.True(t, isRetryable)
	assert.Contains(t, re.Err.Error(), "transient")
}

// TestRouter_MockReturnsNonRetryable_NonRetryableErrResult — the
// nonretryable() builder's msg is wrapped with ErrNonRetryable so
// errors.Is classification fires.
func TestRouter_MockReturnsNonRetryable_NonRetryableErrResult(t *testing.T) {
	src := `tester.mock_action(extension="gh", op="get",
        mock_fn=lambda kwargs, attempt: nonretryable(msg="bad input"))`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
	cb := buildExecuteBatchCallback("u", reg, NewAttemptCounter(), nil)
	refs := []*dag.ActionRef{makeRefAt("gh.get", "u.star", 1, 1)}
	results, _ := cb(context.Background(), refs)
	nr, isNR := results[0].(dag.NonRetryableErrResult)
	require.True(t, isNR)
	assert.Contains(t, nr.Err.Error(), "bad input")
	assert.True(t, errors.Is(nr.Err, extension.ErrNonRetryable))
}

// TestRouter_MockReturnsNone_NonRetryableErrMustReturn — D5-C4: a
// mock that returns None (forgotten return statement) surfaces as a
// NonRetryableErr with the canonical message.
func TestRouter_MockReturnsNone_NonRetryableErrMustReturn(t *testing.T) {
	src := `tester.mock_action(extension="gh", op="get",
        mock_fn=lambda kwargs, attempt: None)`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
	cb := buildExecuteBatchCallback("u", reg, NewAttemptCounter(), nil)
	refs := []*dag.ActionRef{makeRefAt("gh.get", "u.star", 1, 1)}
	results, _ := cb(context.Background(), refs)
	nr, isNR := results[0].(dag.NonRetryableErrResult)
	require.True(t, isNR)
	assert.Contains(t, nr.Err.Error(), "mock must return ok/err/nonretryable")
	assert.True(t, errors.Is(nr.Err, extension.ErrNonRetryable))
}

// TestRouter_MockReturnsWrongType_NonRetryableErrMustReturn — same
// D5-C4 path for an int return value.
func TestRouter_MockReturnsWrongType_NonRetryableErrMustReturn(t *testing.T) {
	src := `tester.mock_action(extension="gh", op="get",
        mock_fn=lambda kwargs, attempt: 42)`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
	cb := buildExecuteBatchCallback("u", reg, NewAttemptCounter(), nil)
	refs := []*dag.ActionRef{makeRefAt("gh.get", "u.star", 1, 1)}
	results, _ := cb(context.Background(), refs)
	nr, isNR := results[0].(dag.NonRetryableErrResult)
	require.True(t, isNR)
	assert.Contains(t, nr.Err.Error(), "mock must return ok/err/nonretryable")
	// Type tag should appear in the message for diagnostics.
	assert.Contains(t, nr.Err.Error(), "int")
}

// TestRouter_PanicRecovery — a mock lambda that triggers a panic
// (Starlark-side fail() builtin or a Go-side stripped value)
// surfaces as NonRetryableErrResult, NOT a bare Go panic.
//
// Pitfall 8 hardens the activity goroutine; Starlark fail() raises
// *EvalError, which path is already covered by
// TestRouter_MockEvalErr_PassesThrough below. Here we hit the Go
// recover() path by registering a captured lambda whose Fn is
// deliberately nil — starlark.Call panics on nil function, which
// the router catches.
func TestRouter_PanicRecovery(t *testing.T) {
	reg := NewMockRegistry()
	bad := MockEntry{
		Extension: "gh",
		Op:        "get",
		Lambda: &dag.CapturedLambda{
			ID:      "bogus",
			Fn:      nil, // triggers a panic inside starlark.Call
			BodyPos: syntax.Position{},
		},
	}
	require.NoError(t, reg.Add(bad))

	cb := buildExecuteBatchCallback("u", reg, NewAttemptCounter(), nil)
	refs := []*dag.ActionRef{makeRefAt("gh.get", "u.star", 1, 1)}
	results, err := cb(context.Background(), refs)
	require.NoError(t, err, "router must absorb panic into NonRetryableErrResult")
	require.Len(t, results, 1)
	nr, isNR := results[0].(dag.NonRetryableErrResult)
	require.True(t, isNR, "expected NonRetryableErrResult after panic, got %T", results[0])
	assert.Contains(t, nr.Err.Error(), "mock router panic")
	assert.True(t, errors.Is(nr.Err, extension.ErrNonRetryable))
}

// TestRouter_MockEvalErr_NonRetryable — a runtime Starlark error
// (here: integer division by zero) surfaces as *EvalError; router
// converts to a NonRetryableErrResult wrapping ErrNonRetryable.
//
// Note: in test-mode parses, `fail()` resolves to the PARSE-time
// builtinFail (D4.2-05 dual-semantics), which returns a *dag.Fail
// value at execute time — the router would surface that as
// "mock must return ok/err/nonretryable" (D5-C4 path), NOT as a
// fail()-style EvalError. We exercise the EvalError surface via
// integer division by zero instead.
func TestRouter_MockEvalErr_NonRetryable(t *testing.T) {
	src := `tester.mock_action(extension="gh", op="get",
        mock_fn=lambda kwargs, attempt: ok(value={"x": 1 // 0}))`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
	cb := buildExecuteBatchCallback("u", reg, NewAttemptCounter(), nil)
	refs := []*dag.ActionRef{makeRefAt("gh.get", "u.star", 1, 1)}
	results, _ := cb(context.Background(), refs)
	nr, isNR := results[0].(dag.NonRetryableErrResult)
	require.True(t, isNR, "expected NonRetryableErrResult, got %T", results[0])
	assert.True(t, strings.Contains(nr.Err.Error(), "zero") ||
		strings.Contains(nr.Err.Error(), "division") ||
		strings.Contains(nr.Err.Error(), "mock lambda eval error"),
		"err must mention the runtime error reason, got %s", nr.Err)
	assert.True(t, errors.Is(nr.Err, extension.ErrNonRetryable))
}

// TestRouter_AttemptIncrementsPerCall — per-call invocation
// increments AttemptCounter. Each ref hits the same key (idx 0,
// nil lookup → fallback (FlowName, -1, 0)).
func TestRouter_AttemptIncrementsPerCall(t *testing.T) {
	src := `tester.mock_action(extension="gh", op="get",
        mock_fn=lambda kwargs, attempt: ok(value={"a": attempt}))`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)

	attempts := NewAttemptCounter()
	cb := buildExecuteBatchCallback("u", reg, attempts, nil)

	refs := []*dag.ActionRef{makeRefAt("gh.get", "u.star", 1, 1)}

	// First call → attempt=1
	results, _ := cb(context.Background(), refs)
	okRes := results[0].(dag.OkResult)
	out := okRes.Output.(MockOperationOutput)
	assert.EqualValues(t, 1, out.Value["a"])

	// Second call (different ref slice; same structural key) → attempt=2
	results, _ = cb(context.Background(), refs)
	okRes = results[0].(dag.OkResult)
	out = okRes.Output.(MockOperationOutput)
	assert.EqualValues(t, 2, out.Value["a"])
}

// TestRouter_KwargsAndCredentialIDExposed — kwargs dict reaches the
// mock lambda; credentialID surfaces under "_credential_id" (D5-C1a).
func TestRouter_KwargsAndCredentialIDExposed(t *testing.T) {
	src := `tester.mock_action(extension="gh", op="get",
        mock_fn=lambda kwargs, attempt: ok(value={
            "path_in": kwargs["path"],
            "cred_in": kwargs["_credential_id"],
        }))`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)

	cb := buildExecuteBatchCallback("u", reg, NewAttemptCounter(), nil)

	ref := makeRefAt("gh.get", "u.star", 1, 1)
	require.NoError(t, ref.Kwargs.SetKey(starlark.String("path"), starlark.String("/users/octocat")))
	ref.CredentialID = "gh-admin"

	results, _ := cb(context.Background(), []*dag.ActionRef{ref})
	require.Len(t, results, 1)
	okRes, ok := results[0].(dag.OkResult)
	require.True(t, ok, "expected OkResult, got %T", results[0])
	out := okRes.Output.(MockOperationOutput)
	assert.Equal(t, "/users/octocat", out.Value["path_in"])
	assert.Equal(t, "gh-admin", out.Value["cred_in"])
}

// TestAttempts_IncrementOnRetry — Phase 5 D5-C1 retry-attempt
// integration. The substantive end-to-end wiring (a real `flow` that
// invokes `gh.get` and surfaces the AttemptCounter via a fake gh
// extension) belongs to Plan 04 Task 3, which owns the
// runner-driven path (`pkgtesting.Run` + tester.run). Plan 03 only
// ships the RunOnceCapturing scaffolding the test depends on; the
// integration body has external-fixture dependencies (a fake `gh`
// extension) that are fully resolved by Plan 04.
//
// Forward-pointing skip per the must_haves convention. The substance
// of replay determinism + divergence reporting is exercised by:
//   - TestReplay_DeterministicEventSequence (Task 1 — pkg/interpreter)
//   - TestReplay_DivergenceReportFormat   (Task 2 — pkg/testing)
//   - TestReplay_RunOnceCapturing_TwoRunsByteEqual (Task 2)
//   - TestRouter_AttemptIncrementsPerCall (Plan 02; unit-tests
//     buildExecuteBatchCallback with a 2-attempt mock directly).
func TestAttempts_IncrementOnRetry(t *testing.T) {
	t.Skip("Plan 04 wires tester.run end-to-end with a fake gh extension; Plan 03 only provides the RunOnceCapturing scaffolding")
}
