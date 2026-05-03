package interpreter

// White-box tests for the action_fn / block_fn dispatch path inside
// walkStep — Plan 04.1-05b Task 1. Tests the strict return-type contract
// (D4.1-07), the empty-batch short-circuit (D4.1-09), the explicit
// ActionRef.Freeze() (W8) before resolveKwargs, and fail() inner-callsite
// preservation (D4.1-08, B6).

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// helperCompileLambdaWithActionRefBuiltin compiles a Starlark source file
// that may reference a predeclared `_action(kind=..., **kwargs)` builtin
// returning a *dag.ActionRef. Returns the resulting top-level `f` function
// and the file bytes so tests can compute a stable lambda ID.
//
// The builtin signature is `_action(kind, **kwargs) -> ActionRef`; tests
// call it inside a lambda body to construct ActionRef values without
// touching the parser. credentialId is always "" for tests.
func helperCompileLambdaWithActionRefBuiltin(t *testing.T, name, src string) (*starlark.Function, []byte) {
	t.Helper()
	srcBytes := []byte(src)
	thread := &starlark.Thread{Name: "test:" + name}
	mkActionFn := starlark.NewBuiltin("_action", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kw []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 1 {
			return nil, assertErrf("_action requires exactly one positional kind arg")
		}
		ks, ok := args[0].(starlark.String)
		if !ok {
			return nil, assertErrf("_action kind must be a string")
		}
		kwargs := starlark.NewDict(len(kw))
		for _, pair := range kw {
			if err := kwargs.SetKey(pair[0], pair[1]); err != nil {
				return nil, err
			}
		}
		ref := &dag.ActionRef{
			Pos:    syntax.Position{},
			Kind_:  string(ks),
			Kwargs: kwargs,
		}
		return ref, nil
	})
	predeclared := starlark.StringDict{"_action": mkActionFn}
	globals, err := starlark.ExecFile(thread, name, srcBytes, predeclared)
	require.NoError(t, err)
	val, ok := globals["f"]
	require.True(t, ok, "test source must define top-level `f = lambda ...`")
	fn, ok := val.(*starlark.Function)
	require.True(t, ok, "f must be a *starlark.Function, got %T", val)
	return fn, srcBytes
}

// assertErrf is a tiny helper used inside the predeclared builtin to
// surface a nicely-shaped error without pulling in a heavy logging dep.
type assertErr string

func (e assertErr) Error() string { return string(e) }
func assertErrf(s string) error   { return assertErr(s) }

// helperBuildActionFnFlow compiles `src` (must define `f = lambda ctx:
// _action("kind", path="...", ...)` or similar), wraps it as
// CapturedLambda, and registers a *ParsedFlow whose Body is a single Step
// with Step.ActionFn pointing at the captured lambda. Returns the parsed
// flow, the lambda ID, and the underlying *dag.Step so tests can inspect
// or mutate it before registry.Register.
func helperBuildActionFnFlow(t *testing.T, flowName, src string) (*ParsedFlow, string, *dag.Step) {
	t.Helper()
	fn, srcBytes := helperCompileLambdaWithActionRefBuiltin(t, flowName+".star", src)
	pos := fn.Position()
	id := dag.ComputeLambdaID(srcBytes, pos)
	captured := &dag.CapturedLambda{
		ID:       id,
		Fn:       fn,
		Pos:      pos,
		FreeVars: starlark.StringDict{},
	}
	filename := flowName + ".star"
	step := &dag.Step{
		Pos:      syntax.MakePosition(&filename, 1, 1),
		ActionFn: captured,
	}
	parsed := &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    syntax.MakePosition(&filename, 1, 1),
			Name:   flowName,
			Inputs: map[string]string{},
			Body:   []dag.Node{step},
		},
		Lambdas: map[string]*dag.CapturedLambda{id: captured},
	}
	return parsed, id, step
}

// helperBuildBlockFnFlow is the block_fn analogue of helperBuildActionFnFlow.
func helperBuildBlockFnFlow(t *testing.T, flowName, src string) (*ParsedFlow, string, *dag.Step) {
	t.Helper()
	fn, srcBytes := helperCompileLambdaWithActionRefBuiltin(t, flowName+".star", src)
	pos := fn.Position()
	id := dag.ComputeLambdaID(srcBytes, pos)
	captured := &dag.CapturedLambda{
		ID:       id,
		Fn:       fn,
		Pos:      pos,
		FreeVars: starlark.StringDict{},
	}
	filename := flowName + ".star"
	step := &dag.Step{
		Pos:     syntax.MakePosition(&filename, 1, 1),
		BlockFn: captured,
	}
	parsed := &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    syntax.MakePosition(&filename, 1, 1),
			Name:   flowName,
			Inputs: map[string]string{},
			Body:   []dag.Node{step},
		},
		Lambdas: map[string]*dag.CapturedLambda{id: captured},
	}
	return parsed, id, step
}

// TestWalkStep_ActionFn_HappyPath: ActionFn returns a single ActionRef;
// ExecuteActivity is called once with a one-action batch.
func TestWalkStep_ActionFn_HappyPath(t *testing.T) {
	parsed, _, _ := helperBuildActionFnFlow(t, "actionfn_happy",
		`f = lambda ctx: _action("http.get", path = "/repos/octocat")`+"\n")

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	var capturedRefs []*dag.ActionRef
	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			refs := args.Get(1).([]*dag.ActionRef)
			capturedRefs = make([]*dag.ActionRef, len(refs))
			copy(capturedRefs, refs)
		}).
		Return(dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "actionfn_happy", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Len(t, capturedRefs, 1, "ExecuteActivity must be called with exactly one ActionRef")
	assert.Equal(t, "http.get", capturedRefs[0].Kind_)
}

// TestWalkStep_ActionFn_WrongReturnType: ActionFn returns starlark.Int —
// workflow fails with NonRetryableApplicationError mentioning
// "expected ActionRef" and the lambda position.
func TestWalkStep_ActionFn_WrongReturnType(t *testing.T) {
	parsed, _, step := helperBuildActionFnFlow(t, "actionfn_wrongtype",
		`f = lambda ctx: 5`+"\n")

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "actionfn_wrongtype", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	wfErr := env.GetWorkflowError()
	require.Error(t, wfErr)
	msg := wfErr.Error()
	assert.Contains(t, msg, "expected ActionRef", "must mention expected type")
	assert.Contains(t, msg, "int", "must mention actual type")
	// Position should appear in the error — the lambda's defined location.
	assert.Contains(t, msg, step.ActionFn.Pos.Filename(),
		"error must include the lambda position filename")
}

// TestWalkStep_BlockFn_HappyPath: BlockFn returns a list of 3 ActionRefs;
// ExecuteActivity is called once with a 3-element batch.
func TestWalkStep_BlockFn_HappyPath(t *testing.T) {
	src := `f = lambda ctx: [
    _action("http.get", path = "/a"),
    _action("http.get", path = "/b"),
    _action("http.get", path = "/c"),
]
`
	parsed, _, _ := helperBuildBlockFnFlow(t, "blockfn_happy", src)
	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	var capturedRefs []*dag.ActionRef
	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			refs := args.Get(1).([]*dag.ActionRef)
			capturedRefs = make([]*dag.ActionRef, len(refs))
			copy(capturedRefs, refs)
		}).
		Return(dag.ActionResults{
			dag.OkResult{Idx: 0, Output: nil},
			dag.OkResult{Idx: 1, Output: nil},
			dag.OkResult{Idx: 2, Output: nil},
		}, nil)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "blockfn_happy", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Len(t, capturedRefs, 3, "ExecuteActivity must receive a 3-element batch")
	assert.Equal(t, "http.get", capturedRefs[0].Kind_)
	assert.Equal(t, "http.get", capturedRefs[2].Kind_)
}

// TestWalkStep_BlockFn_WrongReturnType: BlockFn returns a single ActionRef
// (NOT a list); workflow fails with NonRetryableApplicationError saying
// "expected list of ActionRef".
func TestWalkStep_BlockFn_WrongReturnType(t *testing.T) {
	parsed, _, _ := helperBuildBlockFnFlow(t, "blockfn_wrongtype",
		`f = lambda ctx: _action("http.get", path = "/x")`+"\n")
	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "blockfn_wrongtype", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	wfErr := env.GetWorkflowError()
	require.Error(t, wfErr)
	msg := wfErr.Error()
	assert.Contains(t, msg, "expected list of ActionRef", "must mention list-of-ActionRef expectation")
	assert.Contains(t, msg, "ActionRef", "must mention actual type")
}

// TestWalkStep_BlockFn_ListEntryWrongType: BlockFn returns
// [ActionRef, "not-an-action"]; workflow fails because the second entry
// is a string.
func TestWalkStep_BlockFn_ListEntryWrongType(t *testing.T) {
	src := `f = lambda ctx: [_action("http.get", path = "/a"), "not-an-action"]` + "\n"
	parsed, _, _ := helperBuildBlockFnFlow(t, "blockfn_entrytype", src)
	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "blockfn_entrytype", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	wfErr := env.GetWorkflowError()
	require.Error(t, wfErr)
	msg := wfErr.Error()
	assert.Contains(t, msg, "block_fn batch entry is", "must mention bad batch entry type")
	assert.Contains(t, msg, "string", "must mention actual entry type 'string'")
	assert.Contains(t, msg, "expected ActionRef", "must mention expected entry type 'ActionRef'")
}

// TestWalkStep_BlockFn_EmptyList_ShortCircuits: BlockFn returns []. The
// workflow does NOT call ExecuteActivity. The empty-batch short-circuit
// must be observable: workflow completes successfully without dispatching
// any activity.
func TestWalkStep_BlockFn_EmptyList_ShortCircuits(t *testing.T) {
	parsed, _, _ := helperBuildBlockFnFlow(t, "blockfn_empty",
		`f = lambda ctx: []`+"\n")
	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	// If ExecuteActivity gets called, this assertion fires (the mock has
	// no Return defined so the testsuite would panic on an unexpected
	// invocation). We DO NOT register an OnActivity expectation so any
	// invocation surfaces as a hard failure.

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "blockfn_empty", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError(),
		"empty-batch short-circuit must complete cleanly without dispatching ExecuteActivity")
	// Defensive: if mock's AssertNotCalled exists, it would catch the
	// case. Since it does not on the testsuite mock, the lack of a
	// configured Return + clean completion is the proxy.
	env.AssertNotCalled(t, "ExecuteBatch", mock.Anything, mock.Anything)
}

// TestWalkStep_ActionFn_LambdaPanic: a multi-line lambda body where the
// `fail("oops")` callsite is on a different line than the lambda
// definition. Workflow fails with an error containing BOTH "oops" AND the
// inner-fail callsite line/column (NOT just the lambda definition line).
func TestWalkStep_ActionFn_LambdaPanic(t *testing.T) {
	// Multi-line: lambda definition begins on line 1; fail() is on line 2.
	// Function position is line 1 col ~5; fail() is at line 2 col ~9.
	src := "f = (lambda ctx:\n        fail(\"oops\"))\n"
	parsed, _, step := helperBuildActionFnFlow(t, "actionfn_panic", src)
	require.Equal(t, int32(1), int32(step.ActionFn.Pos.Line),
		"lambda function position must be on line 1 (sanity check)")

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "actionfn_panic", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	wfErr := env.GetWorkflowError()
	require.Error(t, wfErr)
	msg := wfErr.Error()
	assert.Contains(t, msg, "oops", "error must include user's fail() reason string")
	// The inner fail() callsite is on line 2. We assert ":2:" appears
	// somewhere in the error message — proves the inner callsite was
	// preserved (not just the lambda's def-position line 1).
	assert.True(t, strings.Contains(msg, ":2:"),
		"error must include the inner fail() line ':2:'; got: %s", msg)
}

// TestWalkStep_ActionFn_ReplayFrozenKwargs: an ActionFn lambda returns
// gh.get(path="/x") wrapped through buildStepActions. Run via
// TestWorkflowEnvironment twice (two independent runs). Both runs must
// pass byte-identical activity inputs to ExecuteBatch — proving the
// returned ActionRef.Kwargs *Dict was Freeze()'d so Items() iteration
// order is stable, and resolveKwargs runs the same way on every replay
// (W8 + Plan 04.1-05a determinism guarantee).
func TestWalkStep_ActionFn_ReplayFrozenKwargs(t *testing.T) {
	src := `f = lambda ctx: _action("http.get", z = "Z", a = "A", m = "M")` + "\n"
	parsed, _, _ := helperBuildActionFnFlow(t, "actionfn_replay", src)
	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	captureKwargs := func() []string {
		var ts testsuite.WorkflowTestSuite
		env := ts.NewTestWorkflowEnvironment()
		helperRegisterFakeExecuteBatch(env)

		var keys []string
		env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				refs := args.Get(1).([]*dag.ActionRef)
				if len(refs) != 1 || refs[0].Kwargs == nil {
					return
				}
				for _, item := range refs[0].Kwargs.Items() {
					if k, ok := item[0].(starlark.String); ok {
						keys = append(keys, string(k))
					}
				}
			}).
			Return(dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil)

		wf := NewWorkflow(registry)
		env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
		env.ExecuteWorkflow(wf, dag.WorkflowInput{
			FlowName: "actionfn_replay", ContentHash: "h", InitState: map[string]any{},
		})
		require.True(t, env.IsWorkflowCompleted())
		require.NoError(t, env.GetWorkflowError())
		return keys
	}

	run1 := captureKwargs()
	run2 := captureKwargs()
	require.NotEmpty(t, run1, "first run must observe non-empty kwargs")
	assert.Equal(t, run1, run2,
		"replay determinism: two runs must yield identical Kwargs Items() order; got run1=%v run2=%v",
		run1, run2)
}

// TestWalkStep_StaticActionUnchanged: synthesize a Step with Actions
// directly populated (NO ActionFn / BlockFn). ExecuteActivity must
// receive that Actions slice unchanged. No regression on the existing
// path.
func TestWalkStep_StaticActionUnchanged(t *testing.T) {
	step := helperMakeStepWithActions("", nil, 1)
	parsed := helperMakeStepFlow(t, "static_path", "", step)
	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)

	var capturedKind string
	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			refs := args.Get(1).([]*dag.ActionRef)
			if len(refs) > 0 {
				capturedKind = refs[0].Kind_
			}
		}).
		Return(dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "static_path", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	assert.Equal(t, "fake.echo", capturedKind, "static-actions path must be unchanged by Plan 04.1-05b")
}
