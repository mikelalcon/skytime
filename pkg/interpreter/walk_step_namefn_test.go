package interpreter

// Plan 04.1-05b Task 2: NameFn resolution at flow_start and step_dispatch
// emission, plus replay-determinism for dynamic kwargs. These tests
// exercise i.resolveFlowName (workflow.go) and i.stepDisplayLabel
// (walk_step.go) by capturing the testsuite logger output.

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// captureLogger records every Info log call's keyvals so tests can grep
// for specific event attrs (e.g. flow_start's flow_name attr or
// step_dispatch's label attr). Implements go.temporal.io/sdk/log.Logger.
//
// The recorded entries are flat keyval slices preceded by the message;
// helpers below pluck the first value associated with a given key from a
// row whose message and "event" attr match.
type captureLogger struct {
	mu      sync.Mutex
	entries [][]any // each entry is [msg, k1, v1, k2, v2, ...]
}

func newCaptureLogger() *captureLogger { return &captureLogger{} }

func (c *captureLogger) appendEntry(msg string, kvs []any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	row := make([]any, 0, 1+len(kvs))
	row = append(row, msg)
	row = append(row, kvs...)
	c.entries = append(c.entries, row)
}

func (c *captureLogger) Debug(msg string, kvs ...any) { c.appendEntry(msg, kvs) }
func (c *captureLogger) Info(msg string, kvs ...any)  { c.appendEntry(msg, kvs) }
func (c *captureLogger) Warn(msg string, kvs ...any)  { c.appendEntry(msg, kvs) }
func (c *captureLogger) Error(msg string, kvs ...any) { c.appendEntry(msg, kvs) }

// Compile-time guarantee: *captureLogger satisfies log.Logger.
var _ log.Logger = (*captureLogger)(nil)

// findEventValue returns the value associated with `key` on the FIRST
// captured Info entry whose msg=="skytime" AND "event" attr equals
// `event`. Returns (nil, false) when no matching row is found.
func (c *captureLogger) findEventValue(event, key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, row := range c.entries {
		if len(row) == 0 {
			continue
		}
		if msg, ok := row[0].(string); !ok || msg != "skytime" {
			continue
		}
		// scan kvs for event match
		matchEvent := false
		var found any
		var foundFlag bool
		for k := 1; k+1 < len(row); k += 2 {
			ks, _ := row[k].(string)
			if ks == "event" {
				if vs, _ := row[k+1].(string); vs == event {
					matchEvent = true
				}
			}
			if ks == key {
				found = row[k+1]
				foundFlag = true
			}
		}
		if matchEvent && foundFlag {
			return found, true
		}
	}
	return nil, false
}

// helperBuildFlowWithNameFn compiles `nameSrc` (must define `f = lambda
// ctx: <expr returning starlark.String>`) into a *CapturedLambda and
// attaches it as Flow.NameFn. Body is empty (no walkers run; we only
// observe the flow_start event).
func helperBuildFlowWithNameFn(t *testing.T, flowName, nameSrc string) *ParsedFlow {
	t.Helper()
	srcBytes := []byte(nameSrc)
	thread := &starlark.Thread{Name: "test:" + flowName}
	globals, err := starlark.ExecFile(thread, flowName+"_name.star", srcBytes, nil)
	require.NoError(t, err)
	fnVal, ok := globals["f"]
	require.True(t, ok, "name source must define top-level `f = lambda ctx: ...`")
	fn, ok := fnVal.(*starlark.Function)
	require.True(t, ok)
	pos := fn.Position()
	id := dag.ComputeLambdaID(srcBytes, pos)
	captured := &dag.CapturedLambda{
		ID:       id,
		Fn:       fn,
		Pos:      pos,
		FreeVars: starlark.StringDict{},
	}
	filename := flowName + ".star"
	return &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    syntax.MakePosition(&filename, 1, 1),
			Name:   flowName, // literal fallback
			Inputs: map[string]string{},
			Body:   nil,
			NameFn: captured,
		},
		Lambdas: map[string]*dag.CapturedLambda{id: captured},
	}
}

// helperBuildFlowWithStepNameFn synthesizes a flow whose single Step has
// NameFn set (no Name literal). Body has one Step pointing at a single
// static ActionRef so step_dispatch is emitted (we don't care about the
// activity result; an OnActivity is registered).
func helperBuildFlowWithStepNameFn(t *testing.T, flowName, nameSrc string) *ParsedFlow {
	t.Helper()
	srcBytes := []byte(nameSrc)
	thread := &starlark.Thread{Name: "test:" + flowName}
	globals, err := starlark.ExecFile(thread, flowName+"_name.star", srcBytes, nil)
	require.NoError(t, err)
	fn := globals["f"].(*starlark.Function)
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
		Pos:    syntax.MakePosition(&filename, 1, 1),
		NameFn: captured,
		Actions: []*dag.ActionRef{{
			Pos:    syntax.MakePosition(&filename, 2, 1),
			Kind_:  "fake.echo",
			Kwargs: starlark.NewDict(0),
		}},
	}
	return &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    syntax.MakePosition(&filename, 1, 1),
			Name:   flowName,
			Inputs: map[string]string{},
			Body:   []dag.Node{step},
		},
		Lambdas: map[string]*dag.CapturedLambda{id: captured},
	}
}

// TestWorkflow_FlowName_Interpolated: a Flow with NameFn returning the
// resolved string surfaces that resolved string in the flow_start event,
// not the literal Name fallback.
func TestWorkflow_FlowName_Interpolated(t *testing.T) {
	parsed := helperBuildFlowWithNameFn(t, "fname_interp",
		`f = lambda ctx: "Run for octocat"`+"\n")

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	cap := newCaptureLogger()
	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(cap)
	env := ts.NewTestWorkflowEnvironment()

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "fname_interp", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	v, ok := cap.findEventValue("flow_start", "flow_name")
	require.True(t, ok, "flow_start event must include flow_name attr")
	assert.Equal(t, "Run for octocat", v,
		"flow_start.flow_name must equal the lambda-resolved string")
}

// TestWorkflow_FlowName_LiteralUnchanged: a Flow with NameFn==nil keeps
// the literal Name in flow_start (no regression on Phase 4 path).
func TestWorkflow_FlowName_LiteralUnchanged(t *testing.T) {
	parsed := helperBuildEmptyParsedFlow("my-flow")

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	cap := newCaptureLogger()
	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(cap)
	env := ts.NewTestWorkflowEnvironment()

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "my-flow", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	v, ok := cap.findEventValue("flow_start", "flow_name")
	require.True(t, ok)
	assert.Equal(t, "my-flow", v)
}

// TestWalkStep_Name_Interpolated: a Step with NameFn (no Name literal)
// emits step_dispatch with label == the lambda-resolved string.
func TestWalkStep_Name_Interpolated(t *testing.T) {
	parsed := helperBuildFlowWithStepNameFn(t, "stepname_interp",
		`f = lambda ctx: "Get repo octocat"`+"\n")

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	cap := newCaptureLogger()
	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(cap)
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)
	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).
		Return(dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "stepname_interp", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	v, ok := cap.findEventValue("step_dispatch", "label")
	require.True(t, ok, "step_dispatch must include label attr")
	assert.Equal(t, "Get repo octocat", v,
		"step_dispatch.label must use NameFn resolution")
}

// TestWalkStep_Name_Literal: a Step with Name=="my-step" and NameFn==nil
// emits step_dispatch with label=="my-step" (preferred over auto-derived).
func TestWalkStep_Name_Literal(t *testing.T) {
	filename := "stepname_lit.star"
	step := &dag.Step{
		Pos:  syntax.MakePosition(&filename, 1, 1),
		Name: "my-step",
		Actions: []*dag.ActionRef{{
			Pos:    syntax.MakePosition(&filename, 2, 1),
			Kind_:  "fake.echo",
			Kwargs: starlark.NewDict(0),
		}},
	}
	parsed := &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    syntax.MakePosition(&filename, 1, 1),
			Name:   "stepname_lit",
			Inputs: map[string]string{},
			Body:   []dag.Node{step},
		},
		Lambdas: map[string]*dag.CapturedLambda{},
	}

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	cap := newCaptureLogger()
	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(cap)
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)
	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).
		Return(dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "stepname_lit", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	v, ok := cap.findEventValue("step_dispatch", "label")
	require.True(t, ok)
	assert.Equal(t, "my-step", v,
		"step_dispatch.label must equal the literal Name when NameFn is nil")
}

// TestWalkStep_Name_AutoFallback: a Step with Name=="" AND NameFn==nil
// uses the auto-derived stepActionLabel format.
func TestWalkStep_Name_AutoFallback(t *testing.T) {
	step := helperMakeStepWithActions("", nil, 1)
	parsed := helperMakeStepFlow(t, "stepname_auto", "", step)

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	cap := newCaptureLogger()
	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(cap)
	env := ts.NewTestWorkflowEnvironment()
	helperRegisterFakeExecuteBatch(env)
	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).
		Return(dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil)

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName: "stepname_auto", ContentHash: "h", InitState: map[string]any{},
	})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	v, ok := cap.findEventValue("step_dispatch", "label")
	require.True(t, ok)
	// Auto-fallback: when both Name and NameFn are absent the dispatcher
	// must use the existing stepActionLabel(step) format unchanged. We
	// recompute it here so the assertion stays anchored to the helper's
	// definition rather than a magic string that drifts if
	// actionShortSummary changes.
	assert.Equal(t, stepActionLabel(step), v,
		"step_dispatch.label must fall back to stepActionLabel when Name and NameFn both absent")
}

// TestReplayDeterminism_DynamicKwargs: a flow with action_fn returning a
// dynamic ActionRef runs through TestWorkflowEnvironment twice — each
// independent run captures the activity input. Asserts byte-equal
// activity inputs across the two runs. Pin RUN-side determinism: kwarg
// map iteration must be insertion-order, lambda ID must be stable, and
// resolveKwargs must produce the same dict twice.
func TestReplayDeterminism_DynamicKwargs(t *testing.T) {
	src := `f = lambda ctx: _action("http.get", path = "/x", method = "GET", headers_accept = "application/json")` + "\n"
	parsed, _, _ := helperBuildActionFnFlow(t, "replay_dyn", src)

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	captureKwargs := func() ([]string, []string) {
		var ts testsuite.WorkflowTestSuite
		env := ts.NewTestWorkflowEnvironment()
		helperRegisterFakeExecuteBatch(env)

		var keys, values []string
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
					values = append(values, item[1].String())
				}
			}).
			Return(dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil)

		wf := NewWorkflow(registry)
		env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
		env.ExecuteWorkflow(wf, dag.WorkflowInput{
			FlowName: "replay_dyn", ContentHash: "h", InitState: map[string]any{},
		})
		require.True(t, env.IsWorkflowCompleted())
		require.NoError(t, env.GetWorkflowError())
		return keys, values
	}

	k1, v1 := captureKwargs()
	k2, v2 := captureKwargs()
	require.NotEmpty(t, k1, "first run must observe non-empty kwargs")
	assert.Equal(t, k1, k2, "replay determinism: kwarg key order must be identical")
	assert.Equal(t, v1, v2, "replay determinism: kwarg value order must be identical")
}
