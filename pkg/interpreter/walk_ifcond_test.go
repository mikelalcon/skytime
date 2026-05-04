package interpreter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// helperBuildIfCondFlow constructs a *ParsedFlow whose Body is a single
// dag.IfCond. The cond lambda is `lambda ctx: ctx.x > 0`. Then-branch
// runs a script that sets state["taken"] = "then"; Else-branch sets it
// to "else". Returns the ParsedFlow + the cond + body lambda IDs.
func helperBuildIfCondFlow(t *testing.T) (*ParsedFlow, string) {
	t.Helper()
	src := `
f = lambda ctx: ctx.x > 0
fthen = lambda ctx: "then"
felse = lambda ctx: "else"
`
	srcBytes := []byte(src)
	thread := &starlark.Thread{Name: "test:ifcond"}
	globals, err := starlark.ExecFile(thread, "ifcond.star", srcBytes, nil)
	require.NoError(t, err)

	fnCond := globals["f"].(*starlark.Function)
	fnThen := globals["fthen"].(*starlark.Function)
	fnElse := globals["felse"].(*starlark.Function)

	condID := dag.ComputeLambdaID(srcBytes, fnCond.Position())
	thenID := dag.ComputeLambdaID(srcBytes, fnThen.Position())
	elseID := dag.ComputeLambdaID(srcBytes, fnElse.Position())

	filename := "ifcond.star"
	posDummy := syntax.MakePosition(&filename, 1, 1)

	parsed := &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    posDummy,
			Name:   "ifcondtest",
			Inputs: map[string]string{},
			Body: []dag.Node{
				&dag.IfCond{
					Pos:      posDummy,
					LambdaID: condID,
					Then: []dag.Node{
						&dag.Script{Pos: posDummy, ID: "thenS", LambdaID: thenID, OutputAlias: "taken"},
					},
					Else: []dag.Node{
						&dag.Script{Pos: posDummy, ID: "elseS", LambdaID: elseID, OutputAlias: "taken"},
					},
				},
			},
		},
		Lambdas: map[string]*dag.CapturedLambda{
			condID: {ID: condID, Fn: fnCond, Pos: fnCond.Position(), FreeVars: starlark.StringDict{}},
			thenID: {ID: thenID, Fn: fnThen, Pos: fnThen.Position(), FreeVars: starlark.StringDict{}},
			elseID: {ID: elseID, Fn: fnElse, Pos: fnElse.Position(), FreeVars: starlark.StringDict{}},
		},
	}
	return parsed, condID
}

// runIfCondWorkflow executes a workflow with the given init state and
// returns the final state's "taken" key (or "" if missing).
func runIfCondWorkflow(t *testing.T, parsed *ParsedFlow, initState map[string]any) map[string]any {
	t.Helper()
	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName:    parsed.Flow.Name,
		ContentHash: "h",
		InitState:   initState,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result map[string]any
	require.NoError(t, env.GetWorkflowResult(&result))
	return result
}

// TestWalkIfCond_TruthyBranch: cond evaluates True → Then branch runs.
func TestWalkIfCond_TruthyBranch(t *testing.T) {
	parsed, _ := helperBuildIfCondFlow(t)
	result := runIfCondWorkflow(t, parsed, map[string]any{"x": int64(5)})
	assert.Equal(t, "then", result["taken"])
}

// TestWalkIfCond_FalsyBranch: cond evaluates False → Else branch runs.
func TestWalkIfCond_FalsyBranch(t *testing.T) {
	parsed, _ := helperBuildIfCondFlow(t)
	result := runIfCondWorkflow(t, parsed, map[string]any{"x": int64(-1)})
	assert.Equal(t, "else", result["taken"])
}

// TestWalkIfCond_LambdaError: a cond lambda referencing an unknown attr
// produces a Starlark error → walker bubbles error.
func TestWalkIfCond_LambdaError(t *testing.T) {
	src := `
f = lambda ctx: ctx.missing_attr
`
	srcBytes := []byte(src)
	thread := &starlark.Thread{Name: "test:ifcond_err"}
	globals, err := starlark.ExecFile(thread, "iferr.star", srcBytes, nil)
	require.NoError(t, err)
	fn := globals["f"].(*starlark.Function)
	id := dag.ComputeLambdaID(srcBytes, fn.Position())

	filename := "iferr.star"
	posDummy := syntax.MakePosition(&filename, 1, 1)
	parsed := &ParsedFlow{
		Flow: &dag.Flow{
			Pos: posDummy, Name: "iferr", Inputs: map[string]string{},
			Body: []dag.Node{
				&dag.IfCond{Pos: posDummy, LambdaID: id, Then: nil, Else: nil},
			},
		},
		Lambdas: map[string]*dag.CapturedLambda{
			id: {ID: id, Fn: fn, Pos: fn.Position(), FreeVars: starlark.StringDict{}},
		},
	}

	registry := NewRegistry()
	require.NoError(t, registry.Register("iferr", "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "iferr", ContentHash: "h", InitState: map[string]any{}})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

// TestWalkIfCond_ZeroHistoryEvents (INTRP-03): an IfCond with two empty
// branches whose cond lambda evaluates without errors must not introduce
// activity events. We assert the workflow completes successfully and
// inspect the run via env.IsWorkflowCompleted with no GetWorkflowError —
// the testsuite's deterministic env doesn't expose history events
// directly, but if the lambda eval introduced any workflow.* primitive
// (activity, sleep, side effect) it would be visible via the cancel
// watchdog's coroutine count or activity assertion. We check that no
// activity was started by asserting AssertActivityNumberOfCalls == 0.
func TestWalkIfCond_ZeroHistoryEvents(t *testing.T) {
	parsed, _ := helperBuildIfCondFlow(t)

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: parsed.Flow.Name, ContentHash: "h",
		InitState: map[string]any{"x": int64(5)},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	// IfCond + Script use only inline lambda eval — they must not schedule
	// any activity. (The Script walker also works without activity calls.)
	env.AssertActivityNumberOfCalls(t, "ExecuteBatch", 0)
}

// TestWalkIfCond_ProceduralFail: D4.2-07 — fail() is legal as a node
// inside a procedural-mode if_cond branch (no output_alias). The walker
// must dispatch *dag.Fail through walkNode → raiseFail and surface a
// NonRetryableApplicationError. Pinned by examples/skeleton/expression_if.star's
// procedural_demo flow under TestDifferentialCorpus, but exercised here
// at unit-test granularity so a regression in walkNode's *dag.Fail case
// surfaces immediately.
func TestWalkIfCond_ProceduralFail(t *testing.T) {
	src := `
fcond = lambda ctx: ctx.x > 0
`
	srcBytes := []byte(src)
	thread := &starlark.Thread{Name: "test:proc_fail"}
	globals, err := starlark.ExecFile(thread, "proc_fail.star", srcBytes, nil)
	require.NoError(t, err)
	condFn := globals["fcond"].(*starlark.Function)
	condID := dag.ComputeLambdaID(srcBytes, condFn.Position())

	filename := "proc_fail.star"
	pos := syntax.MakePosition(&filename, 1, 1)

	parsed := &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    pos,
			Name:   "proc_fail",
			Inputs: map[string]string{"x": "int"},
			Body: []dag.Node{
				// Procedural mode (no output_alias). Else branch contains a
				// fail() — D4.2-07 says this is legal.
				&dag.IfCond{
					Pos:      pos,
					LambdaID: condID,
					Then:     []dag.Node{},
					Else: []dag.Node{
						&dag.Fail{Pos: pos, Message: "x must be positive"},
					},
				},
			},
		},
		Lambdas: map[string]*dag.CapturedLambda{
			condID: {ID: condID, Fn: condFn, Pos: condFn.Position(), FreeVars: starlark.StringDict{}},
		},
	}

	registry := NewRegistry()
	require.NoError(t, registry.Register("proc_fail", "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName:    "proc_fail",
		ContentHash: "h",
		InitState:   map[string]any{"x": int64(-1)}, // falsy → else → fail()
	})

	require.True(t, env.IsWorkflowCompleted())
	werr := env.GetWorkflowError()
	require.Error(t, werr, "procedural-mode fail() must surface as workflow error")
	require.Contains(t, werr.Error(), "x must be positive")
}
