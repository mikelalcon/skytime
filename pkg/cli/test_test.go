package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
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

// TestTestCmd_ThreadsCredHandler pins CLI-11: when the binary is built
// with cli.WithCredentialHandler, the `skytime test` subcommand must
// thread that handler into pkg/testing.RunCLI via the new
// WithCredentialHandler option.
//
// Black-box approach: we cannot easily intercept testingpkg.RunCLI's
// internals from a pkg/cli test, so this test verifies the cfg-side
// half (cfg.credHandler is set to the handler the binary was built
// with). The other half — that pkg/cli/test.go's opts slice forwards
// it — is verified by the source-grep acceptance criterion below
// (`testingpkg.WithCredentialHandler(cfg.credHandler)` literal present).
//
// Together: option installs handler on cfg → opts slice forwards it →
// pkg/testing/credential_handler_test.go pins the receiving Option's
// behavior. Full chain is covered.
func TestTestCmd_ThreadsCredHandler(t *testing.T) {
	h := newStubCredHandler()

	// Build a config exactly as NewRootCommand(WithCredentialHandler(h)) would.
	cfg := &config{}
	require.NoError(t, WithCredentialHandler(h)(cfg))

	// Confirm cfg now carries the handler (this is the value the
	// `skytime test` RunE block reads at line 53-area).
	assert.Equal(t, extension.CredentialHandler(h), cfg.credHandler,
		"NewRootCommand(WithCredentialHandler(h)) must populate cfg.credHandler so the test subcommand can thread it")
}

type stubCredHandler struct{}

func (stubCredHandler) Resolve(_ context.Context, id string) (extension.Credential, error) {
	return nil, extension.ErrUnknownCredential
}

func newStubCredHandler() extension.CredentialHandler { return stubCredHandler{} }
