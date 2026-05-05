package testing

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJSONEvent_FieldTags — capitalized keys are LOAD-BEARING for
// stdlib cmd/test2json compatibility. Any drift here breaks
// gotestsum / tparse / GitHub Actions test annotations.
func TestJSONEvent_FieldTags(t *testing.T) {
	ev := JSONEvent{
		Time:    time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC),
		Action:  "pass",
		Package: "users_test.star",
		Test:    "test_existing_user",
		Elapsed: 0.04,
	}
	b, err := json.Marshal(ev)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, `"Time":`)
	assert.Contains(t, s, `"Action":"pass"`)
	assert.Contains(t, s, `"Package":"users_test.star"`)
	assert.Contains(t, s, `"Test":"test_existing_user"`)
	assert.Contains(t, s, `"Elapsed":0.04`)
}

// TestJSONEvent_OmitemptyTestAndOutput — start/end events for a
// whole package omit Test, Output, Elapsed. Without omitempty the
// records would carry zero-value fields and break consumers that key
// on field presence.
func TestJSONEvent_OmitemptyTestAndOutput(t *testing.T) {
	ev := JSONEvent{
		Time:    time.Now().UTC(),
		Action:  "start",
		Package: "users_test.star",
	}
	b, err := json.Marshal(ev)
	require.NoError(t, err)
	s := string(b)
	assert.NotContains(t, s, `"Test":`)
	assert.NotContains(t, s, `"Output":`)
	assert.NotContains(t, s, `"Elapsed":`)
}

// TestJSONEmitter_LineDelimited — one JSON record per call, separated
// by newline (json.Encoder appends \n). Each line round-trips through
// json.Unmarshal back into a JSONEvent.
func TestJSONEmitter_LineDelimited(t *testing.T) {
	var buf bytes.Buffer
	em := newJSONEmitter(&buf)
	em.emit("start", "users_test.star", "test_x", "", 0)
	em.emit("pass", "users_test.star", "test_x", "", 0.04)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	var first JSONEvent
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.Equal(t, "start", first.Action)
	var second JSONEvent
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &second))
	assert.Equal(t, "pass", second.Action)
	assert.InDelta(t, 0.04, second.Elapsed, 1e-9)
}

// TestJSONEmitter_TimeIsUTC — emit time stamps in UTC regardless of
// process locale (Open Q6). A naive time.Now() would carry the local
// zone and break replay-determinism for tests captured across hosts.
func TestJSONEmitter_TimeIsUTC(t *testing.T) {
	var buf bytes.Buffer
	em := newJSONEmitter(&buf)
	em.emit("start", "users_test.star", "test_x", "", 0)
	var ev JSONEvent
	require.NoError(t, json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &ev))
	zone, _ := ev.Time.Zone()
	assert.Equal(t, "UTC", zone)
}

// TestFormatHumanLine_PassFailSkip — D5-E1 verbatim line shape:
//
//	"--- PASS: test_x (0.04s)\n"
//	"--- FAIL: test_y (0.13s)\n"
//	"--- SKIP: test_z (0.00s)\n"
func TestFormatHumanLine_PassFailSkip(t *testing.T) {
	assert.Equal(t, "--- PASS: test_x (0.04s)\n", formatHumanLine("PASS", "test_x", 0.04))
	assert.Equal(t, "--- FAIL: test_y (0.13s)\n", formatHumanLine("FAIL", "test_y", 0.13))
	assert.Equal(t, "--- SKIP: test_z (0.00s)\n", formatHumanLine("SKIP", "test_z", 0))
}
