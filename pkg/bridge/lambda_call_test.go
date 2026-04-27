package bridge

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// makeCapturedLambda runs a small Starlark snippet, looks up `f` (which the
// caller is expected to bind via `f = lambda ctx: ...`), and wraps it in a
// CapturedLambda. Used as a test fixture builder by every CallLambda test.
//
// The init exec uses LambdaTimeGlobals() as predeclared so test snippets can
// reference any of the D-20 builtins (notably `sum`, which is not in
// starlark.Universe — it's a locally-implemented builtin).
func makeCapturedLambda(t *testing.T, src string) *dag.CapturedLambda {
	t.Helper()
	thread := &starlark.Thread{Name: "test-init"}
	opts := &syntax.FileOptions{}
	globals, err := starlark.ExecFileOptions(opts, thread, "test.star", src, LambdaTimeGlobals())
	require.NoError(t, err, "init exec failed for src: %s", src)

	fn, ok := globals["f"].(*starlark.Function)
	require.True(t, ok, `expected globals["f"] to be a *starlark.Function`)

	return &dag.CapturedLambda{
		ID:       dag.ComputeLambdaID([]byte(src), fn.Position()),
		Fn:       fn,
		Pos:      fn.Position(),
		FreeVars: starlark.StringDict{},
	}
}

// TestCallLambda_FreshThread (Test 1) — same captured lambda evaluated twice
// with different state must produce different correct results. Reusing a
// thread across calls (Pitfall #1) could surface here as cross-call leakage;
// the test pins fresh-thread behavior.
func TestCallLambda_FreshThread(t *testing.T) {
	captured := makeCapturedLambda(t, `f = lambda ctx: ctx.x`)
	ctx := context.Background()

	v1, err := CallLambda(ctx, captured, map[string]any{"x": 1}, CallOptions{})
	require.NoError(t, err)
	v1Int, ok := v1.(starlark.Int)
	require.True(t, ok)
	got1, _ := v1Int.Int64()
	assert.Equal(t, int64(1), got1)

	v2, err := CallLambda(ctx, captured, map[string]any{"x": 2}, CallOptions{})
	require.NoError(t, err)
	v2Int, ok := v2.(starlark.Int)
	require.True(t, ok)
	got2, _ := v2Int.Int64()
	assert.Equal(t, int64(2), got2)
}

// TestCallLambda_DotAccess_DSL09 (Test 5) — lambdas reach into nested state
// via dot notation. Integration of ToStarlarkStruct + CallLambda for the
// load-bearing DSL-09 contract.
func TestCallLambda_DotAccess_DSL09(t *testing.T) {
	captured := makeCapturedLambda(t, `f = lambda ctx: ctx.req.repo_name + "/issues"`)

	v, err := CallLambda(context.Background(), captured, map[string]any{
		"req": map[string]any{"repo_name": "acme/widget"},
	}, CallOptions{})
	require.NoError(t, err)
	assert.Equal(t, starlark.String("acme/widget/issues"), v)
}

// TestCallLambda_PrintHookRouted (Test 4) — print() output reaches the
// configured PrintSink (D-21).
func TestCallLambda_PrintHookRouted(t *testing.T) {
	captured := makeCapturedLambda(t, `f = lambda ctx: print("hello from lambda") or 42`)

	var msgs []string
	opts := CallOptions{
		PrintSink: func(_ context.Context, msg string) {
			msgs = append(msgs, msg)
		},
	}
	v, err := CallLambda(context.Background(), captured, map[string]any{}, opts)
	require.NoError(t, err)

	vInt, ok := v.(starlark.Int)
	require.True(t, ok)
	got, _ := vInt.Int64()
	assert.Equal(t, int64(42), got)

	require.Len(t, msgs, 1)
	assert.Equal(t, "hello from lambda", msgs[0])
}

// TestCallLambda_DefaultPrintRoutesToSlog (Test 6) — without a PrintSink, the
// lambda's print output reaches a configured slog.Logger.
func TestCallLambda_DefaultPrintRoutesToSlog(t *testing.T) {
	captured := makeCapturedLambda(t, `f = lambda ctx: print("via slog") or 1`)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	_, err := CallLambda(context.Background(), captured, map[string]any{},
		CallOptions{Logger: logger})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "via slog",
		"slog handler must receive the print payload; got: %q", buf.String())
}

// TestCallLambda_MaxExecutionStepsDefault (Test 2) — the default cap of
// 10_000_000 steps stops a runaway computation. We use a small override (1000)
// so the failure is fast and deterministic regardless of the runtime tuning.
func TestCallLambda_MaxExecutionStepsDefault(t *testing.T) {
	captured := makeCapturedLambda(t, `f = lambda ctx: sum([i for i in range(10000000)])`)

	_, err := CallLambda(context.Background(), captured, map[string]any{}, CallOptions{
		MaxExecutionSteps: 1000,
	})
	require.Error(t, err, "execution should be cut off by step limit")
}

// TestCallLambda_DefaultMaxExecutionStepsConstant (Test 3 partial) — verifies
// the package exports DefaultMaxExecutionSteps with the D-22 value.
func TestCallLambda_DefaultMaxExecutionStepsConstant(t *testing.T) {
	assert.Equal(t, uint64(10_000_000), uint64(DefaultMaxExecutionSteps),
		"D-22: default max execution steps must be 10_000_000")
}

// TestCallLambda_ConcurrentSafety (Test 7) — 50 parallel calls on the same
// captured lambda each see their own state. Run with -race; if a thread were
// reused or state leaked, the race detector would flag it.
func TestCallLambda_ConcurrentSafety(t *testing.T) {
	captured := makeCapturedLambda(t, `f = lambda ctx: ctx.x * 2`)

	const N = 50
	var wg sync.WaitGroup
	results := make([]int64, N)
	errs := make([]error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			v, err := CallLambda(context.Background(), captured,
				map[string]any{"x": n}, CallOptions{})
			if err != nil {
				errs[n] = err
				return
			}
			vInt, ok := v.(starlark.Int)
			if !ok {
				errs[n] = assertErrorf("expected starlark.Int, got %T", v)
				return
			}
			got, _ := vInt.Int64()
			results[n] = got
		}(i)
	}
	wg.Wait()

	for i := 0; i < N; i++ {
		require.NoErrorf(t, errs[i], "call %d errored", i)
		assert.Equal(t, int64(i*2), results[i], "call %d wrong result", i)
	}
}

// assertErrorf is a tiny helper to build an error from a format string in the
// concurrent test (we cannot call require.FailNow inside a goroutine).
func assertErrorf(format string, args ...any) error {
	return &simpleErr{msg: format, args: args}
}

type simpleErr struct {
	msg  string
	args []any
}

func (e *simpleErr) Error() string {
	if len(e.args) == 0 {
		return e.msg
	}
	return strings.NewReplacer("%T", "(unprintable)").Replace(e.msg)
}

// TestCallLambda_CancelWatchdog (Test 8) — closing the Cancel channel stops a
// long-running lambda. We use a tight Starlark loop bounded by a small
// MaxExecutionSteps as the upper-bound sanity backstop, then assert
// completion happens within the watchdog window.
func TestCallLambda_CancelWatchdog(t *testing.T) {
	captured := makeCapturedLambda(t, `f = lambda ctx: sum([i for i in range(10000000)])`)

	cancel := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(cancel)
	}()

	start := time.Now()
	_, err := CallLambda(context.Background(), captured, map[string]any{}, CallOptions{
		MaxExecutionSteps: 100_000_000, // big enough that step limit doesn't fire first
		Cancel:            cancel,
	})
	elapsed := time.Since(start)

	require.Error(t, err, "expected cancellation error")
	assert.Less(t, elapsed, 5*time.Second,
		"cancellation should land well within 5s; took %v", elapsed)
}

// TestCallLambda_ZeroValueOptions (Test 9) — CallOptions{} (zero value)
// works and uses defaults for every field.
func TestCallLambda_ZeroValueOptions(t *testing.T) {
	captured := makeCapturedLambda(t, `f = lambda ctx: ctx.greeting`)

	v, err := CallLambda(context.Background(), captured,
		map[string]any{"greeting": "hi"}, CallOptions{})
	require.NoError(t, err)
	assert.Equal(t, starlark.String("hi"), v)
}

// TestCallLambda_StateConversionError surfaces when the state map contains an
// unsupported Go type (e.g. chan int) — the error wraps ToStarlarkStruct's
// message and never reaches the Starlark thread.
func TestCallLambda_StateConversionError(t *testing.T) {
	captured := makeCapturedLambda(t, `f = lambda ctx: ctx.x`)

	_, err := CallLambda(context.Background(), captured, map[string]any{
		"ch": make(chan int),
	}, CallOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported type")
}
