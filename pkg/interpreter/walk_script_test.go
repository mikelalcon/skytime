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

// TestWalkScript_StoresOutputUnderAlias: a script lambda returning a
// dict-like value writes into state under OutputAlias; subsequent
// walkers see the new key.
func TestWalkScript_StoresOutputUnderAlias(t *testing.T) {
	src := `
f1 = lambda ctx: {"a": 1, "b": "two"}
f2 = lambda ctx: ctx.computed.b + "!"
`
	srcBytes := []byte(src)
	thread := &starlark.Thread{Name: "test:script"}
	globals, err := starlark.ExecFile(thread, "script.star", srcBytes, nil)
	require.NoError(t, err)
	fn1 := globals["f1"].(*starlark.Function)
	fn2 := globals["f2"].(*starlark.Function)
	id1 := dag.ComputeLambdaID(srcBytes, fn1.Position())
	id2 := dag.ComputeLambdaID(srcBytes, fn2.Position())

	filename := "script.star"
	pos := syntax.MakePosition(&filename, 1, 1)
	parsed := &ParsedFlow{
		Flow: &dag.Flow{
			Pos: pos, Name: "scripttest", Inputs: map[string]string{},
			Body: []dag.Node{
				&dag.Script{Pos: pos, ID: "compute", LambdaID: id1, OutputAlias: "computed"},
				&dag.Script{Pos: pos, ID: "echo", LambdaID: id2, OutputAlias: "echoed"},
			},
		},
		Lambdas: map[string]*dag.CapturedLambda{
			id1: {ID: id1, Fn: fn1, Pos: fn1.Position(), FreeVars: starlark.StringDict{}},
			id2: {ID: id2, Fn: fn2, Pos: fn2.Position(), FreeVars: starlark.StringDict{}},
		},
	}

	registry := NewRegistry()
	require.NoError(t, registry.Register("scripttest", "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "scripttest", ContentHash: "h", InitState: map[string]any{}})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result map[string]any
	require.NoError(t, env.GetWorkflowResult(&result))
	// "computed" key has the dict; "echoed" appended "!" to its .b key.
	require.Contains(t, result, "computed")
	assert.Equal(t, "two!", result["echoed"])
}

// TestWalkScript_ZeroHistoryEvents (INTRP-03): a script-only flow does
// NOT schedule any activity.
func TestWalkScript_ZeroHistoryEvents(t *testing.T) {
	src := `f = lambda ctx: 42`
	srcBytes := []byte(src)
	thread := &starlark.Thread{Name: "test:script_zero"}
	globals, err := starlark.ExecFile(thread, "scriptzero.star", srcBytes, nil)
	require.NoError(t, err)
	fn := globals["f"].(*starlark.Function)
	id := dag.ComputeLambdaID(srcBytes, fn.Position())
	filename := "scriptzero.star"
	pos := syntax.MakePosition(&filename, 1, 1)
	parsed := &ParsedFlow{
		Flow: &dag.Flow{
			Pos: pos, Name: "szero", Inputs: map[string]string{},
			Body: []dag.Node{
				&dag.Script{Pos: pos, ID: "s1", LambdaID: id, OutputAlias: "x"},
			},
		},
		Lambdas: map[string]*dag.CapturedLambda{
			id: {ID: id, Fn: fn, Pos: fn.Position(), FreeVars: starlark.StringDict{}},
		},
	}
	registry := NewRegistry()
	require.NoError(t, registry.Register("szero", "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "szero", ContentHash: "h", InitState: map[string]any{}})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertActivityNumberOfCalls(t, "ExecuteBatch", 0)
}

// TestWalkScript_LambdaError: lambda that raises (via fail()) produces a
// workflow error.
func TestWalkScript_LambdaError(t *testing.T) {
	src := `f = lambda ctx: fail("boom")`
	srcBytes := []byte(src)
	thread := &starlark.Thread{Name: "test:script_err"}
	globals, err := starlark.ExecFile(thread, "scripterr.star", srcBytes, nil)
	require.NoError(t, err)
	fn := globals["f"].(*starlark.Function)
	id := dag.ComputeLambdaID(srcBytes, fn.Position())
	filename := "scripterr.star"
	pos := syntax.MakePosition(&filename, 1, 1)
	parsed := &ParsedFlow{
		Flow: &dag.Flow{
			Pos: pos, Name: "serr", Inputs: map[string]string{},
			Body: []dag.Node{
				&dag.Script{Pos: pos, ID: "s1", LambdaID: id, OutputAlias: "x"},
			},
		},
		Lambdas: map[string]*dag.CapturedLambda{
			id: {ID: id, Fn: fn, Pos: fn.Position(), FreeVars: starlark.StringDict{}},
		},
	}
	registry := NewRegistry()
	require.NoError(t, registry.Register("serr", "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{FlowName: "serr", ContentHash: "h", InitState: map[string]any{}})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}
