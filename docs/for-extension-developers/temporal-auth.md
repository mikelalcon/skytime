# Temporal authentication — production credential rotation patterns

Production Temporal connections rotate credentials. API keys expire, mTLS certs roll, and cloud workloads obtain short-lived tokens from the platform's identity service (WIF on GCP, IRSA on AWS, Workload Identity on Azure). Long-lived static secrets in process memory are the anti-pattern. This page ships four working Go snippets you can paste into your binary that wires up `client.Credentials` with the right rotation cadence for your cluster + cloud combination — pick the one that matches your environment, fill in your identifiers, and ship.

## Cluster credentials vs application credentials

Skytime distinguishes two credential categories that are easy to conflate:

- **Cluster credentials** authenticate your worker / CLI to the Temporal cluster itself — the API key for Temporal Cloud, or the mTLS cert chain for a self-hosted cluster. These flow through `client.Options{Credentials: ...}` (Temporal Cloud, see `pkg/worker/client.go::NewCloudClient`) or `client.Options{ConnectionOptions: client.ConnectionOptions{TLS: ...}}` (self-hosted mTLS, see `pkg/worker/client.go::NewSelfHostedClient`). **This page is about cluster credentials.**

- **Application credentials** are the secrets your extensions resolve at activity time (GitHub tokens, internal-API keys, webhook signing secrets). These flow through `extension.CredentialHandler.Resolve(ctx, id)` and are wired into your binary via `cli.WithCredentialHandler(...)` or `cli.WithCredfile(...)` (see [`docs/for-extension-developers/README.md`](README.md) for the credfile schema). The default credfile path is `$HOME/.skytime-credentials`.

The two categories share no plumbing. Rotating your application-credential dotfile does NOT rotate the Temporal cluster connection, and vice versa.

## Why `client.Credentials` rotation matters

The Temporal Go SDK exposes two flavors of API-key credentials in `go.temporal.io/sdk/client`:

- `client.NewAPIKeyStaticCredentials(apiKey string)` — fixed string baked at construction. Suitable for short-running CLI invocations like `skytime run` where the process exits in seconds.
- `client.NewAPIKeyDynamicCredentials(func(ctx) (string, error))` — the SDK calls your closure before each RPC. The closure can re-read a Secret Manager / Key Vault entry, exchange a workload identity token for a fresh API key, or look up an in-memory cache. **This is the snippet shape this page demonstrates** for the three cloud sections.

For mTLS self-hosted clusters, rotation works differently — the cert chain lives in `client.Options.ConnectionOptions.TLS`, which the SDK reads once at `client.Dial` time. Reloading the cert requires re-dialing the client; the self-hosted section shows the SIGHUP-triggered re-dial pattern.

Across all four sections: secrets never enter workflow state (Temporal records workflow inputs and step outputs; the API key / cert chain is not exposed there), and there is no static API key kept in process memory after rotation.

## Pick your section

| Cloud | Cluster | Identity flow | Snippet |
|-------|---------|---------------|---------|
| GCP | Temporal Cloud | Workload Identity Federation → Google Secret Manager → API key | [GCP — Workload Identity Federation + Secret Manager](#gcp--workload-identity-federation--secret-manager) |
| AWS | Temporal Cloud | IRSA → AWS Secrets Manager → API key | [AWS — IRSA + Secrets Manager](#aws--irsa--secrets-manager) |
| Azure | Temporal Cloud | Azure Workload Identity → Key Vault → API key | [Azure — Workload Identity + Key Vault](#azure--workload-identity--key-vault) |
| Self-hosted | mTLS | Reload-on-SIGHUP cert refresh | [Self-hosted — mTLS with SIGHUP reload](#self-hosted--mtls-with-sighup-reload) |

Every snippet is a drop-in Go function. Paste it into your binary's package, call it from `main()` to construct your `client.Credentials` (or `client.Client` for the mTLS section), and pass the result into `client.Options`. See [`pkg/worker/client.go`](../../pkg/worker/client.go) for how skytime itself wires `client.Options{Credentials: ...}` for both Temporal Cloud and self-hosted mTLS.

## GCP — Workload Identity Federation + Secret Manager

> _Snippet body for AUTH-01 lands in Plan 07.5-02._

## AWS — IRSA + Secrets Manager

> _Snippet body for AUTH-02 lands in Plan 07.5-03._

## Azure — Workload Identity + Key Vault

> _Snippet body for AUTH-03 lands in Plan 07.5-04._

## Self-hosted — mTLS with SIGHUP reload

> _Snippet body for AUTH-04 lands in Plan 07.5-05._

## How the snippets are verified

The four `.go` files under [`snippets/`](snippets/) are the source of truth for the code blocks above. A drift-test (`snippets/drift_test.go`) reads this markdown, locates each `<!-- snippet: <name>.go -->` HTML-comment marker, extracts the body of the immediately-following <code>```go</code> fence, and asserts byte-equality (whitespace-trimmed) against the corresponding `.go` file. CI runs `go build ./...`, `go vet ./...`, and `go test ./...` in the snippets module on every push — if the markdown drifts from the source, the build fails.

Reader workflow: copy the snippet, paste it into your binary, fill in your identifiers, run.
