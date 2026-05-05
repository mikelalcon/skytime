package testing

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFileCLI writes a file inside dir for cli_run tests. Lives here
// instead of importing from a sibling test file to avoid coupling tests.
func writeFileCLI(t *testing.T, dir, name, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644))
}

func TestRunCLI_HappyPath_ReturnsPassedOne(t *testing.T) {
	dir := t.TempDir()
	writeFileCLI(t, dir, "x_test.star", "def test_pass():\n    assert.eq(1, 1)\n")
	var buf bytes.Buffer
	passed, failed, err := RunCLI(dir, WithOutput(&buf))
	require.NoError(t, err)
	assert.Equal(t, 1, passed)
	assert.Equal(t, 0, failed)
	assert.Contains(t, buf.String(), "--- PASS: test_pass")
}

func TestRunCLI_FailPath_ReturnsFailedOne(t *testing.T) {
	dir := t.TempDir()
	writeFileCLI(t, dir, "fail_test.star", "def test_fail():\n    assert.eq(\"a\", \"b\")\n")
	var buf bytes.Buffer
	passed, failed, err := RunCLI(dir, WithOutput(&buf))
	require.NoError(t, err)
	assert.Equal(t, 0, passed)
	assert.Equal(t, 1, failed)
	out := buf.String()
	assert.Contains(t, out, "--- FAIL: test_fail")
	assert.Contains(t, out, "fail_test.star")
}

func TestRunCLI_Mixed_ReturnsBothCounts(t *testing.T) {
	dir := t.TempDir()
	writeFileCLI(t, dir, "x_test.star",
		"def test_pass():\n    assert.eq(1,1)\n\n"+
			"def test_fail():\n    assert.eq(1,2)\n")
	var buf bytes.Buffer
	passed, failed, err := RunCLI(dir, WithOutput(&buf))
	require.NoError(t, err)
	assert.Equal(t, 1, passed)
	assert.Equal(t, 1, failed)
}

func TestRunCLI_JSONFormat_LineDelimitedRecords(t *testing.T) {
	dir := t.TempDir()
	writeFileCLI(t, dir, "x_test.star", "def test_x():\n    assert.eq(1, 1)\n")
	var buf bytes.Buffer
	_, _, err := RunCLI(dir, WithFormat("json"), WithOutput(&buf))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.NotEmpty(t, lines)
	var actions []string
	for _, line := range lines {
		var ev JSONEvent
		require.NoError(t, json.Unmarshal([]byte(line), &ev))
		actions = append(actions, ev.Action)
		assert.Equal(t, "x_test.star", ev.Package)
	}
	assert.Contains(t, actions, "start")
	assert.Contains(t, actions, "run")
	assert.Contains(t, actions, "pass")
}

// TestRunCLI_NoGoStackTracesInFailureOutput pins CLI-03's explicit
// "no Go stack traces in default output" requirement at the RunCLI
// layer.
func TestRunCLI_NoGoStackTracesInFailureOutput(t *testing.T) {
	dir := t.TempDir()
	writeFileCLI(t, dir, "fail_test.star", "def test_failing():\n    assert.eq(\"a\", \"b\")\n")
	var buf bytes.Buffer
	_, failed, err := RunCLI(dir, WithOutput(&buf))
	require.NoError(t, err)
	require.Equal(t, 1, failed)
	out := buf.String()
	assert.NotContains(t, out, "goroutine ")
	assert.NotContains(t, out, "runtime.")
	assert.NotContains(t, out, ".go:")
	// Starlark callsite must be present.
	assert.Contains(t, out, "fail_test.star")
}

func TestRunCLI_BadRunFilter_ReturnsErrAtOptionTime(t *testing.T) {
	dir := t.TempDir()
	writeFileCLI(t, dir, "x_test.star", "def test_x():\n    pass\n")
	_, _, err := RunCLI(dir, WithRunFilter("[invalid"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBadFilter))
}

func TestRunCLI_RunFilter_ExcludesNonMatching(t *testing.T) {
	dir := t.TempDir()
	writeFileCLI(t, dir, "users_test.star",
		"def test_existing_user():\n    assert.eq(1,1)\n\n"+
			"def test_other():\n    assert.eq(1,1)\n")
	writeFileCLI(t, dir, "orders_test.star", "def test_o():\n    assert.eq(1,1)\n")
	var buf bytes.Buffer
	passed, failed, err := RunCLI(dir, WithRunFilter(`^users_test\.test_existing`), WithOutput(&buf))
	require.NoError(t, err)
	assert.Equal(t, 1, passed)
	assert.Equal(t, 0, failed)
	assert.Contains(t, buf.String(), "--- PASS: test_existing_user")
	assert.NotContains(t, buf.String(), "test_other")
	assert.NotContains(t, buf.String(), "test_o")
}

func TestRunCLI_NoTestFiles_ZeroCountsNoError(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	passed, failed, err := RunCLI(dir, WithOutput(&buf))
	require.NoError(t, err)
	assert.Equal(t, 0, passed)
	assert.Equal(t, 0, failed)
	assert.Contains(t, buf.String(), "no *_test.star files")
}
