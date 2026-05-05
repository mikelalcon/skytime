package testing

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderOneFileWithRecorder drives renderOneFile against a recording
// shim so format tests observe rendered output without propagating
// inner-test failures to the parent *testing.T (Go's t.Run
// propagates inner failures to the parent).
//
// Returns the bytes written to cfg.formatOut and a slice of
// recorders, one per discovered def test_*() that was executed
// (filtered tests are not represented).
func renderOneFileWithRecorder(t *testing.T, file string, cfg *runConfig) []*recordingT {
	t.Helper()
	var recorders []*recordingT
	drive := func(name string, inner func(testReporter) (failed, skipped bool, detail string)) (bool, bool, string) {
		rec := &recordingT{}
		inner(rec)
		recorders = append(recorders, rec)
		// rec.failed flips true on Error/Errorf; rec has no Skipped
		// equivalent (Plan 04 decision: discoverTestFiles + len==0
		// drives Skipf; per-test skip is not exercised by the
		// recording shim path).
		passed := !rec.failed
		return passed, false, rec.detail.String()
	}
	renderOneFile(t, file, cfg, drive)
	return recorders
}

// TestRun_HumanFormat_DefaultOutputContainsPassLine — D5-E1 default
// human format: per-test `--- PASS:` line + per-file footer.
func TestRun_HumanFormat_DefaultOutputContainsPassLine(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "x_test.star", "def test_a():\n    assert.eq(1,1)\n")

	files, err := DiscoverTestFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	var buf bytes.Buffer
	cfg := &runConfig{formatOut: &buf}
	recs := renderOneFileWithRecorder(t, files[0], cfg)
	require.Len(t, recs, 1)
	assert.False(t, recs[0].failed, "test_a passes assert.eq(1,1)")

	out := buf.String()
	assert.Contains(t, out, "--- PASS: test_a")
	// Per-file footer references the basename.
	assert.Contains(t, out, filepath.Base(files[0]))
	assert.Contains(t, out, "PASS  ")
}

// TestRun_HumanFormat_FinalSummary — Run emits a final all-files
// summary line ("PASS  N files  (...)") after all per-file footers.
func TestRun_HumanFormat_FinalSummary(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "x_test.star", "def test_a():\n    assert.eq(1,1)\n")

	var buf bytes.Buffer
	t.Run("inner", func(subT *testing.T) {
		Run(subT, dir, WithOutput(&buf))
	})
	out := buf.String()
	assert.Contains(t, out, "--- PASS: test_a")
	assert.Contains(t, out, "PASS  1 files")
}

// TestSkytimeTestE2E_JSONFormat — VALIDATION.md per-task map cite
// (CLI-03). Plan 06's tests/skytime_test_e2e_test.go drives the same
// output path through the subprocess; this test pins the in-process
// shape.
//
// Expected sequence for one passing test:
//
//	start → run → pass
//
// Each line unmarshals to a JSONEvent; package = file basename;
// test = def name; pass record carries Elapsed.
func TestSkytimeTestE2E_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "users_test.star", "def test_x():\n    assert.eq(1,1)\n")

	files, err := DiscoverTestFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	var buf bytes.Buffer
	cfg := &runConfig{formatJSON: true, formatOut: &buf}
	renderOneFileWithRecorder(t, files[0], cfg)
	require.NotEmpty(t, buf.String(), "WithFormat(\"json\") must emit at least one record")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.NotEmpty(t, lines)

	var actions []string
	for _, line := range lines {
		var ev JSONEvent
		require.NoError(t, json.Unmarshal([]byte(line), &ev), "bad JSON line %q", line)
		actions = append(actions, ev.Action)
		assert.Equal(t, "users_test.star", ev.Package)
	}
	// Expect at minimum: start → run → pass for the single test.
	assert.Contains(t, actions, "start")
	assert.Contains(t, actions, "run")
	assert.Contains(t, actions, "pass")

	// First record action MUST be "start" (cmd/test2json convention).
	var first JSONEvent
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.Equal(t, "start", first.Action)

	// Last record MUST be a terminal action (pass / fail / skip).
	var last JSONEvent
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &last))
	assert.Contains(t, []string{"pass", "fail", "skip"}, last.Action)
}

// TestSkytimeTestE2E_JSONFormat_Failing — failing test produces a
// `fail` terminal record; the failure detail surfaces as an `output`
// record (cmd/test2json convention), and Go stack frames are NOT
// present in any record's Output (CLI-03).
func TestSkytimeTestE2E_JSONFormat_Failing(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "fail_test.star",
		"def test_failing():\n    assert.eq(\"a\", \"b\")\n")

	files, err := DiscoverTestFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	var buf bytes.Buffer
	cfg := &runConfig{formatJSON: true, formatOut: &buf}
	recs := renderOneFileWithRecorder(t, files[0], cfg)
	require.Len(t, recs, 1)
	assert.True(t, recs[0].failed, "test_failing must have triggered a Reporter failure")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.NotEmpty(t, lines)

	sawFail := false
	for _, line := range lines {
		var ev JSONEvent
		require.NoError(t, json.Unmarshal([]byte(line), &ev), "bad JSON line %q", line)
		if ev.Action == "fail" {
			sawFail = true
		}
		// CLI-03: no Go stack frames in any record's Output payload.
		assert.NotContains(t, ev.Output, "goroutine ")
		assert.NotContains(t, ev.Output, "runtime.")
	}
	assert.True(t, sawFail, "must emit at least one fail record for a failing test")
}

// TestTestCommand_DefaultOutput_NoGoStackTraces — VALIDATION.md
// per-task map cite (CLI-03 explicit). A failing test in default
// human format must NOT include Go runtime stack frames.
func TestTestCommand_DefaultOutput_NoGoStackTraces(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "fail_test.star",
		"def test_failing():\n    assert.eq(\"a\", \"b\")\n")

	files, err := DiscoverTestFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	var buf bytes.Buffer
	cfg := &runConfig{formatOut: &buf}
	recs := renderOneFileWithRecorder(t, files[0], cfg)
	require.Len(t, recs, 1)
	assert.True(t, recs[0].failed, "test_failing must have triggered a Reporter failure")

	out := buf.String()
	assert.Contains(t, out, "--- FAIL: test_failing", "expected the per-test FAIL line")

	// CLI-03: NO Go runtime stack frames in default output.
	assert.NotContains(t, out, "goroutine ",
		"Go runtime stack frames must not appear in default output (CLI-03)")
	assert.NotContains(t, out, "runtime.",
		"Go runtime functions must not appear in default output (CLI-03)")
	// Detect Go-source-line pointers like "foo.go:42"; .star pointers
	// are allowed (they're the *Starlark* callsite consultants need).
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, ".go:") {
			continue
		}
		t.Fatalf("default output should not include .go: source pointers (CLI-03); got line: %q", line)
	}

	// The Starlark callsite SHOULD be present — at minimum the
	// fixture's filename surfaces somewhere in the failure detail.
	assert.Contains(t, out, "fail_test.star",
		"Starlark callsite (the .star file) must appear in failure detail")
}

// TestWithFormat_UnknownReturnsErrorAtOptionTime — defensive: typos
// like WithFormat("jsno") fail loudly at option-apply time rather
// than silently falling back to human.
func TestWithFormat_UnknownReturnsErrorAtOptionTime(t *testing.T) {
	cfg := &runConfig{}
	err := WithFormat("jsno")(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown format")
}

// TestWithFormat_AcceptsHumanAndJSON — both spellings of the human
// format ("" and "human") must succeed without error.
func TestWithFormat_AcceptsHumanAndJSON(t *testing.T) {
	cases := []struct {
		format string
		isJSON bool
	}{
		{"", false},
		{"human", false},
		{"json", true},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(subT *testing.T) {
			cfg := &runConfig{}
			err := WithFormat(tc.format)(cfg)
			require.NoError(subT, err)
			assert.Equal(subT, tc.isJSON, cfg.formatJSON)
		})
	}
}
