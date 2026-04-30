package interpreter

// White-box: package interpreter so newInterpreter / interpreter struct
// fields can be exercised directly.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// helperBuildEmptyParsedFlow returns a *ParsedFlow with the given name and
// content hash, an empty Body, and an empty Lambdas map. Used by the
// happy-path empty-body test.
func helperBuildEmptyParsedFlow(name string) *ParsedFlow {
	filename := "test.star"
	return &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    syntax.MakePosition(&filename, 1, 1),
			Name:   name,
			Inputs: map[string]string{},
			Body:   nil, // empty body — walkBody returns nil immediately
		},
		Lambdas: map[string]*dag.CapturedLambda{},
	}
}

// requireFlowNotInRegistryError asserts the wf error is a non-retryable
// ApplicationError of type "FlowNotInRegistry" with the canonical
// remediation message.
func requireFlowNotInRegistryError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr), "expected *temporal.ApplicationError, got %T: %v", err, err)
	require.Equal(t, "FlowNotInRegistry", appErr.Type())
	require.Contains(t, appErr.Message(), "use Build IDs to drain old workflows")
	require.True(t, appErr.NonRetryable(), "FlowNotInRegistry must be non-retryable")
}

// TestSkytimeWorkflow_FlowNotInRegistry: input on an empty/frozen registry
// returns a non-retryable FlowNotInRegistry application error.
func TestSkytimeWorkflow_FlowNotInRegistry(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	registry := NewRegistry()
	registry.Freeze()

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName:    "missing",
		ContentHash: "x",
		InitState:   map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	requireFlowNotInRegistryError(t, env.GetWorkflowError())
}

// TestSkytimeWorkflow_ContentHashMismatch: registry has greet@hash1; input
// asks for greet@hash2. Same FlowNotInRegistry error path.
func TestSkytimeWorkflow_ContentHashMismatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	registry := NewRegistry()
	require.NoError(t, registry.Register("greet", "hash1", helperBuildEmptyParsedFlow("greet")))
	registry.Freeze()

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName:    "greet",
		ContentHash: "hash2",
		InitState:   map[string]any{},
	})

	require.True(t, env.IsWorkflowCompleted())
	requireFlowNotInRegistryError(t, env.GetWorkflowError())
}

// TestSkytimeWorkflow_HappyPath_EmptyBody: empty-body Flow returns InitState
// as the workflow's final state. Proves the dispatcher wires through to
// state.snapshot() without invoking any walker stub (empty body is the
// only happy path until plan 03-03 lands real walkers).
func TestSkytimeWorkflow_HappyPath_EmptyBody(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	registry := NewRegistry()
	require.NoError(t, registry.Register("noop", "h0", helperBuildEmptyParsedFlow("noop")))
	registry.Freeze()

	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	initState := map[string]any{"k": "v", "n": int64(7)}
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName:    "noop",
		ContentHash: "h0",
		InitState:   initState,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result map[string]any
	require.NoError(t, env.GetWorkflowResult(&result))
	// Note: round-trip through testsuite's JSON DataConverter loses int
	// type info — int64(7) decodes as float64(7) in the result. We only
	// assert keys and string equality on "k".
	assert.Equal(t, "v", result["k"])
	_, hasN := result["n"]
	assert.True(t, hasN, "key 'n' must round-trip")
}

// TestNewWorkflow_ReturnsCallable verifies the closure returned by
// NewWorkflow has the exact Go signature
// `func(workflow.Context, dag.WorkflowInput) (map[string]any, error)`.
// Verified via reflection — checks NumIn, NumOut, parameter type names.
func TestNewWorkflow_ReturnsCallable(t *testing.T) {
	registry := NewRegistry()
	registry.Freeze()
	wf := NewWorkflow(registry)

	tt := reflect.TypeOf(wf)
	require.Equal(t, reflect.Func, tt.Kind())
	require.Equal(t, 2, tt.NumIn(), "NewWorkflow result must take exactly 2 args")
	require.Equal(t, 2, tt.NumOut(), "NewWorkflow result must return exactly 2 values")

	// In(0) is workflow.Context — which is a type alias for internal.Context.
	// Reflection sees the underlying named type, so assert against either
	// form. The behavioral check (RegisterWorkflowWithOptions accepts the
	// closure without panicking) is exercised by the FlowNotInRegistry test
	// above; this reflection test is a structural sanity check.
	in0 := tt.In(0)
	in0Str := in0.String()
	assert.True(t,
		in0Str == "workflow.Context" || in0Str == "internal.Context",
		"first arg must be (alias of) workflow.Context, got %s", in0Str)

	// In(1) is dag.WorkflowInput (struct).
	in1 := tt.In(1)
	assert.Equal(t, "dag.WorkflowInput", in1.String())

	// Out(0) is map[string]any (== map[string]interface {}).
	out0 := tt.Out(0)
	assert.Equal(t, "map[string]interface {}", out0.String())

	// Out(1) is the error interface.
	out1 := tt.Out(1)
	assert.Equal(t, "error", out1.Name())
}

// TestNewInterpreter_FinalSignature verifies that newInterpreter accepts
// (ctx, registry, parsed, contentHash, initState, logger) — the FINAL
// signature plan 03-03 will not retrofit. This is a compile-time check
// disguised as a runtime test: if the signature drifts, this file fails
// to compile.
func TestNewInterpreter_FinalSignature(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	registry := NewRegistry()
	parsed := helperBuildEmptyParsedFlow("sig")
	require.NoError(t, registry.Register("sig", "test-hash-0", parsed))
	registry.Freeze()

	// Construct the *interpreter via a tiny wrapper workflow so we have a
	// valid workflow.Context. The wrapper just builds the interpreter and
	// returns nil; the real assertion is "this compiles."
	wfFn := func(ctx workflow.Context) error {
		i := newInterpreter(ctx, registry, parsed, "test-hash-0",
			map[string]any{"x": 1}, workflow.GetLogger(ctx))
		require.NotNil(t, i)
		require.Equal(t, "test-hash-0", i.contentHash)
		require.Same(t, registry, i.registry)
		require.Same(t, parsed, i.parsed)
		require.NotNil(t, i.state)
		return nil
	}
	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
