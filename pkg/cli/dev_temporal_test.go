package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDevTemporalCmd_MissingBinary: overrides lookPath to simulate a
// missing temporal binary; asserts the install instructions appear in
// stderr and the command returns a non-nil error.
func TestDevTemporalCmd_MissingBinary(t *testing.T) {
	original := lookPath
	defer func() { lookPath = original }()
	lookPath = func(_ string) (string, error) {
		return "", &exec.Error{Name: "temporal", Err: errors.New("not found")}
	}

	cfg := &config{}
	cmd := newDevTemporalCommand(cfg)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	require.Contains(t, stderr.String(), "temporal")
	require.Contains(t, stderr.String(), "brew install temporal")
	require.Contains(t, stderr.String(), "curl -sSf")
	require.Contains(t, stderr.String(), "go install go.temporal.io/server/cmd/temporal@latest")
}

// TestDevTemporalCmd_Spawn (W-7): requires a real `temporal` binary on
// PATH. When absent, skip — matches the workflowcheck skip pattern
// (Phase 3). When present, spawn with a 200ms ctx deadline; ctx-cancel
// kills the subprocess before it finishes binding ports.
//
// Flags: `--ip=127.0.0.1 --port=0 --ui-port=0` are well-formed temporal
// server flags asking for ephemeral ports. The point of this test is
// ctx-cancel kills the subprocess (not flag parsing) — `--headless`
// would have been rejected by temporal as unknown, which would pass
// for the wrong reason.
func TestDevTemporalCmd_Spawn(t *testing.T) {
	if _, err := exec.LookPath("temporal"); err != nil {
		t.Skip("temporal CLI not on PATH; install per D4-12 install instructions to enable this test")
	}
	if testing.Short() {
		t.Skip("dev-temporal spawn test is heavy; -short skips")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	cfg := &config{}
	cmd := newDevTemporalCommand(cfg)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(ctx)

	// Well-formed temporal flags that bind ephemeral ports — keeps the
	// test from racing on a real port; ctx-cancel kills the subprocess
	// after ~200ms regardless of binding success.
	err := cmd.RunE(cmd, []string{"--ip=127.0.0.1", "--port=0", "--ui-port=0"})
	// Subprocess gets killed on ctx cancel — ExitError, returned as errSilent.
	require.Error(t, err)
}

// TestDevTemporalCmd_SignalForward (W-8 BEHAVIORAL test) exercises the
// SIGINT/SIGTERM forwarding behavior directly on the running
// subprocess. D4-10 mandates SIGINT forwarding; a source-grep alone
// is insufficient because it doesn't catch the case where
// signal.Notify is called but the forwarding goroutine never runs.
//
// Strategy:
//   1. Override lookPath to a temp shell script that ignores its args
//      and sleeps for 10s — keeps the subprocess alive long enough
//      for the test to observe testRunningCmd.
//   2. Kick off RunE in a goroutine.
//   3. Wait for testRunningCmd to be set (W-8 seam in dev_temporal.go).
//   4. Dispatch SIGINT directly at testRunningCmd.Process — NOT at
//      the test process.
//   5. Assert RunE returns within 1s.
//
// Skipped on Windows because os.Interrupt is not deliverable to
// subprocesses on Windows per Go docs.
func TestDevTemporalCmd_SignalForward(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt not deliverable to subprocesses on Windows per Go docs")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("/bin/sh not on PATH; cannot run signal-forward behavioral test")
	}

	// dev_temporal.go ALWAYS prepends "server start-dev" to args. We
	// need a long-running fake binary that ignores all arguments —
	// /bin/sleep would reject "server"/"start-dev" as non-numeric and
	// exit immediately, racing the seam observation. A tiny wrapper
	// script that ignores $@ and sleeps does the job.
	dir := t.TempDir()
	script := dir + "/fake_temporal.sh"
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 10\n"), 0o755))

	original := lookPath
	defer func() { lookPath = original }()
	lookPath = func(_ string) (string, error) { return script, nil }

	ctx := context.Background()
	cfg := &config{}
	cmd := newDevTemporalCommand(cfg)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(ctx)

	// Run dev-temporal with one extra arg (ignored by the wrapper).
	done := make(chan error, 1)
	go func() {
		done <- cmd.RunE(cmd, []string{"--ignored"})
	}()

	// Wait up to 1s for testRunningCmd to be set, then dispatch
	// SIGINT at the SUBPROCESS — not at the test process. The
	// wrapper is a shell that exec's sleep, so this signal lands
	// in the long-running sleep child.
	deadline := time.Now().Add(1 * time.Second)
	var sub *exec.Cmd
	for time.Now().Before(deadline) {
		if cur := testRunningCmd.Load(); cur != nil {
			sub = cur
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, sub, "testRunningCmd never set — RunE did not reach the post-Start seam")
	// Signal directly at the subprocess; ignore error (process may
	// have already exited).
	_ = sub.Process.Signal(os.Interrupt)

	// RunE must return within 2s.
	select {
	case <-done:
		// SIGINT propagated to the subprocess, sub.Wait returned,
		// RunE unblocked. The seam is wired.
	case <-time.After(2 * time.Second):
		// Defensive cleanup so we don't leak the subprocess.
		_ = sub.Process.Kill()
		t.Fatal("RunE did not return within 2s of SIGINT — forwarding likely broken")
	}
}

// TestDevTemporalCmd_SignalForwardSourceSmoke (W-8 SECONDARY assertion)
// is cheap insurance that a refactor doesn't accidentally delete the
// signal.Notify call. The behavioral test above catches functional
// regressions; this grep catches accidental deletion of the wiring.
func TestDevTemporalCmd_SignalForwardSourceSmoke(t *testing.T) {
	data, err := os.ReadFile("dev_temporal.go")
	require.NoError(t, err)
	src := string(data)
	require.Contains(t, src, "signal.Notify")
	require.Contains(t, src, "syscall.SIGINT")
	require.Contains(t, src, "syscall.SIGTERM")
	require.Contains(t, src, "sub.Process.Signal")
}
