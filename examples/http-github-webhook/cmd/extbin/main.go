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
//	extbin dev-temporal              spawn a local Temporal dev server (renamed in Phase 7 per D-07-21)
//	extbin server                    long-lived worker (Phase 7+)
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
		// already rendered (validate, run, dev-temporal, server, test) return
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
// multiple goroutines per heartbeat boundary; a mutex + initialized
// flag protect the lazy initialization. SetCredfilePath is the pre-init
// path override hook used by pkg/cli/server.go's --credfile flag.
type lazyCredfileHandler struct {
	mu sync.Mutex
	// path is the override captured at construction or via SetCredfilePath.
	// Empty string means "use credfile.New's default" ($HOME/.skytime-credentials).
	path        string
	initialized bool
	inner       *credfile.Resolver
	initErr     error
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
	h.mu.Lock()
	if !h.initialized {
		opts := []credfile.Option{}
		if h.path != "" {
			opts = append(opts, credfile.WithPath(h.path))
		}
		h.inner, h.initErr = credfile.New(opts...)
		h.initialized = true
	}
	inner, initErr := h.inner, h.initErr
	h.mu.Unlock()

	if initErr != nil {
		return nil, fmt.Errorf(
			"extbin: load credfile (set SKYTIME_CREDFILE_PATH or copy .skytime-credentials.example to ~/.skytime-credentials): %w",
			initErr)
	}
	return inner.Resolve(ctx, id)
}

// SetCredfilePath overrides the credfile path captured at construction.
// Must be called before any Resolve(); returns an error if the handler
// has already initialized its underlying resolver. This is the hook
// pkg/cli/server.go's --credfile flag uses to push a runtime path
// override into the handler.
func (h *lazyCredfileHandler) SetCredfilePath(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.initialized {
		return fmt.Errorf("credfile handler already initialized; --credfile must be set before first Resolve")
	}
	h.path = path
	return nil
}

// Compile-time interface check.
var _ extension.CredentialHandler = (*lazyCredfileHandler)(nil)
