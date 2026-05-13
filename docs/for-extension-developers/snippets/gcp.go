package snippets

import (
	"context"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"go.temporal.io/sdk/client"
)

// newGCPCredentials returns Temporal Cloud client credentials backed by a
// Google Secret Manager secret. The Secret Manager client uses Application
// Default Credentials — when running on GKE with Workload Identity
// Federation configured for the pod's service account, ADC transparently
// exchanges the WIF token for a short-lived GCP access token. No JSON key
// file is needed in the container.
//
// The returned Credentials is the rotation-friendly variant: Temporal's
// SDK calls accessLatest before each RPC, so re-versioning the secret in
// GSM propagates without restarting the worker. Cache the value in a
// package-level var with a TTL if your RPC volume makes per-call GSM
// reads expensive.
//
// The Temporal SDK auto-enables TLS when Credentials are set (v1.39+);
// do NOT disable TLS via ConnectionOptions on the returned client.Options.
func newGCPCredentials(ctx context.Context, projectID, secretName string) (client.Credentials, error) {
	sm, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcp: construct secret manager client: %w", err)
	}
	// Note: this snippet leaks the secretmanager client on intentional shutdown
	// — production binaries should keep a package-level handle and call sm.Close()
	// from their shutdown path.

	accessLatest := func(ctx context.Context) (string, error) {
		resp, err := sm.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
			Name: fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretName),
		})
		if err != nil {
			return "", fmt.Errorf("gcp: access secret %q in project %q: %w", secretName, projectID, err)
		}
		return string(resp.Payload.Data), nil
	}
	return client.NewAPIKeyDynamicCredentials(accessLatest), nil
}
