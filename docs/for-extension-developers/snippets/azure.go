package snippets

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/keyvault/azsecrets"
	"go.temporal.io/sdk/client"
)

// newAzureCredentials returns Temporal Cloud client credentials backed by
// an Azure Key Vault secret. The Key Vault client uses the default Azure
// credential chain — when running on AKS with Workload Identity configured
// for the pod's service account, the chain reads the federated token from
// $AZURE_FEDERATED_TOKEN_FILE and exchanges it for short-lived AAD tokens.
// No client secret or certificate is needed in the container.
//
// The returned Credentials calls fetchSecret on every RPC, so re-versioning
// the secret in Key Vault propagates without restarting the worker. Cache
// with a TTL if your RPC volume makes per-call reads expensive.
//
// The Temporal SDK auto-enables TLS when Credentials are set (v1.39+);
// do NOT disable TLS via ConnectionOptions on the returned client.Options.
func newAzureCredentials(ctx context.Context, vaultURL, secretName string) (client.Credentials, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure: construct default credential (Workload Identity / managed identity / env): %w", err)
	}
	kv, err := azsecrets.NewClient(vaultURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("azure: construct key vault client for %q: %w", vaultURL, err)
	}

	fetchSecret := func(ctx context.Context) (string, error) {
		resp, err := kv.GetSecret(ctx, secretName, "", nil)
		if err != nil {
			return "", fmt.Errorf("azure: get secret %q from vault %q: %w", secretName, vaultURL, err)
		}
		if resp.Value == nil {
			return "", fmt.Errorf("azure: secret %q has no value", secretName)
		}
		return *resp.Value, nil
	}
	return client.NewAPIKeyDynamicCredentials(fetchSecret), nil
}
