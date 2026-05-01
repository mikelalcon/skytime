//go:build integration

// Package worker library-embed integration test (WORK-03).
//
// CI integration:
//
//	.github/workflows/ci.yml should include a job that:
//	  1. brew install temporal (or download the binary)
//	  2. temporal server start-dev --headless &
//	  3. wait for localhost:7233
//	  4. go test -tags=integration ./pkg/worker/... -count=1
//
// This is OPTIONAL for v1 — the testsuite-based tests in pkg/interpreter
// cover the workflow logic; this test verifies the real-world embed pattern
// against a live Temporal server.
package worker_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/dag"
	extensiontesting "github.com/mikelalcon/skytime/pkg/extension/testing"
	"github.com/mikelalcon/skytime/pkg/worker"
)

const (
	devAddr   = "localhost:7233"
	namespace = "default"
)

// trivialStarSrc is a minimal flow that uses no extensions — just one inline
// script. Keeps the integration test self-contained: no need for a fake
// extension, no parser-level dependencies beyond the always-available
// `script` builtin.
const trivialStarSrc = `flow(
    name="trivial",
    steps=[
        script(id="bump", fn=lambda ctx: {"x_plus_one": ctx.x + 1}, output_alias="bumped"),
    ],
)
`

// TestEmbed_FullStack is the WORK-03 verification: a library consumer can
// import skytime, call NewDevClient + NewWorker, register the workflow with
// the SDK worker, and execute an end-to-end flow against a real Temporal dev
// server. Skips cleanly if the dev server is not reachable.
//
// Setup:
//
//	$ temporal server start-dev
//	$ go test -tags=integration ./pkg/worker/...
//
// CI: typically runs without a real dev server; this test skips and the
// testsuite-based tests in pkg/interpreter cover the codepath. CI runners
// that want full coverage start a dev server in the workflow.
func TestEmbed_FullStack(t *testing.T) {
	if !devServerReachable() {
		t.Skip("dev server not reachable at " + devAddr +
			"; install + start: brew install temporal && temporal server start-dev")
	}

	// 1. tmpdir fixture with one trivial .star file.
	dir := t.TempDir()
	starPath := filepath.Join(dir, "trivial.star")
	require.NoError(t, os.WriteFile(starPath, []byte(trivialStarSrc), 0644))

	// 2. CredentialHandler — required by WorkerOptions even though trivial.star
	// uses no credentials. Empty Creds map → all Resolve calls fail with
	// ErrUnknownCredential, which never fires for this script-only flow.
	handler := &extensiontesting.FakeCredentialHandler{}

	// 3. Build dev client + worker.
	c, err := worker.NewDevClient(worker.DevClientOptions{
		HostPort:  devAddr,
		Namespace: namespace,
	})
	require.NoError(t, err)
	defer c.Close()

	w, err := worker.NewWorker(c, worker.WorkerOptions{
		RootDir:           dir,
		BuildID:           "embed-test-" + t.Name(),
		TaskQueue:         "skytime-embed-test",
		CredentialHandler: handler,
	})
	require.NoError(t, err)

	require.NoError(t, w.Start())
	defer w.Stop()

	// 4. ExecuteWorkflow.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// CRITICAL: ContentHashFor returns (string, bool) per
	// pkg/interpreter/registry.go. Single-value assignment doesn't compile.
	// Use the two-value form with explicit assertions so the test fails
	// loudly if the trivial flow isn't registered.
	contentHash, ok := w.Registry().ContentHashFor("trivial")
	require.True(t, ok, "registry must have a content hash for the loaded flow")
	require.NotEmpty(t, contentHash)

	wopts := client.StartWorkflowOptions{
		ID:        "embed-test-" + t.Name(),
		TaskQueue: "skytime-embed-test",
	}
	run, err := c.ExecuteWorkflow(ctx, wopts, "SkytimeWorkflow", dag.WorkflowInput{
		FlowName:    "trivial",
		ContentHash: contentHash,
		InitState:   map[string]any{"x": int64(1)},
	})
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, run.Get(ctx, &result))

	// The script's output_alias="bumped" should hold the lambda's output dict.
	bumped, ok := result["bumped"]
	require.True(t, ok, "result must contain 'bumped' key from output_alias")
	bumpedMap, ok := bumped.(map[string]any)
	require.True(t, ok, "'bumped' must be a map")
	// Starlark math returns int64; JSON unmarshal lands it as float64.
	xPlusOne, ok := bumpedMap["x_plus_one"]
	require.True(t, ok)
	require.NotNil(t, xPlusOne)
}

// devServerReachable returns true when localhost:7233 accepts a TCP
// connection within 1 second. Cheap pre-flight check used to skip the
// integration test cleanly when no dev server is running.
func devServerReachable() bool {
	conn, err := net.DialTimeout("tcp", devAddr, 1*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
