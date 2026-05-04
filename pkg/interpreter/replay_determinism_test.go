package interpreter

// Replay-determinism tests for plan 04.2-04 — pin that two consecutive
// runs of the same expression-mode flow produce byte-equal event
// sequences and byte-equal final state. The eventCapturingLogger in
// test_helpers_test.go is mutex-guarded so -race accepts concurrent
// emissions from Temporal's replay machinery.
//
// Pitfall 5 + D3-23: walk_result.go MUST iterate Result.Keys (the
// source insertion order), not `for k := range Result.Values`. Go map
// iteration order is randomized; without the Keys-slice discipline,
// two runs of the same fixture can produce different result_bound
// event payloads and break Temporal replay byte-equality.
//
// Tests are NOT marked t.Parallel() — replay-equality is a sequential
// property; concurrent tests would muddy the per-test logger state.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// runOnceCapturing executes the parsed flow with the given init state
// against a fresh testsuite + eventCapturingLogger. Returns the
// captured records and the final workflow state (nil on error). The
// logger is wired via ts.SetLogger before NewTestWorkflowEnvironment
// so workflow.GetLogger(ctx).Info(...) calls land in the recorder.
func runOnceCapturing(t *testing.T, parsed *ParsedFlow, hash string, init map[string]any) (*eventCapturingLogger, map[string]any, error) {
	t.Helper()

	cap := newEventCapturingLogger()

	registry := NewRegistry()
	require.NoError(t, registry.Register(parsed.Flow.Name, hash, parsed))
	registry.Freeze()

	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(cap)
	env := ts.NewTestWorkflowEnvironment()
	wf := NewWorkflow(registry)
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
	env.ExecuteWorkflow(wf, dag.WorkflowInput{
		FlowName:    parsed.Flow.Name,
		ContentHash: hash,
		InitState:   init,
	})
	require.True(t, env.IsWorkflowCompleted())

	wfErr := env.GetWorkflowError()
	var out map[string]any
	if wfErr == nil {
		require.NoError(t, env.GetWorkflowResult(&out))
	}
	return cap, out, wfErr
}

// TestReplay_DictKeyOrderDeterministic exercises Pitfall 5: the Keys
// slice (`["c","a","b"]` — intentionally non-alphabetical) MUST be
// honored verbatim across two runs. The event sequences and final
// states are compared byte-equal; the result_bound event in
// particular must carry `keys=[c a b]` in BOTH runs.
func TestReplay_DictKeyOrderDeterministic(t *testing.T) {
	src := `flow(
    name="t",
    inputs={"n": "int"},
    steps=[
        if_cond(
            output_alias="r",
            cond=lambda ctx: ctx.n > 0,
            then=[result(value={"c": 1, "a": 2, "b": 3})],
            else_=[result(value={"c": 1, "a": 2, "b": 3})],
        ),
    ],
)`
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	cap1, out1, err1 := runOnceCapturing(t, parsed, hash, map[string]any{"n": int64(5)})
	require.NoError(t, err1)
	cap2, out2, err2 := runOnceCapturing(t, parsed, hash, map[string]any{"n": int64(5)})
	require.NoError(t, err2)

	require.Equal(t, out1, out2,
		"replay determinism: final workflow state must be byte-equal across runs")

	recs1 := cap1.snapshot()
	recs2 := cap2.snapshot()

	ser1 := serializeRecords(recs1)
	ser2 := serializeRecords(recs2)
	require.Equal(t, ser1, ser2,
		"replay determinism: event sequences must be byte-equal across two runs")

	// Pitfall 5 audit: the result_bound event MUST carry the Keys slice
	// in source insertion order (c, a, b — NOT alphabetical).
	require.True(t, strings.Contains(ser1, "keys=[c a b]"),
		"Pitfall 5: keys order must preserve source insertion (got serialized: %q)", ser1)

	// Exactly one result_bound event per run (the chosen branch's
	// terminator binds once).
	bound1 := findEventRecords(recs1, "result_bound")
	require.Len(t, bound1, 1, "expected exactly one result_bound event per run")
	require.Equal(t, "r", bound1[0].Attrs["alias"])
	// keys attribute is the []string slice — verify exact insertion order.
	require.Equal(t, []string{"c", "a", "b"}, bound1[0].Attrs["keys"])
}

// TestReplay_FailMessageDeterministic pins that fail-message
// resolution (literal or interpolated) is byte-stable across replay.
// The fixture interpolates `${ctx.repo}` so the resolved message
// must surface deterministically in env.GetWorkflowError() across
// two runs with the same input.
func TestReplay_FailMessageDeterministic(t *testing.T) {
	src := `flow(
    name="t",
    inputs={"repo": "string"},
    steps=[
        if_cond(
            output_alias="r",
            cond=lambda ctx: True,
            then=[fail("repo ${ctx.repo} not found")],
            else_=[result(value={"k": 1})],
        ),
    ],
)`
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	runOne := func() string {
		_, _, wfErr := runOnceCapturing(t, parsed, hash, map[string]any{"repo": "octocat/Hello-World"})
		require.Error(t, wfErr,
			"expression-mode fail() must surface as workflow error")
		require.Contains(t, wfErr.Error(), "octocat/Hello-World",
			"fail-message interpolation must include resolved ctx.repo")
		return wfErr.Error()
	}

	msg1 := runOne()
	msg2 := runOne()
	require.Equal(t, msg1, msg2,
		"replay determinism: fail-message resolution must be byte-equal across two runs")
}

// TestReplay_LeadingBodyDeterministic pins that an expression-mode
// branch with leading body (script) followed by a result terminator
// produces deterministic state across runs — both ctx.<script_alias>
// and ctx.<result_alias> must be byte-equal.
//
// The fixture has `then=[script(...), result(value={"x": ctx.m})]`
// so the result lambda reads ctx.m which the leading script set —
// this verifies execution order (leading body BEFORE result lambda
// evaluation).
func TestReplay_LeadingBodyDeterministic(t *testing.T) {
	src := `flow(
    name="t",
    inputs={"n": "int"},
    steps=[
        if_cond(
            output_alias="r",
            cond=lambda ctx: ctx.n > 0,
            then=[
                script(id="s", fn=lambda ctx: ctx.n * 2, output_alias="m"),
                result(value={"x": ctx.m}),
            ],
            else_=[result(value={"x": -1})],
        ),
    ],
)`
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	_, out1, err1 := runOnceCapturing(t, parsed, hash, map[string]any{"n": int64(5)})
	require.NoError(t, err1)
	_, out2, err2 := runOnceCapturing(t, parsed, hash, map[string]any{"n": int64(5)})
	require.NoError(t, err2)

	require.Equal(t, out1, out2,
		"replay determinism: leading-body + result combination must produce byte-equal state")

	require.Contains(t, out1, "m", "ctx.m must be set by the leading script")
	require.Contains(t, out1, "r", "ctx.r must be bound by the trailing result")

	rDict, ok := out1["r"].(map[string]any)
	require.True(t, ok, "ctx.r must be a dict; got %T", out1["r"])
	// EqualValues coerces JSON-roundtripped float64 ↔ int64.
	require.EqualValues(t, 10, rDict["x"], "ctx.r.x must equal ctx.m (=ctx.n*2=10)")
	require.EqualValues(t, 10, out1["m"], "ctx.m must equal ctx.n*2 = 10")
}
