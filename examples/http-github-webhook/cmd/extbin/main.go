// extbin is the custom Skytime CLI binary for the http-github-webhook
// example. It demonstrates the canonical "build your own binary" pattern
// (docs/cli-binary.md) by wiring three extensions (HTTP + GitHub +
// Webhook) and a credfile-backed credential resolver into the same
// pkg/cli root command tree that cmd/skytime uses.
//
// Subcommands inherited from pkg/cli (no custom additions):
//
//	extbin validate <file.star>      static validation (Tier 1)
//	extbin run <file.star> ...       trigger a workflow against a Temporal cluster
//	extbin dev-server                spawn a local Temporal dev server
//	extbin test <dir>                discover + run *_test.star (Tier 3)
//
// Credentials live at $HOME/.skytime-credentials (TOML; see
// examples/http-github-webhook/.skytime-credentials.example for the
// schema). Override via SKYTIME_CREDFILE_PATH if the file lives elsewhere.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/mikelalcon/skytime/pkg/cli"
	"github.com/mikelalcon/skytime/pkg/extension"
	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
	"github.com/mikelalcon/skytime/pkg/extension/credfile"

	skygh "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github"
	skyweb "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/webhook"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	root, err := cli.NewRootCommand(
		cli.WithExtensions(
			skyhttp.New(),
			skygh.New(),
			skyweb.New(),
		),
		cli.WithCredentialHandler(newLazyCredfileHandler(os.Getenv("SKYTIME_CREDFILE_PATH"))),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := root.ExecuteContext(ctx); err != nil {
		// Per D4-18, pkg/cli owns user-visible output. Subcommands that
		// already rendered (validate, run, dev-server, test) return
		// cli.ErrAlreadyRendered and RenderRootError no-ops on it.
		// Top-level cobra errors (unknown command, etc.) reach
		// RenderRootError as plain errors and get a human-friendly
		// stderr render here — same shape as cmd/skytime/main.go.
		cli.RenderRootError(os.Stderr, err)
		os.Exit(1)
	}
}

// lazyCredfileHandler defers credfile.New() to the first Resolve() call.
//
// Why: the headline demo (`extbin run public_repo_check.star`) uses the
// public GitHub API and never resolves any credential. If the reader
// hasn't created ~/.skytime-credentials yet, eager construction would
// fail at startup with a confusing "stat: no such file" before any
// command runs. Lazy construction means that startup ALWAYS succeeds;
// the credfile is read only when a flow actually references a
// credential — at which point the user is expecting credfile to be
// configured.
//
// Concurrency: pkg/activity/execute_batch.go invokes Resolve() from
// multiple goroutines per heartbeat boundary. sync.Once + a cached
// pointer + cached error protect the lazy initialization.
type lazyCredfileHandler struct {
	// path is the override captured at construction. Empty string means
	// "use credfile.New's default" ($HOME/.skytime-credentials).
	path string

	once       sync.Once
	once_inner *credfile.Resolver
	once_err   error
}

// newLazyCredfileHandler captures pathOverride for later credfile.New().
// pathOverride may be empty: WithPath("") falls back to the default per
// pkg/extension/credfile/options.go's documented contract.
func newLazyCredfileHandler(pathOverride string) *lazyCredfileHandler {
	return &lazyCredfileHandler{path: pathOverride}
}

// Resolve implements extension.CredentialHandler. The first call
// constructs the underlying credfile.Resolver; subsequent calls reuse
// it. Construction errors (missing file, parse error, world-readable
// under strict mode) are cached and returned on every subsequent call
// so the surfaced message is stable across goroutines.
func (h *lazyCredfileHandler) Resolve(ctx context.Context, id string) (extension.Credential, error) {
	h.once.Do(func() {
		opts := []credfile.Option{}
		if h.path != "" {
			opts = append(opts, credfile.WithPath(h.path))
		}
		r, err := credfile.New(opts...)
		h.once_inner = r
		h.once_err = err
	})
	if h.once_err != nil {
		return nil, fmt.Errorf(
			"extbin: load credfile (set SKYTIME_CREDFILE_PATH or copy .skytime-credentials.example to ~/.skytime-credentials): %w",
			h.once_err)
	}
	return h.once_inner.Resolve(ctx, id)
}

// Compile-time interface check.
var _ extension.CredentialHandler = (*lazyCredfileHandler)(nil)
