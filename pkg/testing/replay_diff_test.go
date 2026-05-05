package testing

// Plan 03 Task 2 — replay-diff helper unit tests. Covers:
//   - identical streams → nil divergence
//   - structural divergence (length mismatch)
//   - payload divergence (per-event content)
//   - Format() verbatim D5-D2 multi-line shape with flow callsite
//     attribution sourced from Plan 03 Task 0's step_dispatch
//     pos+name extension
//   - integration on a real RunOnceCapturing pair (no divergence path)

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// TestReplay_FirstDivergentEvent_IdenticalStreams_ReturnsNil —
// byte-equal captures produce no divergence.
func TestReplay_FirstDivergentEvent_IdenticalStreams_ReturnsNil(t *testing.T) {
	cap1 := interpreter.NewEventCapture()
	cap2 := interpreter.NewEventCapture()
	cap1.Info("skytime", "event", "flow_start", "name", "users")
	cap2.Info("skytime", "event", "flow_start", "name", "users")
	d := FirstDivergentEvent(cap1, cap2, syntax.Position{})
	assert.Nil(t, d, "identical event streams must produce no divergence")
}

// TestReplay_FirstDivergentEvent_StructuralDivergence — length
// mismatch surfaces at the first missing position.
func TestReplay_FirstDivergentEvent_StructuralDivergence(t *testing.T) {
	cap1 := interpreter.NewEventCapture()
	cap2 := interpreter.NewEventCapture()
	cap1.Info("skytime", "event", "flow_start", "name", "users")
	// cap2 deliberately empty → structural divergence at index 0.
	d := FirstDivergentEvent(cap1, cap2, syntax.Position{})
	require.NotNil(t, d)
	assert.Equal(t, 0, d.Index)
	assert.Equal(t, "<missing>", d.After,
		"missing record on the shorter side must render as <missing>")
}

// TestReplay_FirstDivergentEvent_PayloadDivergence — captures with
// the same length but differing payload at index 1 surface that
// index. ONLY the first divergent record is reported (no cascade).
func TestReplay_FirstDivergentEvent_PayloadDivergence(t *testing.T) {
	cap1 := interpreter.NewEventCapture()
	cap2 := interpreter.NewEventCapture()
	cap1.Info("skytime", "event", "flow_start", "name", "users")
	cap2.Info("skytime", "event", "flow_start", "name", "users")
	cap1.Info("skytime", "event", "step_dispatch", "label", "fetch", "kwargs", "/a")
	cap2.Info("skytime", "event", "step_dispatch", "label", "fetch", "kwargs", "/b")
	d := FirstDivergentEvent(cap1, cap2, syntax.Position{})
	require.NotNil(t, d)
	assert.Equal(t, 1, d.Index)
	assert.Equal(t, "slog", d.Kind)
	assert.Contains(t, d.Before, "/a")
	assert.Contains(t, d.After, "/b")
}

// TestReplay_DivergenceReportFormat — VALIDATION.md per-task map
// cite. Verifies the verbatim D5-D2 multi-line shape AND D5-D3
// flow-callsite attribution sourced from the preceding step_dispatch
// event's `pos` + `name` KV pairs (Plan 03 Task 0).
func TestReplay_DivergenceReportFormat(t *testing.T) {
	cap1 := interpreter.NewEventCapture()
	cap2 := interpreter.NewEventCapture()
	// Force divergence: matching step_dispatch events carry the same
	// pos+name; subsequent activity_started records (here simulated as
	// slog records) carry differing payloads.
	flowFile := "users_flow.star"
	stepPos := syntax.MakePosition(&flowFile, 14, 5)
	cap1.Info("skytime", "event", "step_dispatch", "pos", stepPos, "name", "fetch user", "label", "fetch user")
	cap2.Info("skytime", "event", "step_dispatch", "pos", stepPos, "name", "fetch user", "label", "fetch user")
	cap1.Info("skytime", "event", "activity_started", "kwargs", "/users/octocat")
	cap2.Info("skytime", "event", "activity_started", "kwargs", "/users/foo")

	testFile := "users_test.star"
	testCallsite := syntax.MakePosition(&testFile, 23, 5)
	d := FirstDivergentEvent(cap1, cap2, testCallsite)
	require.NotNil(t, d)

	msg := d.Format()
	assert.Contains(t, msg, "replay diverged")
	assert.True(t,
		strings.Contains(msg, "event 1") || strings.Contains(msg, "event 2"),
		"divergence index should reference the differing record; got: %s", msg)
	assert.Contains(t, msg, "/users/octocat")
	assert.Contains(t, msg, "/users/foo")
	// D5-D3: flow callsite must surface from the preceding step_dispatch.
	assert.Equal(t, int32(14), int32(d.FlowCallsite.Line),
		"FlowCallsite.Line must equal the step_dispatch pos.Line")
	assert.Equal(t, "fetch user", d.StepName)
	assert.Contains(t, msg, "users_flow.star:14:5")
	assert.Contains(t, msg, `(step "fetch user")`)
	// Test callsite must surface from the caller-supplied position.
	assert.Equal(t, int32(23), int32(d.TestCallsite.Line))
	assert.Contains(t, msg, "users_test.star:23:5")
	assert.Contains(t, msg, "(tester.run)")
}

// TestReplay_RunOnceCapturing_NoDivergence — integration: two real
// RunOnceCapturing runs against the same fixture produce identical
// captures, FirstDivergentEvent returns nil. Ties D5-D1 (always-on
// replay) end-to-end with the diff helper.
func TestReplay_RunOnceCapturing_NoDivergence(t *testing.T) {
	src := `flow(
    name="x",
    inputs={"n": "int"},
    steps=[
        if_cond(
            output_alias="r",
            cond=lambda ctx: ctx.n > 0,
            then=[result(value={"k": "v"})],
            else_=[result(value={"k": "v"})],
        ),
    ],
)`
	parsed, hash := helperParseProductionFlow(t, src, "x")
	reg := NewMockRegistry()
	cap1, _, err1 := RunOnceCapturing(parsed, hash, map[string]any{"n": int64(2)}, reg, nil, nil)
	require.NoError(t, err1)
	cap2, _, err2 := RunOnceCapturing(parsed, hash, map[string]any{"n": int64(2)}, reg, nil, nil)
	require.NoError(t, err2)

	d := FirstDivergentEvent(cap1, cap2, syntax.Position{})
	assert.Nil(t, d, "two identical RunOnceCapturing runs must not diverge; got: %+v", d)
}
