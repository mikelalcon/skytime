// skytime is the Skytime CLI binary: validate, run, dev-temporal,
// server, cron-plan, info, and test subcommands wired against the
// baked-in HTTP extension and the core extension (Skytime-native
// trigger primitives — cron in 7.2; future signal/queue). Consultants
// who need additional extensions should build their own binary by
// importing pkg/cli — see docs/cli-binary.md.
//
// The binary is intentionally thin per D4-13 — pkg/cli owns the cobra
// tree and the renderer; cmd/skytime is the wiring + the CredentialHandler
// choice. Adding a new subcommand should not require editing this file.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mikelalcon/skytime/pkg/cli"
	"github.com/mikelalcon/skytime/pkg/extension"
	skycore "github.com/mikelalcon/skytime/pkg/extension/builtin/core"
	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
)

func main() {
	// defaultBuildID is referenced so the ldflags injection target stays
	// alive against `unused` linters. Future surfaces (--version,
	// Temporal Identity) can consume it without re-introducing the
	// variable.
	_ = defaultBuildID

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	root, err := cli.NewRootCommand(
		cli.WithExtensions(skyhttp.New(), skycore.New()),
		cli.WithCredentialHandler(noopCredentialHandler{}),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := root.ExecuteContext(ctx); err != nil {
		// Per D4-18, pkg/cli owns user-visible output. Subcommands that
		// already rendered (validate, run, dev-temporal, server) return
		// cli.ErrAlreadyRendered and RenderRootError no-ops on it. Top-
		// level cobra errors (unknown command, etc.) reach
		// RenderRootError as plain errors and get a human-friendly
		// stderr render here — see Quick 260504-jtr for the motivating
		// `skytime path/to/flow.star ...` silent-exit-1 bug.
		cli.RenderRootError(os.Stderr, err)
		os.Exit(1)
	}
}

// noopCredentialHandler is the default for the baked-in skytime binary.
// Returns an error for every ID so any flow attempting to use a
// credential surfaces a clear "no resolver configured" message pointing
// at docs/cli-binary.md. Consultants building a custom binary supply a
// real handler.
type noopCredentialHandler struct{}

// Resolve always returns an error — this default is intentional.
// `skytime validate` never invokes it (parse-only); `skytime run` only
// invokes the resolver if a flow actually references a credential. The
// error message points consultants at the documented escape hatch.
func (noopCredentialHandler) Resolve(_ context.Context, id string) (extension.Credential, error) {
	return nil, fmt.Errorf(
		"no credential resolver configured: build a custom Skytime binary that registers a CredentialHandler — see docs/cli-binary.md (id=%q)",
		id)
}

// Compile-time check.
var _ extension.CredentialHandler = noopCredentialHandler{}
