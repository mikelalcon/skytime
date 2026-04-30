package interpreter

// White-box: package interpreter so evalLambda + interpreter struct
// fields are directly accessible.

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// compileLambdaForTest compiles `expr` (a Starlark source containing one
// `lambda` expression at top-level via `f = <expr>`) and returns the
// resulting *starlark.Function plus the file bytes. Used to build
// CapturedLambda fixtures without going through the full parser.
func compileLambdaForTest(t *testing.T, name, src string) (*starlark.Function, []byte) {
	t.Helper()
	srcBytes := []byte(src)
	thread := &starlark.Thread{Name: "test:" + name}
	globals, err := starlark.ExecFile(thread, name, srcBytes, nil)
	require.NoError(t, err)
	val, ok := globals["f"]
	require.True(t, ok, "test source must define top-level `f = lambda ...`")
	fn, ok := val.(*starlark.Function)
	require.True(t, ok, "f must be a *starlark.Function, got %T", val)
	return fn, srcBytes
}

// helperBuildParsedFlowWithLambda builds a *ParsedFlow whose Lambdas map
// contains a single CapturedLambda at the canonical id "test-lambda-id"
// with the given function source. Returns the ParsedFlow and the lambda's
// stable ID.
func helperBuildParsedFlowWithLambda(t *testing.T, flowName, src string) (*ParsedFlow, string) {
	t.Helper()
	fn, srcBytes := compileLambdaForTest(t, flowName+".star", src)
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
			Name:   flowName,
			Inputs: map[string]string{},
			Body:   nil,
		},
		Lambdas: map[string]*dag.CapturedLambda{
			id: captured,
		},
	}, id
}

// TestEvalLambda_Success: evaluates a lambda that increments ctx.x;
// asserts the returned starlark.Value converts to int64(2) when the
// state has {"x": 1}.
func TestEvalLambda_Success(t *testing.T) {
	parsed, lambdaID := helperBuildParsedFlowWithLambda(t, "evaltest",
		"f = lambda ctx: ctx.x + 1\n")

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h0", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) (int64, error) {
		i := newInterpreter(ctx, registry, parsed, "h0",
			map[string]any{"x": int64(1)}, workflow.GetLogger(ctx))
		val, err := i.evalLambda(ctx, lambdaID)
		if err != nil {
			return 0, err
		}
		intVal, ok := val.(starlark.Int)
		if !ok {
			return 0, fmt.Errorf("expected starlark.Int, got %s", val.Type())
		}
		n, ok := intVal.Int64()
		if !ok {
			return 0, fmt.Errorf("int overflow")
		}
		return n, nil
	}

	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result int64
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, int64(2), result)
}

// TestEvalLambda_LambdaNotFound: evalLambda returns a non-retryable
// LambdaNotFound application error mentioning the missing ID and the
// owning flow's content_hash.
func TestEvalLambda_LambdaNotFound(t *testing.T) {
	parsed, _ := helperBuildParsedFlowWithLambda(t, "missingtest",
		"f = lambda ctx: 1\n")

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "the-hash", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) error {
		i := newInterpreter(ctx, registry, parsed, "the-hash",
			map[string]any{}, workflow.GetLogger(ctx))
		_, err := i.evalLambda(ctx, "nonexistent_id")
		return err
	}

	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())

	wfErr := env.GetWorkflowError()
	require.Error(t, wfErr)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, wfErr, &appErr)
	assert.Equal(t, "LambdaNotFound", appErr.Type())
	assert.True(t, appErr.NonRetryable())
	assert.Contains(t, appErr.Message(), "nonexistent_id")
	assert.Contains(t, appErr.Message(), "the-hash")
	assert.Contains(t, appErr.Message(), "missingtest")
}

// TestEvalLambda_PrintRoutesToWorkflowLogger: a lambda that calls
// print("hello") routes the message through workflow.GetLogger.Info with
// the "[skytime/print]" prefix and a lambda_id field. Captured via a
// buffer-backed *slog.Logger handler set on the test environment.
func TestEvalLambda_PrintRoutesToWorkflowLogger(t *testing.T) {
	parsed, lambdaID := helperBuildParsedFlowWithLambda(t, "printtest",
		"f = lambda ctx: print('hello') or 0\n")

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "ph", parsed))
	registry.Freeze()

	var buf bytes.Buffer
	slogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tlogger := log.NewStructuredLogger(slogger)

	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(tlogger)
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) error {
		i := newInterpreter(ctx, registry, parsed, "ph",
			map[string]any{}, workflow.GetLogger(ctx))
		_, err := i.evalLambda(ctx, lambdaID)
		return err
	}

	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	output := buf.String()
	assert.Contains(t, output, "[skytime/print]", "log must contain [skytime/print] prefix")
	assert.Contains(t, output, "hello", "log must contain the printed message")
	assert.Contains(t, output, "lambda_id", "log must contain lambda_id field key")
	// The lambda ID itself should appear in the output (textual handler emits key=value).
	assert.True(t, strings.Contains(output, lambdaID),
		"log must contain the captured lambda's ID; got: %s", output)
}
