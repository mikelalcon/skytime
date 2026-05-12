package interpreter

// Tests for plan 07.2.1-03: walkLog walker + walkBody counter exclusion.
//
// All tests drive the parser end-to-end (parseSrcAsFlow + runOnceCapturing)
// so the LogStep / lambdas / desugared-MsgFn / attrs-lambda plumbing
// exercises the same path that production workflows use.
//
// The eventCapturingLogger in test_helpers_test.go records every
// workflow.GetLogger(ctx).<Level> call. User-message records (the ones
// produced by walkLog's level-routed logger call) have msg prefix
// "[skytime/log] " — that's our load-bearing test marker.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// findLogUserMessageRecords returns the records whose Msg starts with
// "[skytime/log] " — the load-bearing marker walkLog applies to the
// user-visible level-routed record (mirrors [skytime/print] precedent).
func findLogUserMessageRecords(recs []capturedRecord) []capturedRecord {
	out := []capturedRecord{}
	for _, r := range recs {
		if strings.HasPrefix(r.Msg, "[skytime/log] ") {
			out = append(out, r)
		}
	}
	return out
}

// findDispatchEventsByKind returns step_dispatch records with the given
// kind attr value.
func findDispatchEventsByKind(recs []capturedRecord, kind string) []capturedRecord {
	out := []capturedRecord{}
	for _, r := range recs {
		if r.Attrs["event"] == "step_dispatch" && r.Attrs["kind"] == kind {
			out = append(out, r)
		}
	}
	return out
}

// TestWalkLog_AllFourLevels — log.<level>("hi") routes through the
// matching Logger method (Debug/Info/Warn/Error). Because the captured
// logger records all four methods the same way (msg + attrs), the
// load-bearing signal is that the user-message record exists AND that
// the dispatch event records the level=<level> attribute.
func TestWalkLog_AllFourLevels(t *testing.T) {
	for _, level := range []string{"info", "warn", "error", "debug"} {
		t.Run(level, func(t *testing.T) {
			src := fmt.Sprintf(`flow(name="t", inputs={}, steps=[log.%s("hello")])`, level)
			parsed := parseSrcAsFlow(t, src, "t")
			hash := contentHashForSrc(src)

			cap, _, err := runOnceCapturing(t, parsed, hash, map[string]any{})
			require.NoError(t, err)

			recs := cap.snapshot()
			userMsgs := findLogUserMessageRecords(recs)
			require.Len(t, userMsgs, 1, "expected exactly one [skytime/log] record")
			require.Equal(t, "[skytime/log] hello", userMsgs[0].Msg)

			dispatches := findDispatchEventsByKind(recs, "log")
			require.Len(t, dispatches, 1, "expected one step_dispatch kind=log")
			require.Equal(t, level, dispatches[0].Attrs["level"])
		})
	}
}

// TestWalkLog_InterpolatedMsg — ${ctx.x} interpolation desugars to a
// MsgFn lambda that the walker evaluates per-dispatch; the resolved
// user message reflects the runtime ctx.x value. Uses a string input
// to avoid the testsuite's JSON-roundtrip float64 coercion of integers.
func TestWalkLog_InterpolatedMsg(t *testing.T) {
	src := `flow(name="t", inputs={"x": "string"}, steps=[log.info("v=${ctx.x}")])`
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	cap, _, err := runOnceCapturing(t, parsed, hash, map[string]any{"x": "abc"})
	require.NoError(t, err)

	userMsgs := findLogUserMessageRecords(cap.snapshot())
	require.Len(t, userMsgs, 1)
	require.Equal(t, "[skytime/log] v=abc", userMsgs[0].Msg)
}

// TestWalkLog_AttrsInsertionOrder — non-alphabetical attrs keys must
// surface to the logger in source insertion order via *starlark.Dict.Items().
func TestWalkLog_AttrsInsertionOrder(t *testing.T) {
	src := `flow(name="t", inputs={}, steps=[
  log.info("hi", attrs=lambda ctx: {"zebra": 1, "alpha": 2, "monkey": 3}),
])`
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	cap, _, err := runOnceCapturing(t, parsed, hash, map[string]any{})
	require.NoError(t, err)

	userMsgs := findLogUserMessageRecords(cap.snapshot())
	require.Len(t, userMsgs, 1)
	// The captured-record Attrs map is unordered (Go map), so to verify
	// insertion order we need to inspect the raw keyvals slice. The
	// capturer stores attr ordering as map; instead, verify all keys
	// arrived with expected values. Order is exercised by the replay
	// test (TestReplay_LogStep_AttrsByteStable) which compares the
	// SERIALIZED record stream across runs.
	require.EqualValues(t, int64(1), userMsgs[0].Attrs["zebra"])
	require.EqualValues(t, int64(2), userMsgs[0].Attrs["alpha"])
	require.EqualValues(t, int64(3), userMsgs[0].Attrs["monkey"])
}

// TestWalkLog_AttrsScalarCoercion — different scalar starlark types
// map to typed slog attrs and surface on the user-message record.
func TestWalkLog_AttrsScalarCoercion(t *testing.T) {
	src := `flow(name="t", inputs={}, steps=[
  log.info("hi", attrs=lambda ctx: {"s": "abc", "i": 7, "f": 3.5, "b": True, "n": None}),
])`
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	cap, _, err := runOnceCapturing(t, parsed, hash, map[string]any{})
	require.NoError(t, err)

	userMsgs := findLogUserMessageRecords(cap.snapshot())
	require.Len(t, userMsgs, 1)
	attrs := userMsgs[0].Attrs
	require.Equal(t, "abc", attrs["s"])
	require.EqualValues(t, 7, attrs["i"])
	require.InEpsilon(t, 3.5, attrs["f"].(float64), 1e-9)
	require.Equal(t, true, attrs["b"])
	require.Nil(t, attrs["n"])
}

// TestWalkLog_ThreeRecordContract — D-7.2.1-12: step_dispatch (kind=log)
// + user-message + step_complete (kind=log) = exactly three records per call.
func TestWalkLog_ThreeRecordContract(t *testing.T) {
	src := `flow(name="t", inputs={}, steps=[log.info("hello")])`
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	cap, _, err := runOnceCapturing(t, parsed, hash, map[string]any{})
	require.NoError(t, err)

	recs := cap.snapshot()
	// Count kind=log dispatch + kind=log complete + user-message records.
	dispatchCount := len(findDispatchEventsByKind(recs, "log"))
	completeCount := 0
	for _, r := range recs {
		if r.Attrs["event"] == "step_complete" && r.Attrs["kind"] == "log" {
			completeCount++
		}
	}
	userMsgCount := len(findLogUserMessageRecords(recs))
	require.Equal(t, 1, dispatchCount, "expected 1 step_dispatch kind=log")
	require.Equal(t, 1, completeCount, "expected 1 step_complete kind=log")
	require.Equal(t, 1, userMsgCount, "expected 1 [skytime/log] user-message")
}

// TestWalkLog_RejectReservedKey — attrs dict with a reserved slog key
// must surface as a workflow error containing the offending key.
func TestWalkLog_RejectReservedKey(t *testing.T) {
	for _, key := range []string{"level", "msg", "time", "source"} {
		t.Run(key, func(t *testing.T) {
			src := fmt.Sprintf(`flow(name="t", inputs={}, steps=[
  log.info("hi", attrs=lambda ctx: {%q: "oops"}),
])`, key)
			parsed := parseSrcAsFlow(t, src, "t")
			hash := contentHashForSrc(src)
			_, _, err := runOnceCapturing(t, parsed, hash, map[string]any{})
			require.Error(t, err)
			require.Contains(t, err.Error(), key)
			require.Contains(t, err.Error(), "reserved")
		})
	}
}

// TestWalkLog_RejectTooManyAttrs — dict with 33 entries exceeds the
// hard-coded 32-attr cap (D-7.2.1-07) and surfaces a workflow error.
func TestWalkLog_RejectTooManyAttrs(t *testing.T) {
	pairs := make([]string, 33)
	for i := range pairs {
		pairs[i] = fmt.Sprintf(`"k%d": %d`, i, i)
	}
	src := fmt.Sprintf(`flow(name="t", inputs={}, steps=[
  log.info("hi", attrs=lambda ctx: {%s}),
])`, strings.Join(pairs, ", "))
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	_, _, err := runOnceCapturing(t, parsed, hash, map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "33")
	require.Contains(t, err.Error(), "32")
}

// TestWalkLog_RejectBadKeyShape — key not matching the identifier
// regex must produce a workflow error containing the offending key.
func TestWalkLog_RejectBadKeyShape(t *testing.T) {
	src := `flow(name="t", inputs={}, steps=[
  log.info("hi", attrs=lambda ctx: {"weird key!": 1}),
])`
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	_, _, err := runOnceCapturing(t, parsed, hash, map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "identifier")
}

// TestWalkLog_RejectAttrsLambdaNonDict — attrs lambda returning a
// non-dict value surfaces a workflow error.
func TestWalkLog_RejectAttrsLambdaNonDict(t *testing.T) {
	src := `flow(name="t", inputs={}, steps=[log.info("hi", attrs=lambda ctx: 42)])`
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	_, _, err := runOnceCapturing(t, parsed, hash, map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "dict")
}

// TestWalkBody_LogStepsExcludedFromCounter — flow with [script, log,
// script] produces step_dispatch kind=script events with total=2 (the
// LogStep is excluded from the denominator per D-7.2.1-13 / Pitfall 7),
// and the two scripts see idx=1 and idx=2 (the LogStep does NOT
// advance the counter).
func TestWalkBody_LogStepsExcludedFromCounter(t *testing.T) {
	src := `flow(name="t", inputs={}, steps=[
  script(id="a", fn=lambda ctx: 1, output_alias="_a"),
  log.info("middle"),
  script(id="b", fn=lambda ctx: 2, output_alias="_b"),
])`
	parsed := parseSrcAsFlow(t, src, "t")
	hash := contentHashForSrc(src)

	cap, _, err := runOnceCapturing(t, parsed, hash, map[string]any{})
	require.NoError(t, err)

	scriptDispatches := findDispatchEventsByKind(cap.snapshot(), "script")
	require.Len(t, scriptDispatches, 2, "expected two script dispatches")
	for _, d := range scriptDispatches {
		require.EqualValues(t, 2, d.Attrs["total"],
			"script dispatch total must be 2 (LogStep excluded from denominator); got %v", d.Attrs["total"])
	}
	// idx values must be 1 and 2 (in order); the LogStep does not bump the counter.
	require.EqualValues(t, 1, scriptDispatches[0].Attrs["idx"], "first script idx=1")
	require.EqualValues(t, 2, scriptDispatches[1].Attrs["idx"], "second script idx=2")
}
