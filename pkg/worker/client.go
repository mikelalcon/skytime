package worker

import (
	"crypto/tls"

	"go.temporal.io/sdk/client"
)

// clientDialFunc is the test seam — production assigns client.Dial; tests can
// override to capture client.Options without spinning up a real Temporal
// server.
var clientDialFunc = client.Dial

// NewCloudClient connects to Temporal Cloud (D3-17). v1.39+ behavior:
// providing APIKey auto-enables TLS — do NOT set ConnectionOptions.TLS.
func NewCloudClient(opts CloudOptions) (client.Client, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	return clientDialFunc(client.Options{
		HostPort:    opts.HostPort,
		Namespace:   opts.Namespace,
		Credentials: client.NewAPIKeyStaticCredentials(opts.APIKey),
		Identity:    coalesce(opts.Identity, "skytime/"+defaultBuildID),
		// ConnectionOptions intentionally zero — TLS auto-enabled via Credentials per v1.39+
	})
}

// NewSelfHostedClient connects to a self-hosted Temporal cluster with mTLS
// (D3-17).
func NewSelfHostedClient(opts SelfHostedOptions) (client.Client, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{opts.ClientCert},
		RootCAs:      opts.RootCAs,
		ServerName:   opts.ServerName,
		MinVersion:   tls.VersionTLS12,
	}
	return clientDialFunc(client.Options{
		HostPort:  opts.HostPort,
		Namespace: opts.Namespace,
		ConnectionOptions: client.ConnectionOptions{
			TLS: tlsCfg,
		},
		Identity: coalesce(opts.Identity, "skytime/"+defaultBuildID),
	})
}

// NewDevClient connects to a local dev server (no TLS) (D3-17).
func NewDevClient(opts DevClientOptions) (client.Client, error) {
	return clientDialFunc(client.Options{
		HostPort:  coalesce(opts.HostPort, defaultDevHostPort),
		Namespace: coalesce(opts.Namespace, defaultDevNamespace),
		ConnectionOptions: client.ConnectionOptions{
			TLSDisabled: true, // explicit; v1.39+ default is TLS-on for API-key paths
		},
		Identity: coalesce(opts.Identity, "skytime/"+defaultBuildID),
	})
}
