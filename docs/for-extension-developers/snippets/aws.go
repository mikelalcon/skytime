package snippets

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"go.temporal.io/sdk/client"
)

// newAWSCredentials returns Temporal Cloud client credentials backed by an
// AWS Secrets Manager secret. The Secrets Manager client uses the default
// AWS credential chain — when running on EKS with IRSA (IAM Roles for
// Service Accounts) configured for the pod, the chain reads the projected
// service-account token at $AWS_WEB_IDENTITY_TOKEN_FILE and exchanges it
// for short-lived STS credentials. No long-lived access keys are needed
// in the container.
//
// The returned Credentials calls fetchSecret on every RPC, so rotating
// the secret value in Secrets Manager (via PutSecretValue or scheduled
// rotation) propagates without restarting the worker. Cache with a TTL
// if your RPC volume makes per-call reads expensive.
//
// The Temporal SDK auto-enables TLS when Credentials are set (v1.39+);
// do NOT disable TLS via ConnectionOptions on the returned client.Options.
func newAWSCredentials(ctx context.Context, region, secretID string) (client.Credentials, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("aws: load default config (IRSA / instance role / env): %w", err)
	}
	sm := secretsmanager.NewFromConfig(cfg)

	fetchSecret := func(ctx context.Context) (string, error) {
		out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
			SecretId: aws.String(secretID),
		})
		if err != nil {
			return "", fmt.Errorf("aws: get secret %q in region %q: %w", secretID, region, err)
		}
		if out.SecretString == nil {
			return "", fmt.Errorf("aws: secret %q has no SecretString (binary secret?)", secretID)
		}
		return *out.SecretString, nil
	}
	return client.NewAPIKeyDynamicCredentials(fetchSecret), nil
}
