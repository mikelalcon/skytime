package worker

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
)

// fakeClient embeds client.Client. The embedded nil interface satisfies the
// type but every method panics if called — these tests don't call methods,
// only inspect the captured client.Options.
type fakeClient struct{ client.Client }

// Compile-time type assertion. If the SDK adds a new method to client.Client
// and our embed-pattern stops satisfying the interface, this line FAILS AT
// BUILD TIME (not at runtime via panic on the unexpected method call). Place
// near the type definition for visibility.
var _ client.Client = (*fakeClient)(nil)

// withFakeDial swaps clientDialFunc with a capturing fake for the duration of
// the test, returning the captured options pointer + the cleanup func.
func withFakeDial(t *testing.T) (*client.Options, func()) {
	t.Helper()
	captured := &client.Options{}
	orig := clientDialFunc
	clientDialFunc = func(o client.Options) (client.Client, error) {
		*captured = o
		return &fakeClient{}, nil
	}
	return captured, func() { clientDialFunc = orig }
}

// ---------------------------------------------------------------------------
// NewCloudClient
// ---------------------------------------------------------------------------

func TestNewCloudClient_RequiresHostPortNamespaceAPIKey(t *testing.T) {
	t.Run("missing HostPort", func(t *testing.T) {
		_, err := NewCloudClient(CloudOptions{Namespace: "n", APIKey: "k"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HostPort")
	})
	t.Run("missing Namespace", func(t *testing.T) {
		_, err := NewCloudClient(CloudOptions{HostPort: "h", APIKey: "k"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Namespace")
	})
	t.Run("missing APIKey", func(t *testing.T) {
		_, err := NewCloudClient(CloudOptions{HostPort: "h", Namespace: "n"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "APIKey")
	})
}

func TestNewCloudClient_BuildsCorrectOptions(t *testing.T) {
	captured, cleanup := withFakeDial(t)
	defer cleanup()

	c, err := NewCloudClient(CloudOptions{HostPort: "h", Namespace: "n", APIKey: "k"})
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "h", captured.HostPort)
	assert.Equal(t, "n", captured.Namespace)
	assert.NotNil(t, captured.Credentials, "NewAPIKeyStaticCredentials should produce a non-nil Credentials value")
	assert.Nil(t, captured.ConnectionOptions.TLS, "TLS should be nil — auto-enabled via Credentials per v1.39+")
	assert.False(t, captured.ConnectionOptions.TLSDisabled)
	assert.Contains(t, captured.Identity, "skytime/")
}

// ---------------------------------------------------------------------------
// NewSelfHostedClient
// ---------------------------------------------------------------------------

func TestNewSelfHostedClient_TLSConfigSet(t *testing.T) {
	captured, cleanup := withFakeDial(t)
	defer cleanup()

	c, err := NewSelfHostedClient(SelfHostedOptions{
		HostPort:   "h",
		Namespace:  "n",
		ClientCert: tlsCertFixture(t),
		ServerName: "temporal.example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, c)
	require.NotNil(t, captured.ConnectionOptions.TLS, "SelfHostedClient must set ConnectionOptions.TLS")
	assert.Equal(t, "temporal.example.com", captured.ConnectionOptions.TLS.ServerName)
	assert.Nil(t, captured.Credentials, "SelfHostedClient must not set Credentials (mTLS, not API-key)")
	assert.False(t, captured.ConnectionOptions.TLSDisabled)
}

func TestNewSelfHostedClient_RequiresHostPortNamespace(t *testing.T) {
	t.Run("missing HostPort", func(t *testing.T) {
		_, err := NewSelfHostedClient(SelfHostedOptions{Namespace: "n"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HostPort")
	})
	t.Run("missing Namespace", func(t *testing.T) {
		_, err := NewSelfHostedClient(SelfHostedOptions{HostPort: "h"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Namespace")
	})
}

// ---------------------------------------------------------------------------
// NewDevClient
// ---------------------------------------------------------------------------

func TestNewDevClient_TLSDisabled(t *testing.T) {
	captured, cleanup := withFakeDial(t)
	defer cleanup()

	c, err := NewDevClient(DevClientOptions{})
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.True(t, captured.ConnectionOptions.TLSDisabled)
	assert.Equal(t, "localhost:7233", captured.HostPort, "default HostPort")
	assert.Equal(t, "default", captured.Namespace, "default Namespace")
	assert.Nil(t, captured.Credentials)
	assert.Nil(t, captured.ConnectionOptions.TLS)
}

func TestNewDevClient_OverridesHostPort(t *testing.T) {
	captured, cleanup := withFakeDial(t)
	defer cleanup()

	_, err := NewDevClient(DevClientOptions{HostPort: "192.168.1.10:7233", Namespace: "test-ns"})
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.10:7233", captured.HostPort)
	assert.Equal(t, "test-ns", captured.Namespace)
	assert.True(t, captured.ConnectionOptions.TLSDisabled)
}

// ---------------------------------------------------------------------------
// Identity defaulting (cross-constructor)
// ---------------------------------------------------------------------------

func TestClientConstructors_IdentityDefault(t *testing.T) {
	t.Run("Cloud", func(t *testing.T) {
		captured, cleanup := withFakeDial(t)
		defer cleanup()
		_, err := NewCloudClient(CloudOptions{HostPort: "h", Namespace: "n", APIKey: "k"})
		require.NoError(t, err)
		assert.Equal(t, "skytime/"+defaultBuildID, captured.Identity)
	})
	t.Run("SelfHosted", func(t *testing.T) {
		captured, cleanup := withFakeDial(t)
		defer cleanup()
		_, err := NewSelfHostedClient(SelfHostedOptions{HostPort: "h", Namespace: "n"})
		require.NoError(t, err)
		assert.Equal(t, "skytime/"+defaultBuildID, captured.Identity)
	})
	t.Run("Dev", func(t *testing.T) {
		captured, cleanup := withFakeDial(t)
		defer cleanup()
		_, err := NewDevClient(DevClientOptions{})
		require.NoError(t, err)
		assert.Equal(t, "skytime/"+defaultBuildID, captured.Identity)
	})
}

func TestClientConstructors_IdentityHonored(t *testing.T) {
	captured, cleanup := withFakeDial(t)
	defer cleanup()
	_, err := NewDevClient(DevClientOptions{Identity: "my-app/v1"})
	require.NoError(t, err)
	assert.Equal(t, "my-app/v1", captured.Identity)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// tlsCertFixture returns an empty tls.Certificate for tests that only need to
// confirm the field is wired. The fake dial doesn't actually negotiate TLS,
// so the certificate contents are irrelevant.
func tlsCertFixture(t *testing.T) tls.Certificate {
	t.Helper()
	return tls.Certificate{}
}
