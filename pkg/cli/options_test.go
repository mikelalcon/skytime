package cli

import (
	"testing"

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
