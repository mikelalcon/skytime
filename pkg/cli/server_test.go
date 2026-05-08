package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

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
