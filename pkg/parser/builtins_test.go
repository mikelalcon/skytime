package parser

import (
	"context"
	"errors"
	"reflect"
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
// fakeExtension — task-2 test helper. Exposes one operation `echo(msg=...)`
// returning a *dag.ActionRef carrying the operation kwargs. Initialize
// returns a *starlarkstruct.Module with `echo` as an attribute (D-08).
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
	return &starlarkstruct.Module{
		Name:    "fake_ext",
		Members: starlark.StringDict{"echo": echoFn},
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
	assert.Contains(t, err.Error(), "exactly one of action or block")
}

func TestStep_NeitherActionNorBlock(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[step()])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must provide action or block")
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
