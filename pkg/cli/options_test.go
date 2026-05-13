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
