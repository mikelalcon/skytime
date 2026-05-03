package interpreter

// White-box: package interpreter so resolveKwargs + interpreter struct
// fields are directly accessible.

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// helperBuildParsedFlowWithLambdas registers multiple captured lambdas
// under the synthesized IDs returned. Each (name, src) pair compiles a
// `f = lambda ctx: ...` Starlark snippet via compileLambdaForTest. Returns
// the ParsedFlow plus the slice of lambda IDs in input order.
func helperBuildParsedFlowWithLambdas(t *testing.T, flowName string, srcs []struct{ Name, Src string }) (*ParsedFlow, []string) {
	t.Helper()
	filename := flowName + ".star"
	parsed := &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    syntax.MakePosition(&filename, 1, 1),
			Name:   flowName,
			Inputs: map[string]string{},
			Body:   nil,
		},
		Lambdas: map[string]*dag.CapturedLambda{},
	}
	ids := make([]string, 0, len(srcs))
	for _, s := range srcs {
		fn, srcBytes := compileLambdaForTest(t, s.Name+".star", s.Src)
		pos := fn.Position()
		id := dag.ComputeLambdaID(srcBytes, pos)
		// Ensure unique per call: append the source name to disambiguate
		// multiple lambdas with identical compiled bodies.
		id = id + ":" + s.Name
		parsed.Lambdas[id] = &dag.CapturedLambda{
			ID:       id,
			Fn:       fn,
			Pos:      pos,
			FreeVars: starlark.StringDict{},
		}
		ids = append(ids, id)
	}
	return parsed, ids
}

// TestResolveKwargs_NoLambdas_ReturnsOriginalShape: a Kwargs with all
// string values returns the SAME *Dict pointer (allocation-free fast path).
func TestResolveKwargs_NoLambdas_ReturnsOriginalShape(t *testing.T) {
	kw := starlark.NewDict(2)
	require.NoError(t, kw.SetKey(starlark.String("path"), starlark.String("/repos/octocat")))
	require.NoError(t, kw.SetKey(starlark.String("method"), starlark.String("GET")))
	kw.Freeze()
	ref := &dag.ActionRef{Kind_: "http.get", Kwargs: kw}

	parsed, _ := helperBuildParsedFlowWithLambdas(t, "noop", nil)
	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) error {
		i := newInterpreter(ctx, registry, parsed, "h",
			map[string]any{}, workflow.GetLogger(ctx))
		out, err := i.resolveKwargs(ctx, ref)
		if err != nil {
			return err
		}
		// Same pointer (fast path).
		if out != kw {
			t.Errorf("expected same dict pointer; got new dict")
		}
		return nil
	}
	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// TestResolveKwargs_SingleLambda_ResolvesToString: one lambda kwarg
// "path" = lambda ctx: "x" + str(ctx.r) against state {r: "VAL"} resolves
// to starlark.String("xVAL").
func TestResolveKwargs_SingleLambda_ResolvesToString(t *testing.T) {
	parsed, ids := helperBuildParsedFlowWithLambdas(t, "singlelambda", []struct{ Name, Src string }{
		{Name: "path", Src: "f = lambda ctx: \"x\" + str(ctx.r)\n"},
	})
	captured := parsed.Lambdas[ids[0]]

	kw := starlark.NewDict(1)
	require.NoError(t, kw.SetKey(starlark.String("path"), dag.NewStarlarkLambda(captured)))
	kw.Freeze()
	ref := &dag.ActionRef{Kind_: "http.get", Kwargs: kw}

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) (string, error) {
		i := newInterpreter(ctx, registry, parsed, "h",
			map[string]any{"r": "VAL"}, workflow.GetLogger(ctx))
		out, err := i.resolveKwargs(ctx, ref)
		if err != nil {
			return "", err
		}
		v, _, _ := out.Get(starlark.String("path"))
		s, ok := v.(starlark.String)
		if !ok {
			t.Errorf("expected starlark.String, got %T (%s)", v, v.Type())
			return "", nil
		}
		return string(s), nil
	}
	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, "xVAL", result)
}

// TestResolveKwargs_LambdaReturnsNonString_Errors: a lambda that returns
// starlark.Int(5) instead of a string surfaces an error mentioning the
// kwarg name "path" and the actual type "int".
func TestResolveKwargs_LambdaReturnsNonString_Errors(t *testing.T) {
	parsed, ids := helperBuildParsedFlowWithLambdas(t, "nonstring", []struct{ Name, Src string }{
		{Name: "path", Src: "f = lambda ctx: 5\n"},
	})
	captured := parsed.Lambdas[ids[0]]

	kw := starlark.NewDict(1)
	require.NoError(t, kw.SetKey(starlark.String("path"), dag.NewStarlarkLambda(captured)))
	kw.Freeze()
	ref := &dag.ActionRef{Kind_: "http.get", Kwargs: kw}

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) error {
		i := newInterpreter(ctx, registry, parsed, "h",
			map[string]any{}, workflow.GetLogger(ctx))
		_, err := i.resolveKwargs(ctx, ref)
		return err
	}
	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())

	wfErr := env.GetWorkflowError()
	require.Error(t, wfErr)
	msg := wfErr.Error()
	assert.Contains(t, msg, `"path"`, "error must mention kwarg name 'path'")
	assert.Contains(t, msg, "int", "error must mention actual type 'int'")
	assert.Contains(t, msg, "expected string", "error must say 'expected string'")
}

// TestResolveKwargs_NilKwargs_ReturnsNil: ActionRef with Kwargs==nil
// returns (nil, nil) without error.
func TestResolveKwargs_NilKwargs_ReturnsNil(t *testing.T) {
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: nil}

	parsed, _ := helperBuildParsedFlowWithLambdas(t, "nilkw", nil)
	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) error {
		i := newInterpreter(ctx, registry, parsed, "h",
			map[string]any{}, workflow.GetLogger(ctx))
		out, err := i.resolveKwargs(ctx, ref)
		if err != nil {
			return err
		}
		if out != nil {
			t.Errorf("expected nil dict; got %v", out)
		}
		return nil
	}
	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// orderRecord is a thread-safe slice that captures the order in which
// the deterministic-order test's lambdas execute (via PrintSink). Used
// to prove *starlark.Dict.Items() is insertion-order, NOT randomized.
type orderRecord struct {
	mu    sync.Mutex
	order []string
}

func (r *orderRecord) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, s)
}

func (r *orderRecord) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// TestResolveKwargs_DeterministicOrder: insert kwargs in order [z, a, m]
// where each value is a lambda that prints its key. Assert evaluation
// order is [z, a, m] regardless of Go's hash randomization. Proves the
// determinism guarantee from RESEARCH §Pattern 4.
func TestResolveKwargs_DeterministicOrder(t *testing.T) {
	parsed, ids := helperBuildParsedFlowWithLambdas(t, "detorder", []struct{ Name, Src string }{
		{Name: "z", Src: "f = lambda ctx: print('z') or 'Z'\n"},
		{Name: "a", Src: "f = lambda ctx: print('a') or 'A'\n"},
		{Name: "m", Src: "f = lambda ctx: print('m') or 'M'\n"},
	})

	kw := starlark.NewDict(3)
	// Insertion order: z, a, m. *starlark.Dict.Items() preserves this.
	require.NoError(t, kw.SetKey(starlark.String("z"), dag.NewStarlarkLambda(parsed.Lambdas[ids[0]])))
	require.NoError(t, kw.SetKey(starlark.String("a"), dag.NewStarlarkLambda(parsed.Lambdas[ids[1]])))
	require.NoError(t, kw.SetKey(starlark.String("m"), dag.NewStarlarkLambda(parsed.Lambdas[ids[2]])))
	kw.Freeze()
	ref := &dag.ActionRef{Kind_: "fake.echo", Kwargs: kw}

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	rec := &orderRecord{}

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	// Run the lambdas multiple times to amortize any one-off ordering
	// luck against Go map randomization. *starlark.Dict.Items() must be
	// stable across all runs.
	const runs = 5
	wfFn := func(ctx workflow.Context) error {
		i := newInterpreter(ctx, registry, parsed, "h",
			map[string]any{}, workflow.GetLogger(ctx))
		for r := 0; r < runs; r++ {
			out, err := i.resolveKwargs(ctx, ref)
			if err != nil {
				return err
			}
			// Capture observed iteration order via Items() of the OUTPUT
			// dict, which mirrors the iteration order resolveKwargs walked.
			for _, item := range out.Items() {
				if ks, ok := item[0].(starlark.String); ok {
					rec.record("out:" + string(ks))
				}
			}
		}
		return nil
	}
	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	got := rec.snapshot()
	require.Len(t, got, 3*runs)
	expectedOne := []string{"out:z", "out:a", "out:m"}
	for r := 0; r < runs; r++ {
		slice := got[r*3 : (r+1)*3]
		assert.Equal(t, expectedOne, slice, "run %d order must be [z a m] (insertion order)", r)
	}
}

// TestResolveKwargs_LambdaErrorPropagates: a lambda that calls
// fail("custom reason") inside its body propagates as an error whose
// message contains "custom reason" and the kwarg name.
func TestResolveKwargs_LambdaErrorPropagates(t *testing.T) {
	parsed, ids := helperBuildParsedFlowWithLambdas(t, "failreason", []struct{ Name, Src string }{
		{Name: "path", Src: "f = lambda ctx: fail(\"custom reason\")\n"},
	})
	captured := parsed.Lambdas[ids[0]]

	kw := starlark.NewDict(1)
	require.NoError(t, kw.SetKey(starlark.String("path"), dag.NewStarlarkLambda(captured)))
	kw.Freeze()
	ref := &dag.ActionRef{Kind_: "http.get", Kwargs: kw}

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, "h", parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wfFn := func(ctx workflow.Context) error {
		i := newInterpreter(ctx, registry, parsed, "h",
			map[string]any{}, workflow.GetLogger(ctx))
		_, err := i.resolveKwargs(ctx, ref)
		return err
	}
	env.RegisterWorkflow(wfFn)
	env.ExecuteWorkflow(wfFn)
	require.True(t, env.IsWorkflowCompleted())

	wfErr := env.GetWorkflowError()
	require.Error(t, wfErr)
	msg := wfErr.Error()
	assert.True(t, strings.Contains(msg, "custom reason"),
		"error must contain user's fail() reason; got: %s", msg)
	assert.Contains(t, msg, `"path"`, "error must mention kwarg name 'path'")
}
