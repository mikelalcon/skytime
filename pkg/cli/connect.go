package cli

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/worker"
)

// clientFactory is the test seam — production assigns the real worker
// constructors via defaultClientFactory; tests construct a custom
// clientFactory to capture which variant was chosen without dialing
// Temporal. Mirrors pkg/worker.clientDialFunc and pkg/worker.sdkWorkerNew
// (Phase 3 plan 03-04 seam pattern).
type clientFactory struct {
	NewCloud      func(worker.CloudOptions) (client.Client, error)
	NewSelfHosted func(worker.SelfHostedOptions) (client.Client, error)
	NewDev        func(worker.DevClientOptions) (client.Client, error)
}

// defaultClientFactory uses the real worker constructors.
var defaultClientFactory = clientFactory{
	NewCloud:      worker.NewCloudClient,
	NewSelfHosted: worker.NewSelfHostedClient,
	NewDev:        worker.NewDevClient,
}

// connectClient routes flag/env values to the right worker constructor
// per D4-08 (Phase 4 CONTEXT.md):
//
//	--api-key set                       → NewCloudClient (TLS auto-enabled)
//	--client-cert + --client-key set    → NewSelfHostedClient (mTLS)
//	otherwise                           → NewDevClient (TLS off)
//
// The mTLS-half-set case (only --client-cert OR only --client-key)
// surfaces a CLI-side validation error before any factory is invoked —
// the worker constructor would also catch this, but a CLI-side check
// produces a friendlier message that points at the missing flag.
//
// Calls a package-level factory so tests can capture the choice without
// dialing real Temporal.
func connectClient(cfg *config) (client.Client, error) {
	return connectClientWithFactory(cfg, defaultClientFactory)
}

// connectClientWithFactory is the test-friendly variant of connectClient.
// Production code calls connectClient; tests inject a custom factory that
// captures which constructor was selected.
//
// Quick 260502-guu Fix B: cfg.sdkLogger is threaded into the chosen
// constructor's options struct via .Logger so the SDK client's
// gRPC-side logs flow through the same handler as the worker's.
func connectClientWithFactory(cfg *config, f clientFactory) (client.Client, error) {
	switch {
	case cfg.apiKey != "":
		return f.NewCloud(worker.CloudOptions{
			HostPort:  cfg.address,
			Namespace: cfg.namespace,
			APIKey:    cfg.apiKey,
			Logger:    cfg.sdkLogger,
		})
	case cfg.clientCert != "" && cfg.clientKey != "":
		cert, err := tls.LoadX509KeyPair(cfg.clientCert, cfg.clientKey)
		if err != nil {
			return nil, fmt.Errorf("load mTLS keypair: %w", err)
		}
		opts := worker.SelfHostedOptions{
			HostPort:   cfg.address,
			Namespace:  cfg.namespace,
			ClientCert: cert,
			Logger:     cfg.sdkLogger,
		}
		if cfg.serverCA != "" {
			pool := x509.NewCertPool()
			ca, err := os.ReadFile(cfg.serverCA)
			if err != nil {
				return nil, fmt.Errorf("read --server-ca: %w", err)
			}
			if !pool.AppendCertsFromPEM(ca) {
				return nil, errors.New("--server-ca: no PEM certificates appended")
			}
			opts.RootCAs = pool
		}
		return f.NewSelfHosted(opts)
	case cfg.clientCert != "" || cfg.clientKey != "":
		return nil, errors.New("--client-cert and --client-key must be supplied together for mTLS")
	default:
		return f.NewDev(worker.DevClientOptions{
			HostPort:  cfg.address,
			Namespace: cfg.namespace,
			Logger:    cfg.sdkLogger,
		})
	}
}
