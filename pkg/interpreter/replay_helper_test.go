package interpreter

// White-box test: lives in package interpreter so the existing
// parseSrcAsFlow + contentHashForSrc helpers from test_helpers_test.go
// are reachable directly. Plan 03 Task 1 prefers white-box over
// black-box because parseSrcAsFlow is the canonical "build a
// *ParsedFlow from .star source" pattern across the pkg/interpreter
// test suite (used in replay_determinism_test.go, walk_*_test.go).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReplay_DeterministicEventSequence is the named test from the
// VALIDATION.md per-task map for TEST-04. Two consecutive
// RunOnceCapturing calls on an if_cond/script-only flow (no
// activities; mockCallback=nil) must produce byte-equal serialized
// event streams.
func TestReplay_DeterministicEventSequence(t *testing.T) {
	src := `flow(
    name="x",
    inputs={"n": "int"},
    steps=[
        if_cond(
            output_alias="out",
            cond=lambda ctx: ctx.n > 0,
            then=[result(value={"k": "v", "n": ctx.n})],
            else_=[result(value={"k": "v", "n": ctx.n})],
        ),
    ],
)`
	parsed := parseSrcAsFlow(t, src, "x")
	hash := contentHashForSrc(src)

	cap1, out1, err1 := RunOnceCapturing(parsed, hash, map[string]any{"n": int64(5)}, nil)
	require.NoError(t, err1)
	cap2, out2, err2 := RunOnceCapturing(parsed, hash, map[string]any{"n": int64(5)}, nil)
	require.NoError(t, err2)

	assert.Equal(t, out1, out2,
		"replay determinism: final state must be byte-equal across runs")
	assert.Equal(t, cap1.Serialize(), cap2.Serialize(),
		"replay determinism: two consecutive RunOnceCapturing calls must produce byte-equal event streams (D5-D1)")
}

// TestRunOnceCapturing_NilCallback_BackwardCompat: passing nil
// mockCallback for an activity-free flow succeeds (no panic) and
// captures non-empty events. Pins the contract that pkg/interpreter's
// existing replay tests rely on.
func TestRunOnceCapturing_NilCallback_BackwardCompat(t *testing.T) {
	src := `flow(
    name="x",
    inputs={},
    steps=[
        script(id="s", fn=lambda ctx: {"k": "v"}, output_alias="o"),
    ],
)`
	parsed := parseSrcAsFlow(t, src, "x")
	hash := contentHashForSrc(src)
	cap, out, err := RunOnceCapturing(parsed, hash, map[string]any{}, nil)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.NotEmpty(t, cap.Serialize(),
		"capture buffer must contain at least one slog event (flow_start/flow_complete)")
}

// TestRunOnceCapturing_NilParsed: caller programming-error guard.
func TestRunOnceCapturing_NilParsed(t *testing.T) {
	_, _, err := RunOnceCapturing(nil, "h", map[string]any{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsed must not be nil")
}

// TestEventCapture_LogLogger: the EventCapture type must satisfy
// log.Logger (compile-time assertion at the declaration site is
// already in replay_helper.go; this test exercises the four Info /
// Debug / Warn / Error methods directly).
func TestEventCapture_LogLogger(t *testing.T) {
	c := NewEventCapture()
	c.Debug("d", "k", "v")
	c.Info("i", "k", "v")
	c.Warn("w", "k", "v")
	c.Error("e", "k", "v")
	snap := c.Snapshot()
	require.Len(t, snap, 4)
	assert.Equal(t, "DEBUG", snap[0].Level)
	assert.Equal(t, "INFO", snap[1].Level)
	assert.Equal(t, "WARN", snap[2].Level)
	assert.Equal(t, "ERROR", snap[3].Level)
}
