// Package cli — credfile.go
//
// Implementation of the credfile-backed CredentialHandler that
// pkg/cli/options.go's WithCredfile Option installs. Per D-7.4-04
// LOCKED, this file holds the IMPLEMENTATION (struct, constructor,
// methods, helper) — the WithCredfile DECLARATION lives in options.go
// alongside the other Option functions for surface discoverability.
//
// Resolution chain (D-7.4-03 — KEEP THIS PRECEDENCE EXACTLY):
//
//  1. SetCredfilePath(path) — the --credfile flag's setter hook
//  2. WithCredfile(path)    — the explicit option argument
//  3. SKYTIME_CREDFILE_PATH — env var fallback when (1) and (2) are empty
//  4. credfile defaultPath  — $HOME/.skytime-credentials (D-7.4-05 LOCKED)
//
// Per-subcommand --credfile boilerplate (D-7.4-03 spirit): the
// applyCredfileFlag helper at the bottom of this file owns the
// type-assertion + friendly-error fallback ONCE. server.go and
// cron_plan.go each shrink to a single 3-line call site instead of
// duplicating ~9 lines of inline plumbing.

package cli

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/extension/credfile"
)

// EnvCredfilePath is the env-var name that overrides the WithCredfile
// argument when the argument is empty. Exported for test/discovery; the
// resolution chain order is defined above.
const EnvCredfilePath = "SKYTIME_CREDFILE_PATH"

// credfileHandler is the lifted lazyCredfileHandler (CLI-08). Same
// concurrency model as extbin's original: sync.Mutex + initialized bool +
// cached (inner, initErr). First Resolve() triggers credfile.New(opts);
// subsequent calls reuse the cached resolver (or surface the cached
// initErr for stable cross-goroutine error messages).
type credfileHandler struct {
	mu          sync.Mutex
	path        string // captured via WithCredfile arg OR SetCredfilePath; "" means "consult env/default at init"
	initialized bool
	inner       *credfile.Resolver
	initErr     error
}

// newCredfileHandler captures the construction-time path. Empty means
// "consult SKYTIME_CREDFILE_PATH then defaultPath at first Resolve()".
// Called from WithCredfile in options.go (D-7.4-04 LOCKED split).
func newCredfileHandler(pathOverride string) *credfileHandler {
	return &credfileHandler{path: pathOverride}
}

// Resolve implements extension.CredentialHandler. First call constructs
// the underlying credfile.Resolver per the documented resolution chain;
// subsequent calls reuse the cached (resolver, error) pair.
func (h *credfileHandler) Resolve(ctx context.Context, id string) (extension.Credential, error) {
	h.mu.Lock()
	if !h.initialized {
		// Resolve the path per D-7.4-03 (precedence):
		// h.path was set via SetCredfilePath (--credfile flag) OR via
		// WithCredfile(arg). If still empty, consult env var. If still
		// empty, leave it so credfile.New uses defaultPath().
		effectivePath := h.path
		if effectivePath == "" {
			effectivePath = os.Getenv(EnvCredfilePath)
		}

		opts := []credfile.Option{}
		if effectivePath != "" {
			opts = append(opts, credfile.WithPath(effectivePath))
		}
		h.inner, h.initErr = credfile.New(opts...)
		h.initialized = true
	}
	inner, initErr := h.inner, h.initErr
	h.mu.Unlock()

	if initErr != nil {
		return nil, fmt.Errorf(
			"cli: load credfile (set %s or copy .skytime-credentials.example to ~/.skytime-credentials): %w",
			EnvCredfilePath, initErr)
	}
	return inner.Resolve(ctx, id)
}

// SetCredfilePath overrides the construction-time path. Must be called
// before any Resolve(); returns an error if the handler has already
// initialized its underlying resolver. This is the hook
// applyCredfileFlag invokes when --credfile is set on the command line.
//
// D-7.4-03: this is the ONLY runtime path-override surface. Subcommands
// no longer type-assert against an anonymous interface inline; they call
// applyCredfileFlag below, which encapsulates the type-assertion + the
// friendly-error fallback for the fully-custom WithCredentialHandler case.
func (h *credfileHandler) SetCredfilePath(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.initialized {
		return fmt.Errorf("credfile handler already initialized; --credfile must be set before first Resolve")
	}
	h.path = path
	return nil
}

// Compile-time interface check.
var _ extension.CredentialHandler = (*credfileHandler)(nil)

// applyCredfileFlag pushes a --credfile flag value into the option-installed
// CredentialHandler. Called by server.go and cron_plan.go in their RunE blocks
// before any Resolve() can fire. Owns the entire type-assertion + friendly-error
// surface that used to be duplicated ~9 lines per subcommand (D-7.4-03 spirit:
// no scattered SetCredfilePath plumbing).
//
// Three error paths surface user-facing messages:
//   - cfg.credHandler == nil: binary was built without WithCredfile or
//     WithCredentialHandler. Tell the user to add one of those at compile time.
//   - cfg.credHandler does not implement SetCredfilePath: binary uses a
//     fully-custom WithCredentialHandler that doesn't expose runtime path
//     override. Tell the user to either rebuild with cli.WithCredfile(...) for
//     the dotfile pattern, or implement SetCredfilePath on the custom handler.
//   - SetCredfilePath returns an error (e.g., "already initialized"): wrap
//     with the --credfile=... prefix so the failure points back at the flag.
//
// On success, the next Resolve() on cfg.credHandler will use the new path.
func applyCredfileFlag(cfg *config, flagValue string) error {
	if flagValue == "" {
		return nil // nothing to apply; resolution chain falls through to env/option/default
	}
	if cfg.credHandler == nil {
		return fmt.Errorf("--credfile=%s requires the binary to be built with cli.WithCredfile or cli.WithCredentialHandler (see docs/cli-binary.md)", flagValue)
	}
	setter, ok := cfg.credHandler.(interface {
		SetCredfilePath(string) error
	})
	if !ok {
		return fmt.Errorf("--credfile=%s: this binary's credential handler does not support runtime path overrides; rebuild with cli.WithCredfile(...) for the dotfile pattern, or implement SetCredfilePath(string) error on the custom handler", flagValue)
	}
	if err := setter.SetCredfilePath(flagValue); err != nil {
		return fmt.Errorf("--credfile=%s: %w", flagValue, err)
	}
	return nil
}
