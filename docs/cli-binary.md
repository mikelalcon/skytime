# Building a Custom Skytime CLI Binary

The `skytime` binary distributed with this repo (`cmd/skytime/`) ships with
exactly one extension: a generic HTTP client. Real consultant flows
typically need more — GitHub, Slack, AWS, internal APIs. To register them
you build your own CLI binary by importing `pkg/cli` and calling
`cli.NewRootCommand` with your extension list.

This page is referenced by the `validate` subcommand's "unknown extension"
hint (D4-16) and by `cmd/skytime/main.go`'s `noopCredentialHandler.Resolve`
error message — both surface the path here when consultants need a custom
binary.

## Minimal Example

```go
// cmd/my-skytime/main.go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "syscall"

    "github.com/mikelalcon/skytime/pkg/cli"
    "github.com/mikelalcon/skytime/pkg/extension"
    skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"

    // Your own extensions:
    "github.com/your-org/skytime-extensions/github"
    "github.com/your-org/skytime-extensions/slack"
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    root, err := cli.NewRootCommand(
        cli.WithExtensions(
            skyhttp.New(),     // baked-in HTTP
            github.New(),      // your GitHub extension
            slack.New(),       // your Slack extension
        ),
        cli.WithCredentialHandler(myCredentialHandler{}),
    )
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    if err := root.ExecuteContext(ctx); err != nil {
        os.Exit(1)
    }
}

// myCredentialHandler resolves credential IDs to live secrets at the
// just-in-time activity boundary. Implementations typically read from a
// vault, env vars, or a config file.
type myCredentialHandler struct{}

func (myCredentialHandler) Resolve(ctx context.Context, id string) (extension.Credential, error) {
    // ... look up by id, build a *extension.BearerCredential / etc.
    return nil, fmt.Errorf("not implemented")
}
```

## Required Pieces

- **Extensions** — Each extension implements `extension.Extension`
  (`Name()`, `Initialize()`, `Operations()`). See `pkg/extension/extension.go`
  for the contract and `pkg/extension/builtin/http/http.go` for a complete
  reference implementation.
- **CredentialHandler** — Resolves credential IDs (the strings stored on
  workflow state via the `credential=` kwarg on extension factories) to
  live `extension.Credential` values just before each operation runs. The
  default `noopCredentialHandler` in `cmd/skytime/main.go` errors on every
  ID — production binaries override.

## Build & Use

```sh
go build -o my-skytime ./cmd/my-skytime
./my-skytime validate flows/my_flow.star
./my-skytime run flows/my_flow.star --flow=approve_pr --input='{"pr_id": 42}'
./my-skytime dev-server
```

## Build-time Identity (Optional)

`cmd/skytime/build_id.go` declares `var defaultBuildID = "dev"` so CI builds
can inject the commit SHA via:

```sh
go build -ldflags "-X main.defaultBuildID=$(git rev-parse HEAD)" ./cmd/my-skytime
```

This pattern (D3-20 / D4-13 mirrored) makes the binary's identity traceable
back to the source commit when it surfaces in Temporal worker Identity or
log lines.

## See Also

- `cmd/skytime/main.go` — the canonical wiring template
- `pkg/cli/root.go` — `NewRootCommand` and the persistent flag list
- `pkg/extension/builtin/http/http.go` — example extension implementation
