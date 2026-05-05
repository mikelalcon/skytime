package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helperRunTestCmd builds a fresh root with no extensions and invokes
// `skytime test <args>` against it, returning captured stdout, stderr,
// and the cobra error.
func helperRunTestCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root, rootErr := NewRootCommand()
	require.NoError(t, rootErr)
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	fullArgs := append([]string{"test"}, args...)
	root.SetArgs(fullArgs)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func writeFileTestCmd(t *testing.T, dir, name, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644))
}

func TestNewTestCommand_UseString(t *testing.T) {
	cfg := &config{}
	cmd := newTestCommand(cfg)
	assert.Equal(t, "test <dir>", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("run"))
	assert.NotNil(t, cmd.Flags().Lookup("format"))
	assert.Equal(t, "", cmd.Flags().Lookup("run").DefValue)
	assert.Equal(t, "human", cmd.Flags().Lookup("format").DefValue)
}

func TestTestCommand_HappyPath_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	writeFileTestCmd(t, dir, "x_test.star", "def test_pass():\n    assert.eq(1,1)\n")
	out, _, err := helperRunTestCmd(t, dir)
	require.NoError(t, err)
	assert.Contains(t, out, "--- PASS: test_pass")
}

func TestTestCommand_FailExitsViaErrSilent(t *testing.T) {
	dir := t.TempDir()
	writeFileTestCmd(t, dir, "fail_test.star", "def test_fail():\n    assert.eq(\"a\",\"b\")\n")
	_, _, err := helperRunTestCmd(t, dir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errSilent), "expected errSilent (cobra exit 1), got %v", err)
}

// TestTestCommand_RunFilter — VALIDATION.md per-task map cite (CLI-03).
func TestTestCommand_RunFilter(t *testing.T) {
	dir := t.TempDir()
	writeFileTestCmd(t, dir, "users_test.star",
		"def test_existing_user():\n    assert.eq(1,1)\n\n"+
			"def test_other():\n    assert.eq(1,1)\n")
	writeFileTestCmd(t, dir, "orders_test.star", "def test_o():\n    assert.eq(1,1)\n")

	out, _, err := helperRunTestCmd(t, "--run", `^users_test\.test_existing`, dir)
	require.NoError(t, err)
	assert.Contains(t, out, "--- PASS: test_existing_user")
	assert.NotContains(t, out, "test_other")
	assert.NotContains(t, out, "test_o")
}

// TestTestCommand_DefaultOutput_NoGoStackTraces — VALIDATION.md
// per-task map cite (CLI-03 explicit "no Go stack traces in default
// output"). Verified at the pkg/cli surface, NOT just at pkg/testing.
func TestTestCommand_DefaultOutput_NoGoStackTraces(t *testing.T) {
	dir := t.TempDir()
	writeFileTestCmd(t, dir, "fail_test.star", "def test_failing():\n    assert.eq(\"a\",\"b\")\n")
	out, errOut, err := helperRunTestCmd(t, dir)
	require.Error(t, err) // failed test → errSilent
	combined := out + errOut
	assert.NotContains(t, combined, "goroutine ")
	assert.NotContains(t, combined, "runtime.")
	assert.NotContains(t, combined, ".go:")
	assert.Contains(t, combined, "fail_test.star")
}

// TestTestCommand_BadFlagFormat_Error — RunCLI surfaces an option-time
// error from WithFormat for unknown formats.
func TestTestCommand_BadFlagFormat_Error(t *testing.T) {
	dir := t.TempDir()
	writeFileTestCmd(t, dir, "x_test.star", "def test_x():\n    pass\n")
	_, errOut, err := helperRunTestCmd(t, "--format", "xml", dir)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(errOut), "format")
}
