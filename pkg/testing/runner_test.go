package testing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFixture writes contents to dir/name and returns the file path.
func writeFixture(t *testing.T, dir, name, contents string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(contents), 0o644))
	return p
}

// TestRunner_DiscoversAndRunsSingleFile — Run drives a single
// *_test.star file end-to-end. test_pass holds; the inner subtest
// passes (and so does the outer harness).
func TestRunner_DiscoversAndRunsSingleFile(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "simple_test.star", "def test_pass():\n    assert.eq(1, 1)\n")
	Run(t, dir)
	// If we reach here, the runner walked the file and the test passed.
	// (A failing test inside Run would have failed t via the subtest.)
}

// TestRunner_AssertFailureMakesSubtestFail — a failing assert.eq in
// a discovered def test_*() produces a Reporter.Error call. We
// observe via a recordingT shim by replicating Run's parse+walk
// inline, so that a real t.Run failure does not poison this very
// test (Go's t.Run propagates failure to the parent T).
func TestRunner_AssertFailureMakesSubtestFail(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "fail_test.star", "def test_failing():\n    assert.eq(\"a\", \"b\")\n")

	// We run the same parse path as Run (single-file directory walk)
	// but invoke runOneTest under a recordingT shim.
	files, err := discoverTestFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	rep := &recordingT{}
	hits := walkAndDriveTests(t, files[0], rep)
	require.GreaterOrEqual(t, hits, 1, "must have discovered ≥1 def test_*() in fail_test.star")
	assert.True(t, rep.failed, "fail_test.test_failing should have triggered Reporter failure")
}

// TestRunner_NoFiles_Skips — discovery on an empty directory returns
// no files, which is what triggers the t.Skipf path inside Run. We
// assert on the discovery primitive directly since Skipf calls
// runtime.Goexit and can't be observed cleanly from the calling
// goroutine.
func TestRunner_NoFiles_Skips(t *testing.T) {
	dir := t.TempDir()
	files, err := discoverTestFiles(dir)
	require.NoError(t, err)
	assert.Empty(t, files,
		"discoverTestFiles on empty dir must return no entries; "+
			"runner.Run uses this to drive the t.Skipf path")
}

// walkAndDriveTests parses a *_test.star file and invokes every
// discovered def test_*() under the supplied testReporter. Returns
// the number of tests invoked.
//
// Test-only helper — duplicates the parsedTestFile walk performed by
// runOneFile, but invokes runOneTest with a recording shim instead
// of *testing.T so failure observation does not poison the outer
// test (Go's t.Run propagates inner failures to the parent T).
func walkAndDriveTests(t *testing.T, file string, rep testReporter) int {
	t.Helper()
	tests, err := parseTestFile(file, &runConfig{})
	require.NoError(t, err)
	hits := 0
	for _, tc := range tests.Tests {
		runOneTest(rep, tc.Fn, tests.Reg, tests.WS)
		hits++
	}
	return hits
}

// TestTestCommand_RunFilter — VALIDATION.md per-task map cite (CLI-03).
//
// Plan 05 owns the regex filter at the pkg/testing layer; Plan 06 will
// add a thin pkg/cli wrapper test that exposes the same behavior
// through the cobra `--run` flag.
//
// We can't directly count which subtests t.Run actually executed
// from outside the runner (testing.T doesn't expose that). Instead
// we use walkAndDriveTests + a recording reporter, applying the
// filter inline against `<file_basename>.<test_name>` exactly as the
// runner does. This pins the regex semantics that Run is contracted
// to honor.
func TestTestCommand_RunFilter(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "users_test.star",
		"def test_existing_user():\n    assert.eq(1,1)\ndef test_other():\n    assert.eq(1,1)\n")
	writeFixture(t, dir, "orders_test.star",
		"def test_o():\n    assert.eq(1,1)\n")

	// 1. Empty filter (match all): every discovered test in both
	//    files runs. There are 3 def test_*() functions across the
	//    two fixtures (test_existing_user, test_other, test_o) so
	//    walkAndDriveTests returns 3 invocations total.
	files, err := DiscoverTestFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 2)

	allRep := &recordingT{}
	allHits := 0
	for _, f := range files {
		allHits += walkAndDriveTests(t, f, allRep)
	}
	assert.Equal(t, 3, allHits, "no filter must run all 3 def test_*() across both files")
	assert.False(t, allRep.failed, "all assertions are eq(1,1); the recording reporter should not have failed")

	// 2. Filter active: regex `^users_test\.test_existing` matches
	//    only users_test.test_existing_user. Apply the filter
	//    inline — exactly the rule MatchRunFilter applies.
	re, err := CompileRunFilter(`^users_test\.test_existing`)
	require.NoError(t, err)

	var matched []string
	for _, f := range files {
		tests, perr := parseTestFile(f, &runConfig{})
		require.NoError(t, perr)
		fileStem := stemOf(f)
		for _, tc := range tests.Tests {
			full := fileStem + "." + tc.Name
			if MatchRunFilter(re, full) {
				matched = append(matched, full)
			}
		}
	}
	require.Len(t, matched, 1, "filter must select exactly 1 test")
	assert.Equal(t, "users_test.test_existing_user", matched[0])
}

// stemOf returns the basename without the .star suffix
// ("users_test.star" → "users_test"). Mirrors what runOneFile does
// internally; lifted here so the filter test can match the runner's
// canonical key shape without poking unexported helpers.
func stemOf(file string) string {
	base := filepath.Base(file)
	if len(base) > len(".star") && base[len(base)-len(".star"):] == ".star" {
		return base[:len(base)-len(".star")]
	}
	return base
}
