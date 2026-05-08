package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
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
// Phase 7 Plan 05 Task 6 — signal-loop test stubs (deferred to Phase 7.1)
// =============================================================================
//
// Reachability blocker: pkg/cli is a black-box consumer of pkg/worker. The
// worker.sdkWorkerNew seam used by Plan 04's worker_test.go to inject a
// fake SDK Worker is package-private. Without an exported
// worker.WithSDKFactory(fn) Option, pkg/cli tests cannot construct a
// Worker whose Stop() behavior is deterministic — and the signal-loop
// tests need exactly that.
//
// The function NAMES below match VALIDATION.md's per-task verification
// map so forward compatibility is preserved. The testDrainHook stage
// names are LOCKED in source (regression-prevented by source grep on
// server.go), and the manual-smoke verification in VALIDATION.md
// covers actual end-to-end behavior. Phase 7.1 will drop the t.Skip
// and implement the assertions once worker.WithSDKFactory ships.

// TestServerCmd_DrainOnSIGTERM: SIGTERM during a running worker triggers
// drain via worker.Stop. Skipped pending the Phase 7.1 SDK-factory seam.
func TestServerCmd_DrainOnSIGTERM(t *testing.T) {
	t.Skip("TODO(phase-7.1): pkg/cli tests cannot reach pkg/worker.sdkWorkerNew seam. Add exported worker.WithSDKFactory(fn) Option to enable in-process drain testing. Source-grep acceptance + testDrainHook stage names + manual smoke (VALIDATION.md § Manual-Only Verifications) cover this for Phase 7.")
}

// TestServerCmd_DrainTimeoutExpiry: drain that exceeds --drain-timeout
// returns errSilent and logs the timeout diagnostic. Skipped pending
// the Phase 7.1 SDK-factory seam.
func TestServerCmd_DrainTimeoutExpiry(t *testing.T) {
	t.Skip("TODO(phase-7.1): same reachability blocker as TestServerCmd_DrainOnSIGTERM — needs worker.WithSDKFactory")
}

// TestServerCmd_SecondSignalForceExit: a second SIGINT/SIGTERM during
// drain calls testForceExit(1). Skipped pending the Phase 7.1
// SDK-factory seam (and a testForceExit-override harness wired with it).
func TestServerCmd_SecondSignalForceExit(t *testing.T) {
	t.Skip("TODO(phase-7.1): same reachability blocker — also needs testForceExit override harness wired with the seam")
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
// likewise ("zebra-flow", "alpha-flow"). The banner must emit:
//
//	"starting server"      (rootdir, task-queue, addr)
//	"registered flows"     ([alpha, middle, zebra])
//	"registered triggers"  ([{source,flow=alpha}, {source,flow=zebra}])
//
// Verifies SERVER-03 sorted output without booting a real Temporal
// connection or invoking the SDK worker.
func TestServerCmd_BannerSorted(t *testing.T) {
	flowReg := interpreter.NewRegistry()
	require.NoError(t, flowReg.Register("zebra", "h1", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "zebra"}}))
	require.NoError(t, flowReg.Register("alpha", "h2", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "alpha"}}))
	require.NoError(t, flowReg.Register("middle", "h3", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "middle"}}))
	flowReg.Freeze()

	trigReg := interpreter.NewTriggerRegistry()
	require.NoError(t, trigReg.Register("h1", &dag.Trigger{
		FlowName: "zebra",
		Source:   &extension.FakeTriggerSource{KindName: "skytime.test.webhook", ReqFields: []string{"payload"}},
		Pos:      syntax.MakePosition(stringPtr("flows.star"), 5, 1),
	}))
	require.NoError(t, trigReg.Register("h2", &dag.Trigger{
		FlowName: "alpha",
		Source:   &extension.FakeTriggerSource{KindName: "skytime.test.webhook", ReqFields: []string{"payload"}},
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
	assert.Equal(t, "skytime.test.webhook", first["source"])
	second, _ := triggers[1].(map[string]any)
	assert.Equal(t, "zebra", second["flow"])
	assert.Equal(t, "skytime.test.webhook", second["source"])
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
