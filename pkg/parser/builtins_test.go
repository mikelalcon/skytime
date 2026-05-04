package parser

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// =============================================================================
// fakeExtension — task-2 test helper. Exposes two operations:
//
//   - echo(msg=...)  — idempotent, used by Phase 1 fixtures
//   - post(payload=...) — NOT idempotent, added in Plan 02-01 Task 3 to
//     exercise the lintMixedIdempotency pass (D2-05) without needing a
//     second extension type
//
// Both return a *dag.ActionRef carrying the operation kwargs. Initialize
// returns a *starlarkstruct.Module with both as attributes (D-08).
// =============================================================================

type fakeExtension struct{}

func (*fakeExtension) Name() string { return "fake_ext" }

func (*fakeExtension) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	echoFn := starlark.NewBuiltin("echo", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kw []starlark.Tuple) (starlark.Value, error) {
		var msg string
		if err := starlark.UnpackArgs("echo", args, kw, "msg", &msg); err != nil {
			return nil, err
		}
		kwDict := starlark.NewDict(1)
		_ = kwDict.SetKey(starlark.String("msg"), starlark.String(msg))
		return &dag.ActionRef{
			Pos:    callerPosition(thread),
			Kind_:  "fake_ext.echo",
			Kwargs: kwDict,
		}, nil
	})
	postFn := starlark.NewBuiltin("post", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kw []starlark.Tuple) (starlark.Value, error) {
		var payload string
		if err := starlark.UnpackArgs("post", args, kw, "payload", &payload); err != nil {
			return nil, err
		}
		kwDict := starlark.NewDict(1)
		_ = kwDict.SetKey(starlark.String("payload"), starlark.String(payload))
		return &dag.ActionRef{
			Pos:    callerPosition(thread),
			Kind_:  "fake_ext.post",
			Kwargs: kwDict,
		}, nil
	})
	return &starlarkstruct.Module{
		Name: "fake_ext",
		Members: starlark.StringDict{
			"echo": echoFn,
			"post": postFn,
		},
	}, nil
}

func (*fakeExtension) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"echo": {
			Name:       "echo",
			Idempotent: extension.Ptr(true),
			Func: func(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
				return nil, nil
			},
			KwargsType: reflect.TypeOf(struct {
				Msg string `star:"msg,required"`
			}{}),
		},
		"post": {
			Name:       "post",
			Idempotent: extension.Ptr(false), // D2-05 fixture pair: NOT idempotent
			Func: func(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
				return nil, nil
			},
			KwargsType: reflect.TypeOf(struct {
				Payload string `star:"payload,required"`
			}{}),
		},
	}
}

// =============================================================================
// helpers
// =============================================================================

func newTestParser(t *testing.T) *Parser {
	t.Helper()
	p, err := NewParser(WithExtensions(&fakeExtension{}))
	require.NoError(t, err)
	return p
}

// =============================================================================
// DSL-01: flow(...)
// =============================================================================

func TestParseFlow_DSL01(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={"a":"int"}, steps=[step(action=fake_ext.echo(msg="hi"))])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	require.Contains(t, flows, "x")
	f := flows["x"]
	assert.Equal(t, "x", f.Name)
	require.Equal(t, map[string]string{"a": "int"}, f.Inputs)
	require.Len(t, f.Body, 1)
	_, ok := f.Body[0].(*dag.Step)
	require.True(t, ok, "first body element should be *dag.Step, got %T", f.Body[0])
}

// =============================================================================
// DSL-02: step(action=...)
// =============================================================================

func TestStep_SingleAction(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[step(action=fake_ext.echo(msg="hi"))])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	step, ok := flows["x"].Body[0].(*dag.Step)
	require.True(t, ok)
	require.Len(t, step.Actions, 1)
	assert.Equal(t, "fake_ext.echo", step.Actions[0].Kind_)
}

// =============================================================================
// DSL-03: step(block=[a, b, c])
// =============================================================================

func TestStep_Block(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[step(block=[
    fake_ext.echo(msg="a"),
    fake_ext.echo(msg="b"),
    fake_ext.echo(msg="c"),
])])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	step, ok := flows["x"].Body[0].(*dag.Step)
	require.True(t, ok)
	require.Len(t, step.Actions, 3)
}

// =============================================================================
// DSL-02/03 mutual exclusion + neither
// =============================================================================

func TestStep_MutuallyExclusive(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[step(action=fake_ext.echo(msg="a"), block=[fake_ext.echo(msg="b")])])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	// D4.1-06 unified the error to the 4-way message — Phase 1 callers see
	// the new canonical wording.
	assert.Contains(t, err.Error(), "must provide exactly one of action, block, action_fn, or block_fn")
}

func TestStep_NeitherActionNorBlock(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[step()])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	// D4.1-06: canonical 4-way at-least-one message.
	assert.Contains(t, err.Error(), "must provide action, block, action_fn, or block_fn")
}

// =============================================================================
// DSL-04: if_cond(cond=lambda, then=[...], else_=[...])
// =============================================================================

func TestIfCond_LambdaCapture(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    if_cond(
        cond = lambda ctx: True,
        then = [step(action=fake_ext.echo(msg="t"))],
        else_ = [step(action=fake_ext.echo(msg="e"))],
    ),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	ifc, ok := flows["x"].Body[0].(*dag.IfCond)
	require.True(t, ok)
	assert.NotEmpty(t, ifc.LambdaID, "if_cond.cond must be captured with non-empty LambdaID")
	require.Len(t, ifc.Then, 1)
	require.Len(t, ifc.Else, 1)

	// Captured lambda registered in p.lambdas.
	_, present := p.lambdas[ifc.LambdaID]
	assert.True(t, present, "captured lambda must be present in parser session lambda map")
}

func TestIfCond_NoElseProducesEmptySlice(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    if_cond(cond = lambda ctx: True, then = [step(action=fake_ext.echo(msg="t"))]),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	ifc, ok := flows["x"].Body[0].(*dag.IfCond)
	require.True(t, ok)
	assert.Len(t, ifc.Then, 1)
	assert.Len(t, ifc.Else, 0, "missing else_= renders as empty slice (or nil)")
}

// =============================================================================
// DSL-05: script(id, fn=lambda, output_alias)
// =============================================================================

func TestScript_LambdaCapture(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    script(id = "s", fn = lambda ctx: ctx, output_alias = "r"),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	scr, ok := flows["x"].Body[0].(*dag.Script)
	require.True(t, ok)
	assert.Equal(t, "s", scr.ID)
	assert.Equal(t, "r", scr.OutputAlias)
	assert.NotEmpty(t, scr.LambdaID)
}

// =============================================================================
// DSL-06: for_each_parallel — both items forms
// =============================================================================

func TestForEachParallel_BothItemForms_List(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    for_each_parallel(items=[1, 2, 3], item="x", steps=[step(action=fake_ext.echo(msg="loop"))]),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	fep, ok := flows["x"].Body[0].(*dag.ForEachParallel)
	require.True(t, ok)
	assert.Empty(t, fep.ItemsLambdaID, "literal items: ItemsLambdaID must be empty")
	require.Len(t, fep.ItemsLiteral, 3)
	assert.Equal(t, "x", fep.ItemVar)
	require.NoError(t, fep.Validate(), "validate exactly-one-of after construction")
}

func TestForEachParallel_BothItemForms_Lambda(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    for_each_parallel(items=lambda ctx: [], item="x", steps=[step(action=fake_ext.echo(msg="loop"))]),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	fep, ok := flows["x"].Body[0].(*dag.ForEachParallel)
	require.True(t, ok)
	assert.NotEmpty(t, fep.ItemsLambdaID, "lambda items: ItemsLambdaID must be set")
	assert.Nil(t, fep.ItemsLiteral, "lambda items: ItemsLiteral must be nil")
	require.NoError(t, fep.Validate())
}

// =============================================================================
// DSL-07: call_flow(name, inputs, child_options) — name resolution at parse
// =============================================================================

func TestCallFlow_NameResolution(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="other", inputs={}, steps=[step(action=fake_ext.echo(msg="other"))])
flow(name="caller", inputs={}, steps=[call_flow(name="other", inputs={"x": 1})])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	caller := flows["caller"]
	cf, ok := caller.Body[0].(*dag.CallFlow)
	require.True(t, ok)
	assert.Equal(t, "other", cf.Name)
	require.NotNil(t, cf.Resolved, "D-16: CallFlow.Resolved must be set after finalize pass")
	assert.Equal(t, "other", cf.Resolved.Name)
}

// =============================================================================
// D-15: duplicate flow names
// =============================================================================

func TestDuplicateFlowName(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="dup", inputs={}, steps=[step(action=fake_ext.echo(msg="a"))])
flow(name="dup", inputs={}, steps=[step(action=fake_ext.echo(msg="b"))])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate flow name")
}

// =============================================================================
// D-16: call_flow not found
// =============================================================================

func TestCallFlow_NotFound(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="caller", inputs={}, steps=[call_flow(name="missing", inputs={})])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call_flow target not found")
}

// =============================================================================
// DSL-08: retry / timeout pass through Step
// =============================================================================

func TestRetryPolicy_Through_Step(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    step(
        action = fake_ext.echo(msg="hi"),
        retry  = {"initial_interval": "1s", "backoff_coefficient": 2, "max_attempts": 5, "non_retryable_errors": ["FOO"]},
        timeout = {"start_to_close": "30s", "schedule_to_start": "5s"},
    ),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	step, ok := flows["x"].Body[0].(*dag.Step)
	require.True(t, ok)
	require.NotNil(t, step.Retry)
	assert.Equal(t, time.Second, step.Retry.InitialInterval)
	assert.Equal(t, float64(2), step.Retry.BackoffCoefficient)
	assert.Equal(t, 5, step.Retry.MaxAttempts)
	assert.Equal(t, []string{"FOO"}, step.Retry.NonRetryableErrors)

	require.NotNil(t, step.Timeout)
	assert.Equal(t, 30*time.Second, step.Timeout.StartToClose)
	assert.Equal(t, 5*time.Second, step.Timeout.ScheduleToStart)
}

// =============================================================================
// DSL-08: unknown retry key surfaces a position-aware error
// =============================================================================

func TestRetryPolicy_UnknownKey(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    step(action=fake_ext.echo(msg="hi"), retry={"max_attempt": 3}),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown key")
}

// =============================================================================
// EXT-02 integration: extension factory returns *dag.ActionRef
// =============================================================================

func TestExtensionFactory_ReturnsActionRef(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[step(action=fake_ext.echo(msg="hi"))])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	step, ok := flows["x"].Body[0].(*dag.Step)
	require.True(t, ok)
	require.Len(t, step.Actions, 1)
	ar := step.Actions[0]
	assert.Equal(t, "fake_ext.echo", ar.Kind_)
	require.NotNil(t, ar.Kwargs)

	// Kwargs were preserved verbatim from the .star source.
	val, _, err := ar.Kwargs.Get(starlark.String("msg"))
	require.NoError(t, err)
	require.NotNil(t, val)
	str, ok := val.(starlark.String)
	require.True(t, ok)
	assert.Equal(t, "hi", string(str))
}

// =============================================================================
// callerPosition correctness — error attribution points at the call site
// =============================================================================

func TestStep_ErrorPointsAtCallSite(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    step(),  # neither action nor block — line 2, col 5 (1-based)
])`)
	_, err := p.ParseSource("source.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe))
	require.True(t, pe.Pos.IsValid(), "error must carry a valid position")
	assert.Equal(t, "source.star", pe.Pos.Filename())
	assert.Equal(t, int32(2), pe.Pos.Line, "step() error should point at the line where step() was called")
}

// =============================================================================
// D3-19: task_queue kwarg on flow() and step()
// =============================================================================

// TestBuiltinFlow_TaskQueueKwarg_Valid asserts the parser threads
// flow(..., task_queue="critical") through to dag.Flow.TaskQueue.
func TestBuiltinFlow_TaskQueueKwarg_Valid(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", task_queue="critical", steps=[step(action=fake_ext.echo(msg="hi"))])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	require.Contains(t, flows, "f")
	assert.Equal(t, "critical", flows["f"].TaskQueue,
		"D3-19: flow(..., task_queue=...) must thread to Flow.TaskQueue")
}

// TestBuiltinFlow_TaskQueueKwarg_Default asserts that omitting task_queue
// produces an empty-string default (interpreted as "inherit worker default").
func TestBuiltinFlow_TaskQueueKwarg_Default(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", steps=[step(action=fake_ext.echo(msg="hi"))])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	assert.Empty(t, flows["f"].TaskQueue,
		"D3-19: omitted task_queue must default to empty string")
}

// TestBuiltinStep_TaskQueueKwarg_Valid asserts the parser threads
// step(..., task_queue="slow_io") through to dag.Step.TaskQueue.
func TestBuiltinStep_TaskQueueKwarg_Valid(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", steps=[step(action=fake_ext.echo(msg="hi"), task_queue="slow_io")])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	step, ok := flows["f"].Body[0].(*dag.Step)
	require.True(t, ok)
	assert.Equal(t, "slow_io", step.TaskQueue,
		"D3-19: step(..., task_queue=...) must thread to Step.TaskQueue")
}

// =============================================================================
// D3-13 (backport): max_concurrency kwarg on for_each_parallel()
// =============================================================================

// TestBuiltinForEachParallel_MaxConcurrencyKwarg_Valid asserts the parser
// threads for_each_parallel(..., max_concurrency=4) through to
// dag.ForEachParallel.MaxConcurrency.
func TestBuiltinForEachParallel_MaxConcurrencyKwarg_Valid(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", steps=[
    for_each_parallel(items=[1,2,3], item="row", steps=[step(action=fake_ext.echo(msg="hi"))], max_concurrency=4),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	fep, ok := flows["x"].Body[0].(*dag.ForEachParallel)
	require.True(t, ok)
	assert.Equal(t, 4, fep.MaxConcurrency,
		"D3-13: for_each_parallel(..., max_concurrency=N) must thread to ForEachParallel.MaxConcurrency")
}

// TestBuiltinForEachParallel_MaxConcurrencyKwarg_Default asserts the
// kwarg-omitted default of 0 (interpreter default — D3-13 says "10").
func TestBuiltinForEachParallel_MaxConcurrencyKwarg_Default(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", steps=[
    for_each_parallel(items=[1,2,3], item="row", steps=[step(action=fake_ext.echo(msg="hi"))]),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	fep, ok := flows["x"].Body[0].(*dag.ForEachParallel)
	require.True(t, ok)
	assert.Equal(t, 0, fep.MaxConcurrency,
		"omitted max_concurrency must default to 0 (interpreter applies D3-13 default of 10)")
}

// TestBuiltinForEachParallel_MaxConcurrencyNegativeRejected asserts the
// parser rejects max_concurrency=-1 with a position-aware ParseError.
// Zero is allowed (interpreter default); negative is invalid.
func TestBuiltinForEachParallel_MaxConcurrencyNegativeRejected(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", steps=[
    for_each_parallel(items=[1,2,3], item="row", steps=[step(action=fake_ext.echo(msg="hi"))], max_concurrency=-1),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.Contains(t, pe.Msg, "max_concurrency must be >= 0",
		"D3-13: negative max_concurrency must surface a clear error")
	require.True(t, pe.Pos.IsValid(), "error must carry a valid position")
}

// =============================================================================
// finalize_test surface aliases (some via fixtures_test.go later)
// =============================================================================

func TestResolveCallFlows_Found(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="a", inputs={}, steps=[step(action=fake_ext.echo(msg="a"))])
flow(name="b", inputs={}, steps=[call_flow(name="a", inputs={})])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	cf := flows["b"].Body[0].(*dag.CallFlow)
	require.NotNil(t, cf.Resolved)
	assert.Equal(t, flows["a"], cf.Resolved)
}

func TestResolveCallFlows_NotFound(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="a", inputs={}, steps=[call_flow(name="missing", inputs={})])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call_flow target not found")
}

// =============================================================================
// Sanity: an unwrapped ActionRef in flow.steps surfaces a clear error.
// =============================================================================

func TestFlow_StepsListMustContainNodesNotActionRefs(t *testing.T) {
	p := newTestParser(t)
	// Pass an ActionRef directly to flow's steps list (forgot the step() wrap).
	src := []byte(`flow(name="x", inputs={}, steps=[fake_ext.echo(msg="hi")])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	// Error mentions either the flow-node-shaped message or the step type.
	msg := err.Error()
	assert.True(t,
		strings.Contains(msg, "flow node") || strings.Contains(msg, "ActionRef"),
		"expected error to flag the unwrapped ActionRef in steps list, got: %s", msg)
}

// =============================================================================
// Plan 04.1-03 Task 2 — builtinStep accepts name= / action_fn= / block_fn=
// kwargs and enforces 4-way mutual exclusion (D4.1-06, D4.1-15)
// =============================================================================

// TestBuiltinStep_AcceptsName: a literal name kwarg lands on Step.Name and
// leaves Step.NameFn nil.
func TestBuiltinStep_AcceptsName(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    step(name="my step", action=fake_ext.echo(msg="hi")),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	step, ok := flows["x"].Body[0].(*dag.Step)
	require.True(t, ok)
	assert.Equal(t, "my step", step.Name)
	assert.Nil(t, step.NameFn, "literal name (no ${) must leave NameFn nil")
}

// TestBuiltinStep_AcceptsActionFn: parse step(action_fn=lambda ctx: ...) —
// resulting *dag.Step has ActionFn != nil and Actions == nil. ActionFn.ID
// matches D-18 format.
func TestBuiltinStep_AcceptsActionFn(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={"repo":"string"}, steps=[
    step(action_fn = lambda ctx: fake_ext.echo(msg="hi")),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "action_fn must parse cleanly")
	step, ok := flows["x"].Body[0].(*dag.Step)
	require.True(t, ok)
	require.NotNil(t, step.ActionFn, "ActionFn must be populated when action_fn= is supplied")
	assert.Empty(t, step.Actions, "Actions must be empty when action_fn= is supplied")
	matched, mErr := regexp.MatchString(`^[0-9a-f]{8}:\d+:\d+$`, step.ActionFn.ID)
	require.NoError(t, mErr)
	assert.True(t, matched, "ActionFn.ID %q must match D-18 format", step.ActionFn.ID)
}

// TestBuiltinStep_AcceptsBlockFn: parse step(block_fn=lambda ctx: [...]) —
// resulting *dag.Step has BlockFn != nil, Actions == nil.
func TestBuiltinStep_AcceptsBlockFn(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    step(block_fn = lambda ctx: [fake_ext.echo(msg="a"), fake_ext.echo(msg="b")]),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "block_fn must parse cleanly")
	step, ok := flows["x"].Body[0].(*dag.Step)
	require.True(t, ok)
	require.NotNil(t, step.BlockFn, "BlockFn must be populated when block_fn= is supplied")
	assert.Empty(t, step.Actions, "Actions must be empty when block_fn= is supplied")
}

// TestBuiltinStep_RejectsActionPlusActionFn: action= and action_fn=
// together MUST surface a *dag.ParseError with the canonical 4-way
// message.
func TestBuiltinStep_RejectsActionPlusActionFn(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    step(
        action = fake_ext.echo(msg="a"),
        action_fn = lambda ctx: fake_ext.echo(msg="b"),
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.Contains(t, pe.Msg, "must provide exactly one of action, block, action_fn, or block_fn")
}

// TestBuiltinStep_RejectsBlockPlusBlockFn: block= and block_fn= together
// trigger the same canonical 4-way mutual-exclusion error.
func TestBuiltinStep_RejectsBlockPlusBlockFn(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    step(
        block = [fake_ext.echo(msg="a")],
        block_fn = lambda ctx: [fake_ext.echo(msg="b")],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe))
	assert.Contains(t, pe.Msg, "must provide exactly one of action, block, action_fn, or block_fn")
}

// TestBuiltinStep_RejectsActionPlusBlock_UnchangedFromPhase1: the original
// "action and block together" rejection still fires — now via the new
// canonical 4-way message (D4.1-06 unification).
func TestBuiltinStep_RejectsActionPlusBlock_UnchangedFromPhase1(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[step(action=fake_ext.echo(msg="a"), block=[fake_ext.echo(msg="b")])])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe))
	assert.Contains(t, pe.Msg, "must provide exactly one of action, block, action_fn, or block_fn")
}

// TestBuiltinStep_NameInterpolation: a name kwarg containing ${...} routes
// through desugarInterpolation. Step.Name MUST be empty (the lambda
// carries the value) and Step.NameFn MUST be populated. NameFn.Pos points
// at the user's source.
func TestBuiltinStep_NameInterpolation(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={"repo":"string"}, steps=[
    step(name = "Get repo ${ctx.repo}", action = fake_ext.echo(msg="hi")),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	step, ok := flows["x"].Body[0].(*dag.Step)
	require.True(t, ok)
	assert.Empty(t, step.Name, "interpolated name: literal Name must be empty")
	require.NotNil(t, step.NameFn, "interpolated name: NameFn must be populated")
	assert.Equal(t, "test.star", step.NameFn.Pos.Filename(),
		"NameFn.Pos must point at the user's source")
}

// TestBuiltinStep_NameLiteralNoInterp: a plain literal name (no ${)
// populates Step.Name and leaves Step.NameFn nil.
func TestBuiltinStep_NameLiteralNoInterp(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    step(name = "literal", action = fake_ext.echo(msg="hi")),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	step, ok := flows["x"].Body[0].(*dag.Step)
	require.True(t, ok)
	assert.Equal(t, "literal", step.Name)
	assert.Nil(t, step.NameFn)
}

// TestBuiltinStep_RequiresAtLeastOneActionForm: step() with no kwargs
// surfaces an error mentioning the new 4-form requirement.
func TestBuiltinStep_RequiresAtLeastOneActionForm(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[step()])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe))
	assert.Contains(t, pe.Msg, "must provide action, block, action_fn, or block_fn")
}

// TestBuiltinStep_ActionKwargInterpolation: a string action kwarg
// containing ${...} desugars into a *StarlarkLambda inside the
// ActionRef.Kwargs *Dict. The lambda's Captured.Pos points at the user's
// source.
func TestBuiltinStep_ActionKwargInterpolation(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={"repo":"string"}, steps=[
    step(action = fake_ext.echo(msg = "/repos/${ctx.repo}")),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	step, ok := flows["x"].Body[0].(*dag.Step)
	require.True(t, ok)
	require.Len(t, step.Actions, 1)
	v, _, gErr := step.Actions[0].Kwargs.Get(starlark.String("msg"))
	require.NoError(t, gErr)
	require.NotNil(t, v)
	captured, isLambda := dag.UnwrapStarlarkLambda(v)
	require.True(t, isLambda, "interpolated string kwarg must round-trip as *StarlarkLambda inside ActionRef.Kwargs, got %T", v)
	require.NotNil(t, captured)
	assert.Equal(t, "test.star", captured.Pos.Filename(),
		"captured lambda's Pos must point at the user source")
}

// TestBuiltinStep_ActionFn_CtxTypoRejected: an action_fn lambda that
// references a non-existent input via ctx.<typo> surfaces as
// *dag.ValidationError (W10/D4.1-09 — action_fn lambdas flow through the
// existing D4-02 ctx.<name> walker via captureLambda).
func TestBuiltinStep_ActionFn_CtxTypoRejected(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={"repo":"string"}, steps=[
    step(action_fn = lambda ctx: fake_ext.echo(msg = ctx.tyop)),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	assert.Contains(t, ve.Msg, "tyop",
		"D4-02 walker must surface the ctx.<typo> attribute name")
}

// =============================================================================
// Plan 04.1-03 Task 3 — builtinFlow(name=...) supports ${...} interpolation
// (D4.1-16)
// =============================================================================

// TestBuiltinFlow_NameInterpolation: a flow name carrying ${...} populates
// Flow.NameFn while keeping Flow.Name as the literal template (kept for
// D-15 duplicate detection).
func TestBuiltinFlow_NameInterpolation(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="Run for ${ctx.org}", inputs={"org":"string"}, steps=[
    step(action=fake_ext.echo(msg="hi")),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	require.Contains(t, flows, "Run for ${ctx.org}",
		"literal name template kept for duplicate-detection key")
	f := flows["Run for ${ctx.org}"]
	require.NotNil(t, f.NameFn, "interpolated flow name must populate NameFn")
	assert.Equal(t, "test.star", f.NameFn.Pos.Filename())
}

// TestBuiltinFlow_NameLiteral_NoInterpolation: a plain literal flow name
// leaves Flow.NameFn nil (no regression on Phase 1 behavior).
func TestBuiltinFlow_NameLiteral_NoInterpolation(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="literal", inputs={}, steps=[step(action=fake_ext.echo(msg="hi"))])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	f, ok := flows["literal"]
	require.True(t, ok)
	assert.Equal(t, "literal", f.Name)
	assert.Nil(t, f.NameFn, "literal flow name must leave NameFn nil")
}

// TestBuiltinFlow_DuplicateNameStillRejected: two flows with the same
// literal name still produce a duplicate-name *dag.ParseError. Pin
// no-regression on D-15.
func TestBuiltinFlow_DuplicateNameStillRejected(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="dup", inputs={}, steps=[step(action=fake_ext.echo(msg="a"))])
flow(name="dup", inputs={}, steps=[step(action=fake_ext.echo(msg="b"))])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate flow name")
}

// TestBuiltinFlow_DuplicateNameAcrossLiteralAndInterpolated_Allowed: two
// flows where one is a literal name and the other a ${}-template that
// happens to read identically AT THE TEMPLATE LAYER do NOT collide.
// Documented v1 limitation: collision detection is template-equality, not
// runtime-resolved-string-equality.
func TestBuiltinFlow_DuplicateNameAcrossLiteralAndInterpolated_Allowed(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[step(action=fake_ext.echo(msg="a"))])
flow(name="${ctx.x}", inputs={"x":"string"}, steps=[step(action=fake_ext.echo(msg="b"))])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "different template strings must not collide on Flow.Name")
	require.Contains(t, flows, "x")
	require.Contains(t, flows, "${ctx.x}")
	assert.Nil(t, flows["x"].NameFn, "literal flow leaves NameFn nil")
	assert.NotNil(t, flows["${ctx.x}"].NameFn, "interpolated flow populates NameFn")
}

// =============================================================================
// Plan 04.1-03 Task 4 — script(id=...) supports ${...} interpolation
// (D4.1-02 / W9)
// =============================================================================

// TestBuiltinScript_IDInterpolation_PopulatesIDFn: a script id containing
// ${...} routes through desugarInterpolation and populates Script.IDFn.
// Script.ID retains the literal template (kept for future cross-script
// duplicate-detection — same shape as flow.NameFn / step.NameFn).
func TestBuiltinScript_IDInterpolation_PopulatesIDFn(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={"repo":"string"}, steps=[
    script(id = "extract_${ctx.repo}", fn = lambda ctx: {}, output_alias = "out"),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	scr, ok := flows["x"].Body[0].(*dag.Script)
	require.True(t, ok)
	assert.Equal(t, "extract_${ctx.repo}", scr.ID,
		"literal template kept on Script.ID for cross-script keys")
	require.NotNil(t, scr.IDFn, "interpolated script id must populate IDFn")
	assert.Equal(t, "test.star", scr.IDFn.Pos.Filename())
}

// TestBuiltinScript_IDLiteral_NoInterpolation: a plain literal id
// populates Script.ID and leaves Script.IDFn nil. No regression on
// Phase 3 behavior.
func TestBuiltinScript_IDLiteral_NoInterpolation(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    script(id = "extract_status", fn = lambda ctx: {}, output_alias = "out"),
])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	scr, ok := flows["x"].Body[0].(*dag.Script)
	require.True(t, ok)
	assert.Equal(t, "extract_status", scr.ID)
	assert.Nil(t, scr.IDFn)
}

// TestBuiltinScript_IDNonString_Errors: a non-string id surfaces a clear
// *dag.ParseError naming the offending kwarg and the actual type.
func TestBuiltinScript_IDNonString_Errors(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[
    script(id = 123, fn = lambda ctx: {}, output_alias = "out"),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.Contains(t, pe.Msg, "script.id: expected string")
	assert.Contains(t, pe.Msg, "int")
}

// =============================================================================
// Quick 260504-k9c — flow(description=...) kwarg + FlowsInOrder() accessor
// =============================================================================

// TestBuiltinFlow_DescriptionKwarg_RoundTrips proves the new optional
// description= kwarg flows verbatim into *dag.Flow.Description.
func TestBuiltinFlow_DescriptionKwarg_RoundTrips(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[script(id="s", fn=lambda ctx: {"a":1}, output_alias="a")], description="hello world")`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	require.Contains(t, flows, "f")
	assert.Equal(t, "hello world", flows["f"].Description)
}

// TestBuiltinFlow_DescriptionKwarg_DefaultEmpty proves omitting the
// description kwarg yields the empty-string default.
func TestBuiltinFlow_DescriptionKwarg_DefaultEmpty(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[script(id="s", fn=lambda ctx: {"a":1}, output_alias="a")])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	require.Contains(t, flows, "f")
	assert.Equal(t, "", flows["f"].Description, "default Description must be empty string")
}

// TestBuiltinFlow_DescriptionKwarg_AcceptsLongFreeForm proves there is no
// length cap and free-form content (newlines, unicode) round-trips
// byte-equal.
func TestBuiltinFlow_DescriptionKwarg_AcceptsLongFreeForm(t *testing.T) {
	p := newTestParser(t)
	// Build a 1KB+ string with newlines + unicode. Starlark string literal
	// supports backslash escapes; we use a raw string concatenation
	// shape inside Starlark (".." + "..") to keep readable test source.
	longStr := strings.Repeat("¡Hola, mundo!\nLine of unicode → ★\n", 40) // ~1.3KB
	src := []byte(`flow(name="f", inputs={}, steps=[script(id="s", fn=lambda ctx: {"a":1}, output_alias="a")], description=` + starlark.String(longStr).String() + `)`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	require.Contains(t, flows, "f")
	assert.Equal(t, longStr, flows["f"].Description, "long free-form description must round-trip byte-equal")
}

// TestBuiltinFlow_DescriptionKwarg_RejectsNonString proves UnpackArgs
// rejects a non-string description (int) with a typed *dag.ParseError.
func TestBuiltinFlow_DescriptionKwarg_RejectsNonString(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[script(id="s", fn=lambda ctx: {"a":1}, output_alias="a")], description=123)`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.Contains(t, pe.Msg, "description")
}

// TestParser_FlowsInOrder_PreservesDeclarationOrder proves FlowsInOrder()
// returns flows in source-declaration order (NOT alphabetical).
func TestParser_FlowsInOrder_PreservesDeclarationOrder(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="alpha",  inputs={}, steps=[script(id="s1", fn=lambda ctx: {"a":1}, output_alias="a")])
flow(name="zeta",   inputs={}, steps=[script(id="s2", fn=lambda ctx: {"b":2}, output_alias="b")])
flow(name="middle", inputs={}, steps=[script(id="s3", fn=lambda ctx: {"c":3}, output_alias="c")])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err)

	ordered := p.FlowsInOrder()
	require.Len(t, ordered, 3)
	assert.Equal(t, "alpha", ordered[0].Name)
	assert.Equal(t, "zeta", ordered[1].Name, "FlowsInOrder must preserve source order, not sort alphabetically (would put 'middle' here)")
	assert.Equal(t, "middle", ordered[2].Name)
}

// TestParser_FlowsInOrder_EmptyBeforeParse proves FlowsInOrder() returns
// an empty (non-nil) slice before any ParseFile/ParseSource invocation.
func TestParser_FlowsInOrder_EmptyBeforeParse(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)
	ordered := p.FlowsInOrder()
	require.NotNil(t, ordered, "FlowsInOrder must return non-nil even when empty")
	assert.Empty(t, ordered)
}
