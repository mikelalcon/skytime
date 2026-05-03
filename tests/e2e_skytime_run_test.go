//go:build !windows

// Note: subprocess teardown uses process-group kill (Setpgid +
// syscall.Kill(-pid, ...)) so the `temporal server start-dev`
// subprocess's children (UI, persistence, frontend gRPC) are reliably
// reaped on test exit, panic, or interrupt. This is Unix-specific
// (Setpgid does not exist on Windows). Windows users who want to run
// these e2e tests must use WSL or Docker, which they would already
// need to obtain `temporal` itself for start-dev mode. Hence the
// build tag rather than a runtime skip.

package firewall_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var (
	skytimeBinOnce sync.Once
	skytimeBin     string
	skytimeBinErr  error
	devServerCmd   *exec.Cmd
	devServerOnce  sync.Once
	devServerErr   error
)

// teardownDevServer is the centralized cleanup. Idempotent: safe to call
// from defer AND from a signal handler. Uses process-group kill via
// Setpgid (set in ensureDevServer) so the entire start-dev subtree is
// reaped, not just the parent shell wrapper.
func teardownDevServer() {
	if devServerCmd == nil || devServerCmd.Process == nil {
		return
	}
	pgid := devServerCmd.Process.Pid // because Setpgid → PGID == PID
	// Step 1: SIGTERM the whole process group.
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	// Step 2: wait up to 3 seconds for graceful exit.
	done := make(chan error, 1)
	go func() { done <- devServerCmd.Wait() }()
	select {
	case <-done:
		// graceful exit — done.
	case <-time.After(3 * time.Second):
		// Step 3: escalate to SIGKILL on the group.
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done // reap
	}
}

// TestMain sets up two package-wide responsibilities:
//
//  1. Signal handler → reliable subprocess group teardown on Ctrl-C /
//     SIGTERM mid-test (the dev-server process group lives outside the
//     normal defer chain since it's a long-lived background subprocess).
//  2. After-suite teardown of any spawned dev server.
//
// The dev server itself is spawned on demand by ensureDevServer (called
// only by tests that need it) — TestMain does NOT pre-spawn it so the
// non-e2e tests in this package (differential_test.go,
// firewall_cli_test.go) are NOT impacted. Per-test t.Skip() inside
// ensureDevServer handles the temporal-CLI-missing case.
func TestMain(m *testing.M) {
	// Wire signal handler BEFORE m.Run so a Ctrl-C mid-test still tears
	// down the dev server. The handler calls teardownDevServer
	// (idempotent with the defer below) and exits with the conventional
	// 128+signal code. Setpgid + group-kill is the reusable pattern for
	// any future test that spawns a long-running CLI subprocess.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "skytime e2e: caught %s, tearing down dev server\n", sig)
		teardownDevServer()
		n := 0
		switch sig {
		case syscall.SIGINT:
			n = 2
		case syscall.SIGTERM:
			n = 15
		}
		os.Exit(128 + n)
	}()

	code := m.Run()
	teardownDevServer()
	os.Exit(code)
}

// ensureBinary builds /tmp/<unique>/skytime once per process.
func ensureBinary(t *testing.T) string {
	t.Helper()
	skytimeBinOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "skytime-e2e-bin-*")
		if err != nil {
			skytimeBinErr = err
			return
		}
		bin := filepath.Join(tmp, "skytime")
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/skytime")
		cmd.Dir = findModuleRootE2E(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			skytimeBinErr = fmt.Errorf("go build skytime: %w: %s", err, string(out))
			return
		}
		skytimeBin = bin
	})
	require.NoError(t, skytimeBinErr)
	return skytimeBin
}

// ensureDevServer launches temporal server start-dev once in its own
// process group (Setpgid) so teardown can SIGTERM/SIGKILL the whole
// subtree (start-dev spawns UI + persistence children). Polls the
// default gRPC port (7233) for readiness with a 30s timeout. Skips the
// calling test if the temporal CLI is not on PATH (this is the documented
// dev workstation prerequisite — CI without it just t.Skip()s).
//
// If a dev server is already listening on 127.0.0.1:7233 (e.g., a
// developer left one running), the test reuses it without spawning a
// new one — devServerCmd stays nil so teardown is a no-op. This avoids
// port-collision failures and respects developer-controlled lifecycles.
func ensureDevServer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("temporal"); err != nil {
		t.Skipf("temporal CLI not in PATH; skipping e2e: %v", err)
	}
	devServerOnce.Do(func() {
		// Probe for an existing server first.
		probe := exec.Command("temporal", "operator", "namespace", "describe", "default", "--address", "127.0.0.1:7233")
		if err := probe.Run(); err == nil {
			// A dev server is already listening — reuse it.
			return
		}

		devServerCmd = exec.Command(
			"temporal", "server", "start-dev",
			"--ui-port", "0", // disable UI to avoid port collision
		)
		// Critical: Setpgid → child runs in its own process group
		// (PGID == its PID). teardownDevServer relies on this to
		// signal the WHOLE group via syscall.Kill(-pid, ...).
		devServerCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if err := devServerCmd.Start(); err != nil {
			devServerErr = fmt.Errorf("start temporal server: %w", err)
			return
		}
		// Poll 127.0.0.1:7233 readiness via the temporal CLI's
		// namespace describe (fastest cheap check).
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			check := exec.Command("temporal", "operator", "namespace", "describe", "default", "--address", "127.0.0.1:7233")
			if err := check.Run(); err == nil {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		devServerErr = errors.New("temporal server start-dev did not become ready within 30s")
	})
	require.NoError(t, devServerErr)
}

// requireNetwork t.Skip()s when api.github.com is unreachable.
func requireNetwork(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/zen", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Skipf("api.github.com returned %d on /zen pre-flight", resp.StatusCode)
	}
}

// findModuleRootE2E walks up to find go.mod.
func findModuleRootE2E(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod walking up from %s", cwd)
		}
		dir = parent
	}
}

// TestE2E_SkytimeRun_Happy: full stack — binary build → dev-server →
// skytime run examples/skeleton/simple_check.star → assert status=200
// + flow complete + no ✗ + correct ordering.
func TestE2E_SkytimeRun_Happy(t *testing.T) {
	requireNetwork(t)
	ensureDevServer(t)
	bin := ensureBinary(t)
	root := findModuleRootE2E(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin,
		"run",
		filepath.Join(root, "examples", "skeleton", "simple_check.star"),
		"--flow", "simple_check",
		// Phase 04.1 (D4.1-23): simple_check.star inputs={"repo": "string"},
		// path is built via ${ctx.repo} interpolation. The pre-04.1 corpus
		// declared "repo_path"; it was renamed when the demo was rewritten
		// to actually use --input.
		"--input", `{"repo":"octocat/Hello-World"}`,
		"--address", "127.0.0.1:7233",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	t.Logf("stdout: %s", stdout.String())
	t.Logf("stderr: %s", stderr.String())

	require.NoError(t, err, "skytime run failed: %s", stderr.String())

	out := stderr.String()
	require.Contains(t, out, "status=200",
		"Fix B summary missing — expected status=200")
	require.Contains(t, out, "[skytime] flow complete",
		"Fix C complete line missing")
	require.NotContains(t, out, "✗",
		"happy path must not render ✗ failure marker")
	require.NotContains(t, out, "flow failed",
		"happy path must not render flow failed")

	// M-5 (checker iteration 1): ordering guard. The status=200 summary
	// MUST appear in the stream BEFORE the flow_complete line.
	// require.Contains alone is order-insensitive — without this guard
	// a partial-success regression where flow_complete emits before any
	// step_complete could pass silently.
	statusIdx := strings.Index(out, "status=200")
	completeIdx := strings.Index(out, "[skytime] flow complete")
	require.True(t,
		statusIdx >= 0 && completeIdx >= 0 && statusIdx < completeIdx,
		"status=200 must appear before flow complete (got status=%d, complete=%d)",
		statusIdx, completeIdx)
}

// TestE2E_SkytimeRun_Unhappy: build a temp .star file pointing at a
// definitely-404 endpoint; assert ✗ + HTTP 404 + flow failed; exit
// non-zero via *exec.ExitError (NOT context cancellation).
func TestE2E_SkytimeRun_Unhappy(t *testing.T) {
	requireNetwork(t)
	ensureDevServer(t)
	bin := ensureBinary(t)

	starContent := `gh = http.endpoint(base_url = "https://api.github.com")
flow(name = "bad", inputs = {}, steps = [
    step(action = gh.get(path = "/repos/totally/does-not-exist-xyzzy12345")),
])
`
	starFile := filepath.Join(t.TempDir(), "bad.star")
	require.NoError(t, os.WriteFile(starFile, []byte(starContent), 0o644))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin,
		"run", starFile,
		"--flow", "bad",
		"--input", "{}",
		"--address", "127.0.0.1:7233",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	t.Logf("stdout: %s", stdout.String())
	t.Logf("stderr: %s", stderr.String())

	// M-4 (checker iteration 1): plain require.Error doesn't distinguish
	// a true non-zero exit from a context-cancellation timeout (which
	// would surface as a context.DeadlineExceeded-wrapped error type).
	// Pin to *exec.ExitError to confirm the process actually ran to
	// completion AND exited non-zero.
	require.Error(t, err, "skytime run on a 404-pointing flow must exit non-zero")
	require.IsType(t, &exec.ExitError{}, err,
		"expected *exec.ExitError (real non-zero exit), got %T: %v — likely a context-cancellation timeout, dev-server hang, or binary crash",
		err, err)

	// Defensive: confirm the exit code is a real number, not -1
	// (which exec sets when the process was killed by signal).
	require.NotNil(t, cmd.ProcessState, "ProcessState must be populated after Run()")
	require.NotEqual(t, -1, cmd.ProcessState.ExitCode(),
		"exit code -1 means killed-by-signal; want a true non-zero exit from skytime run")

	out := stderr.String()
	require.Contains(t, out, "✗",
		"unhappy path must render ✗ failure marker (Fix C)")
	require.Contains(t, out, "HTTP 404",
		"unhappy path must surface HTTP 404 in error message (Fix A)")
	require.Contains(t, out, "[skytime] flow failed",
		"unhappy path must render flow failed line (Fix C)")
	// Defense in depth: must not also print success line.
	require.NotContains(t, out, "[skytime] flow complete")
}
