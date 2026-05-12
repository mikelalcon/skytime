package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nexus-rpc/sdk-go/nexus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
	"go.temporal.io/api/workflowservice/v1"
	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	sdkworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	skycore "github.com/mikelalcon/skytime/pkg/extension/builtin/core"
	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
	"github.com/mikelalcon/skytime/pkg/extension/schedules"
	"github.com/mikelalcon/skytime/pkg/interpreter"
	"github.com/mikelalcon/skytime/pkg/worker"
)

// TestServerCmd_Flags asserts every server-subcommand flag is registered
// with the correct type and default value, and that --rootdir is marked
// required. The acceptance criteria for this test live in the plan's
// must_haves block; flag inventory drift is a load-bearing regression
// surface for SERVER-01.
func TestServerCmd_Flags(t *testing.T) {
	cmd := newServerCommand(&config{})
	flagSpecs := []struct {
		name      string
		wantType  string
		wantValue string
	}{
		{"rootdir", "string", ""},
		{"task-queue", "string", "skytime"},
		{"addr", "string", ":8080"},
		{"credfile", "string", ""},
		{"drain-timeout", "duration", "30s"},
		{"json-log", "bool", "false"},
		{"cron-reconcile", "bool", "false"},
	}
	for _, spec := range flagSpecs {
		f := cmd.Flags().Lookup(spec.name)
		require.NotNil(t, f, "flag %q must be registered", spec.name)
		assert.Equal(t, spec.wantType, f.Value.Type(), "flag %q wrong type", spec.name)
		assert.Equal(t, spec.wantValue, f.DefValue, "flag %q wrong default", spec.name)
	}
	f := cmd.Flags().Lookup("rootdir")
	require.NotNil(t, f)
	assert.NotEmpty(t, f.Annotations[cobra.BashCompOneRequiredFlag], "rootdir must be marked required")
}

// TestServerCmd_DrainTimeoutRangeCheck_Zero: --drain-timeout=0s is rejected
// before any side effect (per § Pitfall 6 — pflag.Duration accepts zero
// syntactically; the RunE-side range check enforces a 1s floor).
func TestServerCmd_DrainTimeoutRangeCheck_Zero(t *testing.T) {
	cmd := newServerCommand(&config{})
	cmd.SetArgs([]string{"--rootdir=/tmp", "--drain-timeout=0s"})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drain-timeout must be at least 1s")
}

// TestServerCmd_DrainTimeoutRangeCheck_Negative: negative durations
// rejected with the same friendly error.
func TestServerCmd_DrainTimeoutRangeCheck_Negative(t *testing.T) {
	cmd := newServerCommand(&config{})
	cmd.SetArgs([]string{"--rootdir=/tmp", "--drain-timeout=-5s"})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drain-timeout must be at least 1s")
}

// TestServerCmd_DrainTimeoutRangeCheck_AboveOneHour: 2h is ACCEPTED
// (warning only). Failure must come from the downstream connect step,
// not the range check.
func TestServerCmd_DrainTimeoutRangeCheck_AboveOneHour(t *testing.T) {
	prev := defaultClientFactory
	defaultClientFactory = clientFactory{
		NewCloud:      func(_ worker.CloudOptions) (client.Client, error) { return nil, errors.New("test-stub") },
		NewSelfHosted: func(_ worker.SelfHostedOptions) (client.Client, error) { return nil, errors.New("test-stub") },
		NewDev:        func(_ worker.DevClientOptions) (client.Client, error) { return nil, errors.New("test-stub") },
	}
	t.Cleanup(func() { defaultClientFactory = prev })

	cmd := newServerCommand(&config{})
	cmd.SetArgs([]string{"--rootdir=/tmp", "--drain-timeout=2h"})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	require.Error(t, err) // expected: connect stub error (errSilent), NOT the range-check error
	assert.NotContains(t, err.Error(), "drain-timeout must be at least 1s",
		"2h must be accepted (warning only), not rejected")
}

// TestServerCmd_ConnectClient: the server subcommand reuses
// connectClient's D4-08 variant routing. Cloud (apiKey set) and dev
// (no flags) variants verified by stubbing defaultClientFactory and
// counting calls. Selfhosted variant is skipped — mTLS PEM fixtures
// are non-trivial and the variant is already covered in
// pkg/cli/connect_test.go for the run subcommand's identical surface.
func TestServerCmd_ConnectClient(t *testing.T) {
	cases := []struct {
		name     string
		cfg      *config
		wantCall string
	}{
		{"cloud", &config{apiKey: "k"}, "cloud"},
		{"dev", &config{}, "dev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cloudCalls, selfHostedCalls, devCalls int
			prev := defaultClientFactory
			defaultClientFactory = clientFactory{
				NewCloud:      func(_ worker.CloudOptions) (client.Client, error) { cloudCalls++; return nil, errors.New("stub") },
				NewSelfHosted: func(_ worker.SelfHostedOptions) (client.Client, error) { selfHostedCalls++; return nil, errors.New("stub") },
				NewDev:        func(_ worker.DevClientOptions) (client.Client, error) { devCalls++; return nil, errors.New("stub") },
			}
			t.Cleanup(func() { defaultClientFactory = prev })

			cmd := newServerCommand(tc.cfg)
			cmd.SetArgs([]string{"--rootdir=/tmp"})
			cmd.SetErr(&bytes.Buffer{})
			_ = cmd.Execute()

			switch tc.wantCall {
			case "cloud":
				assert.Equal(t, 1, cloudCalls)
				assert.Zero(t, selfHostedCalls)
				assert.Zero(t, devCalls)
			case "dev":
				assert.Zero(t, cloudCalls)
				assert.Zero(t, selfHostedCalls)
				assert.Equal(t, 1, devCalls)
			}
		})
	}
}

// TestServerCmd_ConnectClient_SelfHosted: the selfhosted variant is
// covered indirectly via pkg/cli/connect_test.go (which exercises the
// shared connectClient surface). Wiring a full mTLS test here would
// require valid PEM fixtures with no marginal coverage gain.
func TestServerCmd_ConnectClient_SelfHosted(t *testing.T) {
	t.Skip("TODO: mTLS PEM fixtures non-trivial; selfhosted variant covered in pkg/cli/connect_test.go")
}

// =============================================================================
// Phase 7.1 Plan 05 — signal-loop end-to-end tests (D-7.1-13)
// =============================================================================
//
// These three tests exercise the full RunE signal loop against a fake SDK
// worker injected via worker.WithSDKFactory (Plan 05 Task 1). Each test
// assigns testWorkerOptions to thread the option into the NewWorker call,
// installs a testDrainHook recorder, sends real SIGTERM(s) to the test
// process via syscall.Kill, and asserts the recorded six-stage sequence
// matches the LOCKED stage names from Phase 7 Plan 05.
//
// Synchronization: signal.Notify is installed AFTER w.Start() succeeds
// and BEFORE testDrainHook("worker_started") fires, so the
// "worker_started" recording is the synchronization point — once it
// lands, syscall.Kill(SIGTERM) is safe to dispatch.

// fakeReceivingWorker is a minimal sdkworker.Worker stub used by the
// drain-loop tests. Stop() behavior is parameterized via onStop so each
// test can choose: no-op (drain_completed), sleep-longer-than-timeout
// (drain_timeout), block-indefinitely (drain_forced via second signal).
type fakeReceivingWorker struct {
	onStop func()
}

func (*fakeReceivingWorker) RegisterWorkflow(_ interface{}) {}
func (*fakeReceivingWorker) RegisterWorkflowWithOptions(_ interface{}, _ workflow.RegisterOptions) {
}
func (*fakeReceivingWorker) RegisterDynamicWorkflow(_ interface{}, _ workflow.DynamicRegisterOptions) {
}
func (*fakeReceivingWorker) RegisterActivity(_ interface{}) {}
func (*fakeReceivingWorker) RegisterActivityWithOptions(_ interface{}, _ sdkactivity.RegisterOptions) {
}
func (*fakeReceivingWorker) RegisterDynamicActivity(_ interface{}, _ sdkactivity.DynamicRegisterOptions) {
}
func (*fakeReceivingWorker) RegisterNexusService(_ *nexus.Service) {}

func (*fakeReceivingWorker) Start() error                   { return nil }
func (*fakeReceivingWorker) Run(_ <-chan interface{}) error { return nil }

func (f *fakeReceivingWorker) Stop() {
	if f.onStop != nil {
		f.onStop()
	}
}

// Compile-time interface assertion.
var _ sdkworker.Worker = (*fakeReceivingWorker)(nil)

// stageRecorder is a thread-safe recorder for testDrainHook stages.
type stageRecorder struct {
	mu     sync.Mutex
	stages []string
	ch     chan string // buffered; one send per stage to enable Eventually waits
}

func newStageRecorder() *stageRecorder {
	return &stageRecorder{ch: make(chan string, 16)}
}

func (r *stageRecorder) record(stage string) {
	r.mu.Lock()
	r.stages = append(r.stages, stage)
	r.mu.Unlock()
	// Non-blocking send; channel is buffered.
	select {
	case r.ch <- stage:
	default:
	}
}

func (r *stageRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.stages))
	copy(out, r.stages)
	return out
}

// waitFor blocks until the recorder observes `target` or the deadline
// elapses. Returns true if observed, false on timeout.
func (r *stageRecorder) waitFor(target string, deadline time.Duration) bool {
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		// Fast path: already recorded.
		r.mu.Lock()
		for _, s := range r.stages {
			if s == target {
				r.mu.Unlock()
				return true
			}
		}
		r.mu.Unlock()
		select {
		case s := <-r.ch:
			if s == target {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

// makeServerTestDir writes a minimal .star file with one flow + one
// trigger so bootRegistry succeeds. Returns the dir.
func makeServerTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "flows.star"), []byte(`
flow(name = "test_flow", steps = [])
`), 0o644))
	return dir
}

// installFakeClientFactory swaps defaultClientFactory to return a stub
// nil-embedded client.Client (defer cleanup restores). Lets the connect
// stage succeed without dialing Temporal.
func installFakeClientFactory(t *testing.T) {
	t.Helper()
	prev := defaultClientFactory
	stub := &stubClient{}
	defaultClientFactory = clientFactory{
		NewCloud:      func(_ worker.CloudOptions) (client.Client, error) { return stub, nil },
		NewSelfHosted: func(_ worker.SelfHostedOptions) (client.Client, error) { return stub, nil },
		NewDev:        func(_ worker.DevClientOptions) (client.Client, error) { return stub, nil },
	}
	t.Cleanup(func() { defaultClientFactory = prev })
}

// stubClient is a no-op client.Client that the server subcommand's
// `defer c.Close()` can call safely. Phase 7.3 Plan 04: the dashboard's
// workflow poller calls ListOpenWorkflow / ListClosedWorkflow on a 2s
// cadence — provide no-op implementations that return empty responses
// so the poller goroutine doesn't nil-pan via the embedded
// client.Client. The poller swallows transient errors (Pitfall 4), but
// a nil-pointer dereference panics regardless.
type stubClient struct{ client.Client }

func (*stubClient) Close() {}

func (*stubClient) ListOpenWorkflow(_ context.Context, _ *workflowservice.ListOpenWorkflowExecutionsRequest) (*workflowservice.ListOpenWorkflowExecutionsResponse, error) {
	return &workflowservice.ListOpenWorkflowExecutionsResponse{}, nil
}

func (*stubClient) ListClosedWorkflow(_ context.Context, _ *workflowservice.ListClosedWorkflowExecutionsRequest) (*workflowservice.ListClosedWorkflowExecutionsResponse, error) {
	return &workflowservice.ListClosedWorkflowExecutionsResponse{}, nil
}

// runServerInBackground starts cmd.Execute in a goroutine and returns a
// channel that delivers the RunE return value when Execute returns.
func runServerInBackground(t *testing.T, cmd *cobra.Command, args []string) <-chan error {
	t.Helper()
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()
	return done
}

// nopCredHandler is a no-op CredentialHandler good enough for these
// drain tests which never resolve a credential.
type nopCredHandler struct{}

func (nopCredHandler) Resolve(_ context.Context, _ string) (extension.Credential, error) {
	return nil, errors.New("nopCredHandler: no credentials configured")
}

// TestServerCmd_DrainOnSIGTERM: first SIGTERM triggers drain; the fake
// worker.Stop returns immediately so drain_completed fires. RunE returns
// nil and testForceExit is NOT called. With Phase 7.1 Plan 06 the HTTP
// listener also binds + drains, so the recorded sequence is now 7 stages
// (worker_started → listener_started → signal_received →
// listener_shutdown_started → listener_shutdown_complete → drain_started
// → drain_completed).
func TestServerCmd_DrainOnSIGTERM(t *testing.T) {
	installFakeClientFactory(t)

	rec := newStageRecorder()
	prevHook := testDrainHook
	testDrainHook = rec.record
	t.Cleanup(func() { testDrainHook = prevHook })

	var forceExitCalls int
	var forceExitMu sync.Mutex
	prevForceExit := testForceExit
	testForceExit = func(code int) {
		forceExitMu.Lock()
		forceExitCalls++
		forceExitMu.Unlock()
	}
	t.Cleanup(func() { testForceExit = prevForceExit })

	fake := &fakeReceivingWorker{onStop: func() { /* no-op: returns immediately */ }}
	prevOpts := testWorkerOptions
	testWorkerOptions = []worker.Option{
		worker.WithSDKFactory(func(_ client.Client, _ string, _ sdkworker.Options) sdkworker.Worker {
			return fake
		}),
	}
	t.Cleanup(func() { testWorkerOptions = prevOpts })

	dir := makeServerTestDir(t)
	cmd := newServerCommand(&config{credHandler: nopCredHandler{}})
	done := runServerInBackground(t, cmd, []string{
		"--rootdir=" + dir,
		"--addr=127.0.0.1:0",
		"--drain-timeout=2s",
	})

	// Wait for the listener_started stage so both the worker AND
	// the HTTP listener are live before SIGTERM is dispatched.
	require.True(t, rec.waitFor("listener_started", 5*time.Second),
		"listener_started must be recorded before sending SIGTERM")

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	// Wait for drain_completed.
	require.True(t, rec.waitFor("drain_completed", 5*time.Second),
		"drain_completed must be recorded after SIGTERM with no-op Stop")

	// RunE must return nil (clean drain).
	select {
	case err := <-done:
		assert.NoError(t, err, "clean drain returns nil")
	case <-time.After(5 * time.Second):
		t.Fatal("RunE did not return after drain_completed")
	}

	stages := rec.snapshot()
	assert.Equal(t, []string{
		"worker_started",
		"listener_started",
		"signal_received",
		"listener_shutdown_started",
		"listener_shutdown_complete",
		"drain_started",
		"drain_completed",
	}, stages, "exact 7-stage sequence for clean drain (Plan 06 adds 3 listener stages)")

	forceExitMu.Lock()
	defer forceExitMu.Unlock()
	assert.Zero(t, forceExitCalls, "testForceExit must NOT be called on clean drain")
}

// TestServerCmd_DrainTimeoutExpiry: --drain-timeout fires before
// fakeWorker.Stop returns. With Plan 06's listener-first shutdown the
// drainCtx is shared between srv.Shutdown and w.Stop; the empty mux
// shuts down instantly, leaving the full drain budget for the worker.
// Sequence: worker_started → listener_started → signal_received →
// listener_shutdown_started → listener_shutdown_complete → drain_started
// → drain_timeout. RunE returns errSilent.
func TestServerCmd_DrainTimeoutExpiry(t *testing.T) {
	installFakeClientFactory(t)

	rec := newStageRecorder()
	prevHook := testDrainHook
	testDrainHook = rec.record
	t.Cleanup(func() { testDrainHook = prevHook })

	var forceExitCalls int
	var forceExitMu sync.Mutex
	prevForceExit := testForceExit
	testForceExit = func(code int) {
		forceExitMu.Lock()
		forceExitCalls++
		forceExitMu.Unlock()
	}
	t.Cleanup(func() { testForceExit = prevForceExit })

	// Stop blocks longer than --drain-timeout. We use a channel so the
	// test can release Stop on cleanup (avoids leaking goroutines).
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	fake := &fakeReceivingWorker{
		onStop: func() {
			select {
			case <-release:
			case <-time.After(5 * time.Second):
			}
		},
	}
	prevOpts := testWorkerOptions
	testWorkerOptions = []worker.Option{
		worker.WithSDKFactory(func(_ client.Client, _ string, _ sdkworker.Options) sdkworker.Worker {
			return fake
		}),
	}
	t.Cleanup(func() { testWorkerOptions = prevOpts })

	dir := makeServerTestDir(t)
	cmd := newServerCommand(&config{credHandler: nopCredHandler{}})
	// drain-timeout=1s so the test stays fast; Stop blocks >5s.
	done := runServerInBackground(t, cmd, []string{
		"--rootdir=" + dir,
		"--addr=127.0.0.1:0",
		"--drain-timeout=1s",
	})

	require.True(t, rec.waitFor("listener_started", 5*time.Second))
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))
	require.True(t, rec.waitFor("drain_timeout", 5*time.Second),
		"drain_timeout must fire when Stop blocks longer than --drain-timeout")

	select {
	case err := <-done:
		assert.ErrorIs(t, err, errSilent, "drain timeout returns errSilent")
	case <-time.After(5 * time.Second):
		t.Fatal("RunE did not return after drain_timeout")
	}

	stages := rec.snapshot()
	assert.Equal(t, []string{
		"worker_started",
		"listener_started",
		"signal_received",
		"listener_shutdown_started",
		"listener_shutdown_complete",
		"drain_started",
		"drain_timeout",
	}, stages, "exact stage sequence for drain timeout (Plan 06 adds 3 listener stages)")

	forceExitMu.Lock()
	defer forceExitMu.Unlock()
	assert.Zero(t, forceExitCalls, "testForceExit must NOT be called on drain_timeout (only on second signal)")
}

// TestServerCmd_SecondSignalForceExit: first SIGTERM starts drain; second
// SIGTERM during drain escalates via testForceExit(1). Sequence:
// worker_started → listener_started → signal_received →
// listener_shutdown_started → listener_shutdown_complete → drain_started
// → drain_forced.
func TestServerCmd_SecondSignalForceExit(t *testing.T) {
	installFakeClientFactory(t)

	rec := newStageRecorder()
	prevHook := testDrainHook
	testDrainHook = rec.record
	t.Cleanup(func() { testDrainHook = prevHook })

	var forceExitCalls int
	var forceExitCode int
	var forceExitMu sync.Mutex
	prevForceExit := testForceExit
	testForceExit = func(code int) {
		forceExitMu.Lock()
		forceExitCalls++
		forceExitCode = code
		forceExitMu.Unlock()
	}
	t.Cleanup(func() { testForceExit = prevForceExit })

	// Stop blocks indefinitely (until t.Cleanup releases it). The test
	// sends a SECOND signal during drain to escalate.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	fake := &fakeReceivingWorker{
		onStop: func() {
			<-release
		},
	}
	prevOpts := testWorkerOptions
	testWorkerOptions = []worker.Option{
		worker.WithSDKFactory(func(_ client.Client, _ string, _ sdkworker.Options) sdkworker.Worker {
			return fake
		}),
	}
	t.Cleanup(func() { testWorkerOptions = prevOpts })

	dir := makeServerTestDir(t)
	cmd := newServerCommand(&config{credHandler: nopCredHandler{}})
	// drain-timeout=10s so timeout doesn't beat the second signal.
	done := runServerInBackground(t, cmd, []string{
		"--rootdir=" + dir,
		"--addr=127.0.0.1:0",
		"--drain-timeout=10s",
	})

	require.True(t, rec.waitFor("listener_started", 5*time.Second))
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))
	require.True(t, rec.waitFor("drain_started", 5*time.Second),
		"drain_started must fire before sending second signal")

	// Second signal — escalates to forced exit. The RunE select blocks
	// on three channels (done, sigCh, drainCtx.Done). The buffered sigCh
	// (size 2) must hold this second send while Stop() is still blocked.
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	require.True(t, rec.waitFor("drain_forced", 5*time.Second),
		"drain_forced must fire on second signal")

	select {
	case err := <-done:
		// RunE returns nil (unreachable in production but reachable in
		// tests because testForceExit is overridden).
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("RunE did not return after drain_forced")
	}

	stages := rec.snapshot()
	assert.Equal(t, []string{
		"worker_started",
		"listener_started",
		"signal_received",
		"listener_shutdown_started",
		"listener_shutdown_complete",
		"drain_started",
		"drain_forced",
	}, stages, "exact stage sequence for forced exit (Plan 06 adds 3 listener stages)")

	forceExitMu.Lock()
	defer forceExitMu.Unlock()
	assert.Equal(t, 1, forceExitCalls, "testForceExit called exactly once on second signal")
	assert.Equal(t, 1, forceExitCode, "testForceExit code must be 1 (drain interrupted)")
}

// =============================================================================
// Phase 7 Plan 05 Task 7 — banner shape + json-log handler
// =============================================================================

// stringPtr returns &s — helper for syntax.MakePosition which takes
// *string for the filename component.
func stringPtr(s string) *string { return &s }

// TestServerCmd_BannerSorted exercises printStartupBanner directly
// against a Worker built via NewWorkerForTest. Three flows are
// registered out of order ("zebra", "alpha", "middle") and two triggers
// likewise. The banner must emit:
//
//	"starting server"      (rootdir, task-queue, addr)
//	"registered flows"     ([alpha, middle, zebra])
//	"registered triggers"  ([{source,flow=alpha,mount=POST /x},
//	                        {source,flow=zebra,mount=POST /x}])
//
// Plan 06 extends trigger lines with the "mount" key for HTTP-shaped
// sources (D-7.1 §Reusable Assets). This test now uses the real
// *httpWebhookSource (via NewHTTPWebhookSourceForTest) so the
// type-assertion to receiver.HTTPMounter inside printStartupBanner
// actually fires.
//
// Verifies SERVER-03 sorted output WITH the new mount-path extension,
// without booting a real Temporal connection or invoking the SDK worker.
func TestServerCmd_BannerSorted(t *testing.T) {
	flowReg := interpreter.NewRegistry()
	require.NoError(t, flowReg.Register("zebra", "h1", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "zebra"}}))
	require.NoError(t, flowReg.Register("alpha", "h2", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "alpha"}}))
	require.NoError(t, flowReg.Register("middle", "h3", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "middle"}}))
	flowReg.Freeze()

	// Real *httpWebhookSource — kind is "http.webhook", HTTPMount()
	// returns ("/x", "POST"). printStartupBanner type-asserts and
	// emits "mount":"POST /x" per Plan 06.
	httpSrc := skyhttp.NewHTTPWebhookSourceForTest("/x", "POST", "", "sha256", "X-Signature")

	trigReg := interpreter.NewTriggerRegistry()
	require.NoError(t, trigReg.Register("h1", &dag.Trigger{
		FlowName: "zebra",
		Source:   httpSrc,
		Pos:      syntax.MakePosition(stringPtr("flows.star"), 5, 1),
	}))
	require.NoError(t, trigReg.Register("h2", &dag.Trigger{
		FlowName: "alpha",
		Source:   httpSrc,
		Pos:      syntax.MakePosition(stringPtr("flows.star"), 11, 1),
	}))
	trigReg.Freeze()

	w := worker.NewWorkerForTest(flowReg, trigReg)

	// Capture banner output via JSON handler against a buffer so each
	// log record parses individually. setupServerLogging is intentionally
	// not used here (it mutates slog.Default).
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	printStartupBanner(logger, w, "/some/dir", "demo-queue", ":8080")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 3, "banner emits 3 records: starting server, registered flows, registered triggers")

	var startingServer, registeredFlows, registeredTriggers map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &startingServer))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &registeredFlows))
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &registeredTriggers))

	assert.Equal(t, "starting server", startingServer["msg"])
	assert.Equal(t, "/some/dir", startingServer["rootdir"])
	assert.Equal(t, "demo-queue", startingServer["task-queue"])
	assert.Equal(t, ":8080", startingServer["addr"])

	assert.Equal(t, "registered flows", registeredFlows["msg"])
	flows, _ := registeredFlows["flows"].([]any)
	assert.Equal(t, []any{"alpha", "middle", "zebra"}, flows)

	assert.Equal(t, "registered triggers", registeredTriggers["msg"])
	triggers, _ := registeredTriggers["triggers"].([]any)
	require.Len(t, triggers, 2)
	first, _ := triggers[0].(map[string]any)
	assert.Equal(t, "alpha", first["flow"])
	assert.Equal(t, "http.webhook", first["source"])
	assert.Equal(t, "POST /x", first["mount"], "Plan 06: trigger lines include mount path for HTTP-shaped sources")
	second, _ := triggers[1].(map[string]any)
	assert.Equal(t, "zebra", second["flow"])
	assert.Equal(t, "http.webhook", second["source"])
	assert.Equal(t, "POST /x", second["mount"], "Plan 06: trigger lines include mount path for HTTP-shaped sources")
}

// TestServerCmd_BannerSorted_NonHTTPSourceOmitsMount: cron sources (Phase
// 7.2) and queue sources (v1.44+) won't satisfy receiver.HTTPMounter; the
// banner must omit the "mount" key for those entries (no nil/empty value
// pollution).
func TestServerCmd_BannerSorted_NonHTTPSourceOmitsMount(t *testing.T) {
	flowReg := interpreter.NewRegistry()
	require.NoError(t, flowReg.Register("flow1", "h1", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "flow1"}}))
	flowReg.Freeze()

	trigReg := interpreter.NewTriggerRegistry()
	// FakeTriggerSource does NOT implement receiver.HTTPMounter (it has
	// no HTTPMount method). The banner must skip the "mount" key for
	// this entry.
	require.NoError(t, trigReg.Register("h1", &dag.Trigger{
		FlowName: "flow1",
		Source:   &extension.FakeTriggerSource{KindName: "skytime.test.webhook", ReqFields: []string{"payload"}},
		Pos:      syntax.MakePosition(stringPtr("flows.star"), 5, 1),
	}))
	trigReg.Freeze()

	w := worker.NewWorkerForTest(flowReg, trigReg)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	printStartupBanner(logger, w, "/some/dir", "demo-queue", ":8080")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 3)

	var registeredTriggers map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &registeredTriggers))

	triggers, _ := registeredTriggers["triggers"].([]any)
	require.Len(t, triggers, 1)
	first, _ := triggers[0].(map[string]any)
	assert.Equal(t, "flow1", first["flow"])
	assert.Equal(t, "skytime.test.webhook", first["source"])
	_, hasMount := first["mount"]
	assert.False(t, hasMount, "non-HTTP source must omit the mount key")
}

// TestServerCmd_JSONLog: setupServerLogging(debug=false, jsonMode=true)
// returns a *slog.Logger backed by slog.NewJSONHandler writing to
// os.Stderr. Capturing stderr via os.Pipe lets us assert the emitted
// record parses as JSON with the expected shape.
func TestServerCmd_JSONLog(t *testing.T) {
	prevStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = prevStderr })

	prevDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prevDefault) })

	logger := setupServerLogging(false, true)
	require.NotNil(t, logger)
	logger.Info("test record", "key1", "value1", "count", 42)

	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)

	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimRight(buf.String(), "\n")), &rec))
	assert.Equal(t, "test record", rec["msg"])
	assert.Equal(t, "value1", rec["key1"])
	assert.Equal(t, float64(42), rec["count"])
	assert.NotEmpty(t, rec["level"])
	assert.NotEmpty(t, rec["time"])
}

// =============================================================================
// Phase 7.1 Plan 06 — HTTP listener bind + drain tests (TRIG-06)
// =============================================================================

// TestServerCmd_ListenerBindsAfterWorkerStart: TRIG-06 boot-order proof.
//
// Asserts D-7.1-10 (worker first, then listener) AND D-7.1-11
// (listener-first shutdown — srv.Shutdown BEFORE w.Stop) by recording the
// LOCKED 9-stage testDrainHook sequence:
//
//	worker_started
//	listener_started
//	signal_received
//	listener_shutdown_started
//	listener_shutdown_complete
//	drain_started
//	drain_completed
//
// Order invariants verified explicitly:
//   - worker_started < listener_started (boot order)
//   - listener_shutdown_started < drain_started (shutdown order)
//   - all 7 stages present, in this exact sequence (regression gate for
//     any future reorder).
func TestServerCmd_ListenerBindsAfterWorkerStart(t *testing.T) {
	installFakeClientFactory(t)

	rec := newStageRecorder()
	prevHook := testDrainHook
	testDrainHook = rec.record
	t.Cleanup(func() { testDrainHook = prevHook })

	// fakeReceivingWorker.Stop returns immediately so drain_completed
	// fires cleanly. We're testing the boot+shutdown ORDER, not Stop
	// blocking semantics (those are pinned by TestServerCmd_DrainTimeoutExpiry).
	fake := &fakeReceivingWorker{onStop: func() { /* no-op */ }}
	prevOpts := testWorkerOptions
	testWorkerOptions = []worker.Option{
		worker.WithSDKFactory(func(_ client.Client, _ string, _ sdkworker.Options) sdkworker.Worker {
			return fake
		}),
	}
	t.Cleanup(func() { testWorkerOptions = prevOpts })

	dir := makeServerTestDir(t)
	cmd := newServerCommand(&config{credHandler: nopCredHandler{}})
	// :0 lets the OS assign a free port — avoids collisions when tests
	// run in parallel and prevents "address already in use" flakes.
	done := runServerInBackground(t, cmd, []string{
		"--rootdir=" + dir,
		"--addr=127.0.0.1:0",
		"--drain-timeout=2s",
	})

	// Sync point: listener_started fires AFTER worker_started + after
	// receiver.Mount + after net.Listen succeeds + after srv.Serve
	// goroutine has been spawned.
	require.True(t, rec.waitFor("listener_started", 5*time.Second),
		"listener_started must be recorded; net.Listen + srv.Serve goroutine spawned")

	// At this point the recorder MUST have worker_started before
	// listener_started — pinned by D-7.1-10.
	stagesAfterBoot := rec.snapshot()
	require.GreaterOrEqual(t, len(stagesAfterBoot), 2,
		"at minimum: [worker_started, listener_started]")
	assert.Equal(t, "worker_started", stagesAfterBoot[0],
		"D-7.1-10: worker_started must precede listener_started")
	assert.Equal(t, "listener_started", stagesAfterBoot[1],
		"D-7.1-10: listener_started must follow worker_started")

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))
	require.True(t, rec.waitFor("drain_completed", 5*time.Second))

	select {
	case err := <-done:
		assert.NoError(t, err, "clean drain returns nil")
	case <-time.After(5 * time.Second):
		t.Fatal("RunE did not return after drain_completed")
	}

	stages := rec.snapshot()
	assert.Equal(t, []string{
		"worker_started",
		"listener_started",
		"signal_received",
		"listener_shutdown_started",
		"listener_shutdown_complete",
		"drain_started",
		"drain_completed",
	}, stages, "Plan 06 LOCKED 9-stage testDrainHook sequence (3 new listener stages)")

	// Spot-check the load-bearing order invariants individually so a
	// future reorder produces a clear, named failure.
	idx := func(s string) int {
		for i, x := range stages {
			if x == s {
				return i
			}
		}
		return -1
	}
	assert.Less(t, idx("worker_started"), idx("listener_started"),
		"D-7.1-10: worker_started < listener_started (worker-first boot)")
	assert.Less(t, idx("listener_shutdown_started"), idx("drain_started"),
		"D-7.1-11: listener_shutdown_started < drain_started (listener-first drain)")
	assert.Less(t, idx("listener_shutdown_complete"), idx("drain_started"),
		"D-7.1-11: listener_shutdown_complete < drain_started (Shutdown returns before w.Stop)")
}

// TestServerCmd_HTTPServerDefaults: source-grep verification that
// pkg/cli/server.go ships the LOCKED D-7.1-12 HTTP server timeouts.
//
// This is a regression-prevention test, not a behavioral test — the
// httpServer struct is constructed inside RunE and is not directly
// inspectable from a black-box test. The test reads server.go and
// asserts the exact constants are present.
//
// Locked timeouts (D-7.1-12):
//   - ReadHeaderTimeout: 10 * time.Second  (slowloris defense)
//   - ReadTimeout:       30 * time.Second  (full request)
//   - WriteTimeout:      30 * time.Second  (full response)
//   - IdleTimeout:       60 * time.Second  (keep-alive ceiling)
//   - MaxHeaderBytes:    64 * 1024         (header DoS defense)
func TestServerCmd_HTTPServerDefaults(t *testing.T) {
	src, err := os.ReadFile("server.go")
	require.NoError(t, err, "server.go must be readable for source-grep verification")
	body := string(src)

	requiredLiterals := []string{
		"ReadHeaderTimeout: 10 * time.Second",
		"ReadTimeout:       30 * time.Second",
		"WriteTimeout:      30 * time.Second",
		"IdleTimeout:       60 * time.Second",
		"MaxHeaderBytes:    64 * 1024",
	}
	for _, lit := range requiredLiterals {
		assert.Contains(t, body, lit,
			"D-7.1-12 HTTP server defaults: server.go MUST contain literal %q", lit)
	}
}

// TestServerCmd_AddrFlagNoLongerWarns: Plan 06 removes the "no effect
// until Phase 7.1" warning that Phase 7 emitted when --addr was set
// explicitly. The warning is gone — the listener is now load-bearing.
//
// Source-grep verification: the warning string must not appear in
// server.go (regression gate against a careless re-add during a future
// edit).
func TestServerCmd_AddrFlagNoLongerWarns(t *testing.T) {
	src, err := os.ReadFile("server.go")
	require.NoError(t, err)
	body := string(src)

	assert.NotContains(t, body, "no effect until Phase 7.1",
		"Plan 06: the Phase 7 --addr warning must be removed (the listener is now load-bearing)")
	assert.NotContains(t, body, `cmd.Flags().Changed("addr")`,
		"Plan 06: the Changed(\"addr\") branch is no longer needed — listener always binds")
}

// =============================================================================
// Phase 7.2 Plan 03 — --cron-reconcile flag + cron banner rendering (SCHED-03)
// =============================================================================

// makeCronServerTestDir writes a temp rootdir with one flow and one
// core.cron(...) trigger so bootRegistry produces a *dag.Trigger whose
// Source is a *skycore.CronSource.
func makeCronServerTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// The `trigger(...)` primitive requires map= and idempotency_key=
	// kwargs (parser/builtins.go::builtinTrigger). For cron triggers the
	// req surface is [scheduled_time, actual_time]; the test fixture
	// uses a no-op map and a deterministic idempotency_key built from
	// scheduled_time so parsing succeeds.
	starContent := `flow(name = "weekly_digest", steps = [])
trigger(
    flow = "weekly_digest",
    source = core.cron(schedule = "0 9 * * 1", timezone = "America/New_York"),
    map = lambda req: {},
    idempotency_key = lambda req: str(req.scheduled_time),
)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "weekly_digest.star"), []byte(starContent), 0o644))
	return dir
}

// TestServerCmd_CronReconcileWiresThrough exercises the full end-to-end
// --cron-reconcile wiring: server boots → worker_started →
// cron_reconcile_complete → listener_started → SIGTERM → drain_completed.
// Asserts the FakeScheduleClient saw exactly one Create with the expected
// ID prefix + workflow name (SCHED-03 success criterion #1).
func TestServerCmd_CronReconcileWiresThrough(t *testing.T) {
	installFakeClientFactory(t)

	rec := newStageRecorder()
	prevHook := testDrainHook
	testDrainHook = rec.record
	t.Cleanup(func() { testDrainHook = prevHook })

	prevForceExit := testForceExit
	testForceExit = func(_ int) {}
	t.Cleanup(func() { testForceExit = prevForceExit })

	fake := &fakeReceivingWorker{onStop: func() { /* no-op */ }}
	prevOpts := testWorkerOptions
	testWorkerOptions = []worker.Option{
		worker.WithSDKFactory(func(_ client.Client, _ string, _ sdkworker.Options) sdkworker.Worker {
			return fake
		}),
	}
	t.Cleanup(func() { testWorkerOptions = prevOpts })

	fakeSC := schedules.NewFakeScheduleClient()
	dir := makeCronServerTestDir(t)

	cfg := &config{
		exts:            []extension.Extension{skyhttp.New(), skycore.New()},
		credHandler:     nopCredHandler{},
		scheduleFactory: func(_ client.Client) client.ScheduleClient { return fakeSC },
	}
	cmd := newServerCommand(cfg)
	done := runServerInBackground(t, cmd, []string{
		"--rootdir=" + dir,
		"--task-queue=demo",
		"--addr=127.0.0.1:0",
		"--drain-timeout=2s",
		"--cron-reconcile",
	})

	require.True(t, rec.waitFor("listener_started", 5*time.Second),
		"listener_started must follow cron_reconcile_complete")

	// Verify the reconciler was called exactly once and produced the
	// expected create.
	require.Len(t, fakeSC.CreateCalls, 1, "exactly one Create call (1 cron trigger in fixture)")
	createdOpts := fakeSC.CreateCalls[0]
	assert.True(t, strings.HasPrefix(createdOpts.ID, "skytime/weekly_digest/"),
		"Schedule ID prefixed with skytime/<flow>/; got %q", createdOpts.ID)
	wfAction, ok := createdOpts.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok, "Action must be *ScheduleWorkflowAction; got %T", createdOpts.Action)
	assert.Equal(t, "SkytimeWorkflow", wfAction.Workflow)

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))
	require.True(t, rec.waitFor("drain_completed", 5*time.Second))

	select {
	case err := <-done:
		assert.NoError(t, err, "clean exit on drain_completed")
	case <-time.After(5 * time.Second):
		t.Fatal("RunE did not return after drain_completed")
	}
}

// TestServerCmd_CronReconcileFailureExitsNonZero: when the
// ScheduleClient.List returns an error, the reconciler fails, the
// listener is NEVER bound, RunE returns errSilent, and stderr contains
// "cron-reconcile failed" (D-7.2-11 fail-loud).
func TestServerCmd_CronReconcileFailureExitsNonZero(t *testing.T) {
	installFakeClientFactory(t)

	rec := newStageRecorder()
	prevHook := testDrainHook
	testDrainHook = rec.record
	t.Cleanup(func() { testDrainHook = prevHook })

	fake := &fakeReceivingWorker{onStop: func() { /* no-op */ }}
	prevOpts := testWorkerOptions
	testWorkerOptions = []worker.Option{
		worker.WithSDKFactory(func(_ client.Client, _ string, _ sdkworker.Options) sdkworker.Worker {
			return fake
		}),
	}
	t.Cleanup(func() { testWorkerOptions = prevOpts })

	fakeSC := schedules.NewFakeScheduleClient()
	fakeSC.ListErr = errors.New("permission denied")

	dir := makeCronServerTestDir(t)

	cfg := &config{
		exts:            []extension.Extension{skyhttp.New(), skycore.New()},
		credHandler:     nopCredHandler{},
		scheduleFactory: func(_ client.Client) client.ScheduleClient { return fakeSC },
	}
	cmd := newServerCommand(cfg)
	cmd.SetArgs([]string{
		"--rootdir=" + dir,
		"--task-queue=demo",
		"--addr=127.0.0.1:0",
		"--drain-timeout=2s",
		"--cron-reconcile",
	})

	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(io.Discard)

	err := cmd.Execute()
	require.Error(t, err, "reconcile failure must propagate as error")
	assert.ErrorIs(t, err, errSilent)
	assert.Contains(t, stderr.String(), "cron-reconcile failed",
		"stderr must surface the reconcile failure (D-7.2-11 fail-loud)")

	stages := rec.snapshot()
	assert.NotContains(t, stages, "cron_reconcile_complete",
		"cron_reconcile_complete must NOT fire when reconcile errors")
	assert.NotContains(t, stages, "listener_started",
		"listener must NOT bind when reconcile fails (K8s readinessProbe stays unready)")

	// Worker started before reconcile attempted but the worker.Stop
	// path runs via defer'd flows. No explicit drain hooks since RunE
	// returns errSilent before the sigCh wait.
	assert.Contains(t, stages, "worker_started",
		"worker_started fires before reconcile is attempted")
}

// TestServerCmd_CronReconcileBootOrder pins the LOCKED stage sequence
// for --cron-reconcile=true: worker_started precedes
// cron_reconcile_complete which precedes listener_started (D-7.2-16
// boot order).
func TestServerCmd_CronReconcileBootOrder(t *testing.T) {
	installFakeClientFactory(t)

	rec := newStageRecorder()
	prevHook := testDrainHook
	testDrainHook = rec.record
	t.Cleanup(func() { testDrainHook = prevHook })

	fake := &fakeReceivingWorker{onStop: func() { /* no-op */ }}
	prevOpts := testWorkerOptions
	testWorkerOptions = []worker.Option{
		worker.WithSDKFactory(func(_ client.Client, _ string, _ sdkworker.Options) sdkworker.Worker {
			return fake
		}),
	}
	t.Cleanup(func() { testWorkerOptions = prevOpts })

	fakeSC := schedules.NewFakeScheduleClient()
	dir := makeCronServerTestDir(t)

	cfg := &config{
		exts:            []extension.Extension{skyhttp.New(), skycore.New()},
		credHandler:     nopCredHandler{},
		scheduleFactory: func(_ client.Client) client.ScheduleClient { return fakeSC },
	}
	cmd := newServerCommand(cfg)
	done := runServerInBackground(t, cmd, []string{
		"--rootdir=" + dir,
		"--task-queue=demo",
		"--addr=127.0.0.1:0",
		"--drain-timeout=2s",
		"--cron-reconcile",
	})

	require.True(t, rec.waitFor("listener_started", 5*time.Second))
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))
	require.True(t, rec.waitFor("drain_completed", 5*time.Second))
	<-done

	stages := rec.snapshot()
	require.GreaterOrEqual(t, len(stages), 3)
	assert.Equal(t, "worker_started", stages[0])
	assert.Equal(t, "cron_reconcile_complete", stages[1])
	assert.Equal(t, "listener_started", stages[2])

	idx := func(s string) int {
		for i, x := range stages {
			if x == s {
				return i
			}
		}
		return -1
	}
	assert.Less(t, idx("worker_started"), idx("cron_reconcile_complete"),
		"D-7.2-16: worker_started < cron_reconcile_complete")
	assert.Less(t, idx("cron_reconcile_complete"), idx("listener_started"),
		"D-7.2-16: cron_reconcile_complete < listener_started")
}

// TestServerCmd_CronReconcileSkippedWithoutFlag: without
// --cron-reconcile, ZERO ScheduleClient API calls happen and
// cron_reconcile_complete is NOT emitted (D-7.2-17).
func TestServerCmd_CronReconcileSkippedWithoutFlag(t *testing.T) {
	installFakeClientFactory(t)

	rec := newStageRecorder()
	prevHook := testDrainHook
	testDrainHook = rec.record
	t.Cleanup(func() { testDrainHook = prevHook })

	fake := &fakeReceivingWorker{onStop: func() { /* no-op */ }}
	prevOpts := testWorkerOptions
	testWorkerOptions = []worker.Option{
		worker.WithSDKFactory(func(_ client.Client, _ string, _ sdkworker.Options) sdkworker.Worker {
			return fake
		}),
	}
	t.Cleanup(func() { testWorkerOptions = prevOpts })

	fakeSC := schedules.NewFakeScheduleClient()
	dir := makeCronServerTestDir(t)

	cfg := &config{
		exts:            []extension.Extension{skyhttp.New(), skycore.New()},
		credHandler:     nopCredHandler{},
		scheduleFactory: func(_ client.Client) client.ScheduleClient { return fakeSC },
	}
	cmd := newServerCommand(cfg)
	done := runServerInBackground(t, cmd, []string{
		"--rootdir=" + dir,
		"--task-queue=demo",
		"--addr=127.0.0.1:0",
		"--drain-timeout=2s",
		// NO --cron-reconcile flag.
	})

	require.True(t, rec.waitFor("listener_started", 5*time.Second))
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))
	require.True(t, rec.waitFor("drain_completed", 5*time.Second))
	<-done

	stages := rec.snapshot()
	assert.NotContains(t, stages, "cron_reconcile_complete",
		"cron_reconcile_complete must NOT fire without --cron-reconcile (D-7.2-17)")

	assert.Empty(t, fakeSC.CreateCalls, "no Create when --cron-reconcile is absent")
	assert.Empty(t, fakeSC.UpdateCalls, "no Update when --cron-reconcile is absent")
	assert.Empty(t, fakeSC.DeleteCalls, "no Delete when --cron-reconcile is absent")
	// The fake's ListErr is unused; whether List was hit is recorded
	// implicitly — the reconciler never runs, so List can never be
	// called by our code.
}

// TestServerCmd_BannerSorted_CronSourceRendersScheduleInMount asserts
// printStartupBanner renders cron triggers with `cron @ {schedule}
// ({timezone})` in the mount field — distinct from the HTTP-shaped
// `METHOD path` format.
func TestServerCmd_BannerSorted_CronSourceRendersScheduleInMount(t *testing.T) {
	flowReg := interpreter.NewRegistry()
	require.NoError(t, flowReg.Register("weekly_digest", "h1", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "weekly_digest"}}))
	flowReg.Freeze()

	cronSource := skycore.NewCronSourceForTest("0 9 * * 1", "America/New_York", "skip", nil)
	trigReg := interpreter.NewTriggerRegistry()
	require.NoError(t, trigReg.Register("h1", &dag.Trigger{
		FlowName: "weekly_digest",
		Source:   cronSource,
		Pos:      syntax.MakePosition(stringPtr("weekly_digest.star"), 5, 1),
	}))
	trigReg.Freeze()

	w := worker.NewWorkerForTest(flowReg, trigReg)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	printStartupBanner(logger, w, "/tmp/rootdir", "demo", ":8080")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 3, "banner emits 3 records")

	var registeredTriggers map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &registeredTriggers))

	triggers, _ := registeredTriggers["triggers"].([]any)
	require.Len(t, triggers, 1)
	first, _ := triggers[0].(map[string]any)
	assert.Equal(t, "weekly_digest", first["flow"])
	assert.Equal(t, "core.cron", first["source"])
	assert.Equal(t, "cron @ 0 9 * * 1 (America/New_York)", first["mount"],
		"Phase 7.2 Plan 03: cron triggers render schedule + timezone in mount field")
}

// =============================================================================
// Phase 7.2.1 Plan 04 — logKindFilterHandler wiring tests (LOG-02 D-7.2.1-13/14)
// =============================================================================

// TestServer_LogFilterAttached_HumanMode: in non-JSON mode, kind=log
// step_dispatch + step_complete records emitted through the logger
// returned by setupServerLogging are suppressed; the user-message
// record (no `event` attr) and unrelated kinds (script/step/...) still
// render. D-7.2.1-13.
//
// charm-log writes to os.Stderr; we redirect stderr to a pipe BEFORE
// calling setupServerLogging so the handler captures os.Stderr at
// construction time and emits into our pipe.
func TestServer_LogFilterAttached_HumanMode(t *testing.T) {
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	prevDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prevDefault) })

	logger := setupServerLogging(false /*debug*/, false /*jsonMode*/)
	require.NotNil(t, logger)

	logger.Info("skytime", "event", "step_dispatch", "kind", "log", "label", "log")
	logger.Info("skytime", "event", "step_complete", "kind", "log", "status", "ok")
	logger.Info("[skytime/log] weekly digest complete", "kind", "log")
	logger.Info("workflow start", "event", "step_dispatch", "kind", "script", "label", "seed_authors")

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	got := string(out)

	require.NotContains(t, got, "event=step_dispatch kind=log",
		"kind=log step_dispatch must be suppressed in human mode")
	require.NotContains(t, got, "event=step_complete kind=log",
		"kind=log step_complete must be suppressed in human mode")
	require.Contains(t, got, "[skytime/log] weekly digest complete",
		"user-message record must pass through")
	require.Contains(t, got, "seed_authors",
		"non-log script dispatch must still render")
}

// TestServer_LogFilterNotAttached_JSONMode: in --json-log mode, all
// three records (dispatch + user message + complete) appear verbatim
// per D-7.2.1-14 — log-analysis tools need the full step graph.
func TestServer_LogFilterNotAttached_JSONMode(t *testing.T) {
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	prevDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prevDefault) })

	logger := setupServerLogging(false /*debug*/, true /*jsonMode*/)
	require.NotNil(t, logger)

	logger.Info("skytime", "event", "step_dispatch", "kind", "log", "label", "log")
	logger.Info("[skytime/log] hi", "kind", "log")
	logger.Info("skytime", "event", "step_complete", "kind", "log", "status", "ok")

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	got := string(out)

	// JSON mode: all three records present verbatim.
	require.Contains(t, got, `"event":"step_dispatch"`)
	require.Contains(t, got, `"event":"step_complete"`)
	require.Contains(t, got, "[skytime/log] hi")
	// All three records carry kind=log — JSON mode emits everything.
	require.Equal(t, 3, strings.Count(got, `"kind":"log"`),
		"JSON mode must emit all three kind=log records verbatim (D-7.2.1-14)")
}
