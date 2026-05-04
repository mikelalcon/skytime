package interpreter

// Wave 0 RED scaffolding for D4.2-04..06 expression-mode if_cond walker
// extensions (plan 04 fills). Three integration tests exercise:
//   - Result branch terminator binds keys+values into ctx.<OutputAlias>
//   - Fail branch terminator raises NonRetryableApplicationError
//   - Replay determinism: dict key order matches Result.Keys (Pitfall 5)
//
// Until plan 04 wires the *dag.Result and *dag.Fail cases into walkIfCond
// (and the walker reads IfCond.OutputAlias), each test panics or errors
// out with "unhandled node kind: *dag.Result" — the RED state.

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// helperBuildExpressionIfCondFlow constructs a *ParsedFlow whose body is
// a single expression-mode IfCond. Both branches end in *dag.Result with
// the same key set ({"sign", "magnitude"}). Returns the parsed flow.
func helperBuildExpressionIfCondFlow(t *testing.T) *ParsedFlow {
	t.Helper()
	src := `
fcond = lambda ctx: ctx.n > 0
fthen_sign = lambda ctx: "positive"
fthen_mag = lambda ctx: ctx.n
felse_sign = lambda ctx: "negative"
felse_mag = lambda ctx: -ctx.n
`
	srcBytes := []byte(src)
	thread := &starlark.Thread{Name: "test:expression-ifcond"}
	globals, err := starlark.ExecFile(thread, "expr_ifcond.star", srcBytes, nil)
	require.NoError(t, err)

	condFn := globals["fcond"].(*starlark.Function)
	thenSignFn := globals["fthen_sign"].(*starlark.Function)
	thenMagFn := globals["fthen_mag"].(*starlark.Function)
	elseSignFn := globals["felse_sign"].(*starlark.Function)
	elseMagFn := globals["felse_mag"].(*starlark.Function)

	condID := dag.ComputeLambdaID(srcBytes, condFn.Position())
	thenSignID := dag.ComputeLambdaID(srcBytes, thenSignFn.Position())
	thenMagID := dag.ComputeLambdaID(srcBytes, thenMagFn.Position())
	elseSignID := dag.ComputeLambdaID(srcBytes, elseSignFn.Position())
	elseMagID := dag.ComputeLambdaID(srcBytes, elseMagFn.Position())

	filename := "expr_ifcond.star"
	pos := syntax.MakePosition(&filename, 1, 1)

	return &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    pos,
			Name:   "expr_ifcond",
			Inputs: map[string]string{"n": "int"},
			Body: []dag.Node{
				&dag.IfCond{
					Pos:         pos,
					LambdaID:    condID,
					OutputAlias: "result_dict",
					Then: []dag.Node{
						&dag.Result{
							Pos:  pos,
							Keys: []string{"sign", "magnitude"},
							Values: map[string]*dag.CapturedLambda{
								"sign":      {ID: thenSignID, Fn: thenSignFn, Pos: thenSignFn.Position(), FreeVars: starlark.StringDict{}},
								"magnitude": {ID: thenMagID, Fn: thenMagFn, Pos: thenMagFn.Position(), FreeVars: starlark.StringDict{}},
							},
						},
					},
					Else: []dag.Node{
						&dag.Result{
							Pos:  pos,
							Keys: []string{"sign", "magnitude"},
							Values: map[string]*dag.CapturedLambda{
								"sign":      {ID: elseSignID, Fn: elseSignFn, Pos: elseSignFn.Position(), FreeVars: starlark.StringDict{}},
								"magnitude": {ID: elseMagID, Fn: elseMagFn, Pos: elseMagFn.Position(), FreeVars: starlark.StringDict{}},
							},
						},
					},
				},
			},
		},
		Lambdas: map[string]*dag.CapturedLambda{
			condID:     {ID: condID, Fn: condFn, Pos: condFn.Position(), FreeVars: starlark.StringDict{}},
			thenSignID: {ID: thenSignID, Fn: thenSignFn, Pos: thenSignFn.Position(), FreeVars: starlark.StringDict{}},
			thenMagID:  {ID: thenMagID, Fn: thenMagFn, Pos: thenMagFn.Position(), FreeVars: starlark.StringDict{}},
			elseSignID: {ID: elseSignID, Fn: elseSignFn, Pos: elseSignFn.Position(), FreeVars: starlark.StringDict{}},
			elseMagID:  {ID: elseMagID, Fn: elseMagFn, Pos: elseMagFn.Position(), FreeVars: starlark.StringDict{}},
		},
	}
}

// runExpressionIfCondWorkflow drives the workflow with an init state and
// returns the final state map (or fails the test on workflow error).
func runExpressionIfCondWorkflow(t *testing.T, parsed *ParsedFlow, initState map[string]any) map[string]any {
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

	var out map[string]any
	require.NoError(t, env.GetWorkflowResult(&out))
	return out
}

// TestWalkIfCond_ResultBoundToCtx: D4.2-04 — when a branch ends in
// *dag.Result, the walker resolves each value lambda, builds the key →
// value dict, and binds it into ctx.<OutputAlias>. RED until plan 04
// adds the *dag.Result case to walkBody.
func TestWalkIfCond_ResultBoundToCtx(t *testing.T) {
	parsed := helperBuildExpressionIfCondFlow(t)
	out := runExpressionIfCondWorkflow(t, parsed, map[string]any{"n": int64(3)})

	bound, ok := out["result_dict"]
	require.True(t, ok, "ctx.result_dict must be bound after expression-mode if_cond")
	dict, ok := bound.(map[string]any)
	require.True(t, ok, "result_dict must be a dict; got %T", bound)
	require.Equal(t, "positive", dict["sign"])
	// Numeric values round-trip through testsuite's JSON DataConverter,
	// so int64 surfaces as float64 here. EqualValues coerces; the
	// in-memory state map (before JSON) stores int64 — the next test
	// (TestWalkIfCond_ResultBoundToCtx_HappyThenBranch) covers that
	// directly via i.state.snapshot().
	require.EqualValues(t, 3, dict["magnitude"])
}

// TestWalkIfCond_FailRaisesNonRetryable: D4.2-05 — when a branch ends
// in *dag.Fail, the walker MUST raise a NonRetryableApplicationError.
// RED until plan 04 adds the *dag.Fail case.
func TestWalkIfCond_FailRaisesNonRetryable(t *testing.T) {
	parsed := helperBuildExpressionIfCondFlow(t)
	// Mutate the else-branch to be a Fail (otherwise we hit Result).
	failFlow := parsed.Flow.Body[0].(*dag.IfCond)
	failFlow.Else = []dag.Node{
		&dag.Fail{Pos: failFlow.Pos, Message: "boom"},
	}

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
		InitState:   map[string]any{"n": int64(-1)}, // falsy → else branch (Fail)
	})
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError(),
		"RED until plan 04: Fail branch must surface as workflow error")
	require.Contains(t, env.GetWorkflowError().Error(), "boom")
}

// TestReplay_DictKeyOrderDeterministic and the other replay-determinism
// tests live in replay_determinism_test.go (plan 04.2-04 Task 3). The
// canonical Wave-0 RED stub for TestReplay_DictKeyOrderDeterministic
// has been promoted there and given a more comprehensive scaffold
// (slog-event capture + serializeRecords byte-equality + Pitfall 5
// audit). See replay_determinism_test.go.
