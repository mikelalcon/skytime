package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
)

// TestWithScheduleClientFactory_SetsField asserts the option populates
// c.scheduleFactory and that the captured closure is invokable. Function
// pointer identity comparison in Go is unreliable for non-nil values,
// so we verify behavior (closure side effect) instead of pointer
// equality.
func TestWithScheduleClientFactory_SetsField(t *testing.T) {
	called := false
	fn := func(_ client.Client) client.ScheduleClient {
		called = true
		return nil
	}
	cfg := &config{}
	require.NoError(t, WithScheduleClientFactory(fn)(cfg))
	require.NotNil(t, cfg.scheduleFactory)
	// Invoke the factory to confirm the closure was stored
	// unmodified.
	cfg.scheduleFactory(nil)
	require.True(t, called)
}

// TestWithScheduleClientFactory_NilAccepted: passing nil sets
// scheduleFactory to nil. Production callers omit the option; tests
// that DON'T set it get the default c.ScheduleClient() behavior in the
// server / cron-plan subcommands.
func TestWithScheduleClientFactory_NilAccepted(t *testing.T) {
	cfg := &config{}
	require.NoError(t, WithScheduleClientFactory(nil)(cfg))
	require.Nil(t, cfg.scheduleFactory)
}

// TestWithBuildID_SetsConfigField pins the Option closure → buildID
// field assignment (CLI-09 surface contract).
func TestWithBuildID_SetsConfigField(t *testing.T) {
	cfg := &config{}
	require.NoError(t, WithBuildID("v1.43.0-abcdef")(cfg))
	assert.Equal(t, "v1.43.0-abcdef", cfg.buildID,
		"WithBuildID must store the value verbatim on cfg.buildID")
}

// TestWithBuildID_EmptyAcceptedNoOp pins the no-validation contract:
// empty string is accepted (matches WithExtensions / WithCredentialHandler
// precedent). Callers can pass `os.Getenv("BUILD_ID")` without guarding.
func TestWithBuildID_EmptyAcceptedNoOp(t *testing.T) {
	cfg := &config{}
	require.NoError(t, WithBuildID("")(cfg))
	assert.Equal(t, "", cfg.buildID,
		"WithBuildID(empty) must be a no-op no-error (callers pass os.Getenv freely)")
}

// TestWithBuildID_OverwritesPriorCall pins last-wins semantics for
// repeated calls in the same option chain (standard Go option idiom).
func TestWithBuildID_OverwritesPriorCall(t *testing.T) {
	cfg := &config{}
	require.NoError(t, WithBuildID("v1")(cfg))
	require.NoError(t, WithBuildID("v2")(cfg))
	assert.Equal(t, "v2", cfg.buildID, "later WithBuildID call must win")
}

// TestWithBuildID_FlowsToWorkerOptions pins CLI-09 success criterion 2:
// cli.WithBuildID("v1.43.0-abcdef") → worker.WorkerOptions.BuildID ==
// "v1.43.0-abcdef" at the seam pkg/cli uses to construct WorkerOptions.
//
// We don't actually invoke worker.NewWorker (that needs a Temporal
// client). Instead we apply the option to a *config and assert the
// field that the run.go and server.go construction blocks read.
// Combined with the source-grep acceptance criterion ("BuildID:
// cfg.buildID" present in both run.go and server.go), this pins the
// full chain.
func TestWithBuildID_FlowsToWorkerOptions(t *testing.T) {
	cfg := &config{}
	require.NoError(t, WithBuildID("v1.43.0-abcdef")(cfg))

	// The construction sites in run.go and server.go do:
	//   worker.WorkerOptions{ ..., BuildID: cfg.buildID, ... }
	// Asserting cfg.buildID here pins the value the WorkerOptions
	// literal will receive. The worker's own pkg/worker/options_test.go
	// (TestWorkerOptions_ExplicitOverrides) pins the worker-side property
	// that BuildID flows through applyDefaults. Together those two tests
	// cover the full Option → WorkerOptions field chain.
	assert.Equal(t, "v1.43.0-abcdef", cfg.buildID,
		"cfg.buildID must equal the value passed to WithBuildID; the run.go "+
			"and server.go construction sites read this field directly into "+
			"worker.WorkerOptions.BuildID (verify via grep against those files)")
}

// TestWithBuildID_AbsentLeavesWorkerOptionsBuildIDEmpty pins the
// fall-back-to-default contract: when WithBuildID is NOT called,
// cfg.buildID is empty and worker.WorkerOptions.BuildID stays empty
// until WorkerOptions.applyDefaults assigns defaultBuildID.
func TestWithBuildID_AbsentLeavesWorkerOptionsBuildIDEmpty(t *testing.T) {
	cfg := &config{} // no options applied
	assert.Equal(t, "", cfg.buildID,
		"default cfg.buildID must be empty so worker.WorkerOptions.applyDefaults can fall back to defaultBuildID")
}
