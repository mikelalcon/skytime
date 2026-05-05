//go:build !windows

// Subprocess E2E for `skytime test` (CLI-03 success-criteria #5
// verbatim). Mirrors tests/e2e_skytime_run_test.go's ensureBinary
// pattern. Build tag: this test does not need a Temporal dev-server
// (Tier-3 harness uses TestWorkflowEnvironment in-process), but we
// keep the !windows guard for parity with sibling e2e tests in this
// directory.

package firewall_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testCmdBinOnce sync.Once
	testCmdBin     string
	testCmdBinErr  error
)

// ensureTestCmdBinary builds /tmp/.../skytime once per process for
// the `skytime test` E2E. Separate from the run-e2e binary so a
// parallel test invocation doesn't fight over the same /tmp dir.
func ensureTestCmdBinary(t *testing.T) string {
	t.Helper()
	testCmdBinOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "skytime-testcmd-e2e-*")
		if err != nil {
			testCmdBinErr = err
			return
		}
		bin := filepath.Join(tmp, "skytime")
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/skytime")
		cmd.Dir = findModuleRootE2E(t) // helper from e2e_skytime_run_test.go
		out, err := cmd.CombinedOutput()
		if err != nil {
			testCmdBinErr = fmt.Errorf("go build skytime (test e2e): %w: %s", err, string(out))
			return
		}
		testCmdBin = bin
	})
	require.NoError(t, testCmdBinErr)
	return testCmdBin
}

// helperWriteTestE2E writes one *_test.star file under dir.
func helperWriteTestE2E(t *testing.T, dir, name, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644))
}

// TestSkytimeTestE2E_HappyPath — VALIDATION.md per-task map cite
// (CLI-03 success-criteria #5).
func TestSkytimeTestE2E_HappyPath(t *testing.T) {
	bin := ensureTestCmdBinary(t)
	dir := t.TempDir()
	helperWriteTestE2E(t, dir, "users_test.star",
		"def test_existing_user():\n    assert.eq(1, 1)\n\n"+
			"def test_default_user():\n    assert.eq(\"x\", \"x\")\n")

	cmd := exec.Command(bin, "test", dir)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	require.NoError(t, err, "expected exit 0, got err=%v\nstdout=%s\nstderr=%s", err, out.String(), errOut.String())
	stdout := out.String()
	assert.Contains(t, stdout, "--- PASS: test_existing_user")
	assert.Contains(t, stdout, "--- PASS: test_default_user")
}

// TestSkytimeTestE2E_FailureExitNonzero — VALIDATION.md per-task map
// cite (CLI-03 D5-E4 exit code mapping; CLI-03 explicit "no Go stack
// traces in default output").
func TestSkytimeTestE2E_FailureExitNonzero(t *testing.T) {
	bin := ensureTestCmdBinary(t)
	dir := t.TempDir()
	helperWriteTestE2E(t, dir, "fail_test.star",
		"def test_failing():\n    assert.eq(\"octocat\", \"default-user\")\n")

	cmd := exec.Command(bin, "test", dir)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	require.Error(t, err, "expected exit 1, got nil\nstdout=%s\nstderr=%s", out.String(), errOut.String())
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok)
	assert.Equal(t, 1, exitErr.ExitCode())

	combined := out.String() + errOut.String()
	assert.Contains(t, combined, "--- FAIL: test_failing")

	// CLI-03 explicit: NO Go stack traces in default output.
	assert.NotContains(t, combined, "goroutine ")
	assert.NotContains(t, combined, "runtime.")
	assert.NotContains(t, combined, ".go:")
}

// TestSkytimeTestE2E_JSONFormat — VALIDATION.md per-task map cite
// (CLI-03 D5-E2 cmd/test2json mirror).
func TestSkytimeTestE2E_JSONFormat(t *testing.T) {
	bin := ensureTestCmdBinary(t)
	dir := t.TempDir()
	helperWriteTestE2E(t, dir, "users_test.star",
		"def test_existing_user():\n    assert.eq(1, 1)\n")

	cmd := exec.Command(bin, "test", "--format=json", dir)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	require.NoError(t, err, "stderr=%s", errOut.String())

	type jsonEvent struct {
		Action  string  `json:"Action"`
		Package string  `json:"Package"`
		Test    string  `json:"Test,omitempty"`
		Elapsed float64 `json:"Elapsed,omitempty"`
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	require.NotEmpty(t, lines)
	var actions []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		var ev jsonEvent
		require.NoError(t, json.Unmarshal([]byte(line), &ev), "bad JSON line: %q", line)
		assert.Equal(t, "users_test.star", ev.Package)
		actions = append(actions, ev.Action)
	}
	assert.Contains(t, actions, "start")
	assert.Contains(t, actions, "run")
	assert.Contains(t, actions, "pass")
}
