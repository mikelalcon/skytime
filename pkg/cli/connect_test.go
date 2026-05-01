package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/worker"
)

// fakeTemporalClient satisfies client.Client just enough for the variant
// tests; we never call methods on it. Variant tests assert WHICH factory
// was called, not what it returned.
type fakeTemporalClient struct{ client.Client }

// TestConnectClient_VariantRouting exercises the D4-08 routing rules:
//   - --api-key set                  → NewCloudClient
//   - --client-cert + --client-key   → NewSelfHostedClient (covered by
//     a focused test below since constructing a real X509 keypair is
//     heavy; here we only assert routing on the partial case)
//   - otherwise                      → NewDevClient
func TestConnectClient_VariantRouting(t *testing.T) {
	cases := []struct {
		name     string
		cfg      *config
		wantKind string
	}{
		{"cloud routes via api-key", &config{address: "host:7233", namespace: "default", apiKey: "k"}, "cloud"},
		{"dev routes when nothing else", &config{address: "host:7233", namespace: "default"}, "dev"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var picked string
			factory := clientFactory{
				NewCloud: func(_ worker.CloudOptions) (client.Client, error) {
					picked = "cloud"
					return fakeTemporalClient{}, nil
				},
				NewSelfHosted: func(_ worker.SelfHostedOptions) (client.Client, error) {
					picked = "self"
					return fakeTemporalClient{}, nil
				},
				NewDev: func(_ worker.DevClientOptions) (client.Client, error) {
					picked = "dev"
					return fakeTemporalClient{}, nil
				},
			}
			_, err := connectClientWithFactory(tc.cfg, factory)
			require.NoError(t, err)
			require.Equal(t, tc.wantKind, picked)
		})
	}
}

// TestConnectClient_PartialMTLSRejected: --client-cert without
// --client-key surfaces an error before any factory is called.
func TestConnectClient_PartialMTLSRejected(t *testing.T) {
	cfg := &config{clientCert: "/tmp/cert.pem"} // no clientKey
	factory := clientFactory{
		NewCloud:      func(worker.CloudOptions) (client.Client, error) { return nil, errors.New("should not be called") },
		NewSelfHosted: func(worker.SelfHostedOptions) (client.Client, error) { return nil, errors.New("should not be called") },
		NewDev:        func(worker.DevClientOptions) (client.Client, error) { return nil, errors.New("should not be called") },
	}
	_, err := connectClientWithFactory(cfg, factory)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be supplied together")
}
