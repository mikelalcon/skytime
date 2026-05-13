// Package cli is the reusable cobra command tree for Skytime.
//
// # Build your own binary
//
// Custom Skytime binaries (the "build your own binary" pattern) compose
// extensions and credentials by chaining Options into NewRootCommand:
//
//	package main
//
//	import (
//	    "context"
//	    "fmt"
//	    "os"
//	    "os/signal"
//	    "syscall"
//
//	    "github.com/mikelalcon/skytime/pkg/cli"
//	    skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
//	    // ... extensions ...
//	)
//
//	func main() {
//	    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
//	    defer cancel()
//	    root, err := cli.NewRootCommand(
//	        cli.WithExtensions(skyhttp.New() /*, ... */),
//	        cli.WithCredfile(os.Getenv(cli.EnvCredfilePath)),
//	        // cli.WithBuildID("v1.43.0-abcdef"), // optional; defaults to worker.defaultBuildID
//	    )
//	    if err != nil {
//	        fmt.Fprintln(os.Stderr, err)
//	        os.Exit(1)
//	    }
//	    if err := root.ExecuteContext(ctx); err != nil {
//	        cli.RenderRootError(os.Stderr, err)
//	        os.Exit(1)
//	    }
//	}
//
// Subcommands inherited from cli.NewRootCommand (no custom additions
// required):
//
//	<bin> validate <file.star>      static validation (Tier 1)
//	<bin> run <file.star> ...       trigger a workflow against a Temporal cluster
//	<bin> dev-temporal              spawn a local Temporal dev server
//	<bin> server --rootdir=...      long-lived worker + HTTP receiver + dashboard
//	<bin> cron-plan --rootdir=...   one-shot cron Schedule diff (dry-run)
//	<bin> test <dir>                discover + run *_test.star (Tier 3)
//
// # Credentials
//
// Default location: $HOME/.skytime-credentials (TOML; see
// examples/http-github-webhook/.skytime-credentials.example for the
// schema). The full resolution chain (D-7.4-03) is, in order of
// precedence:
//
//  1. --credfile flag on `server` and `cron-plan`
//  2. SKYTIME_CREDFILE_PATH env var
//  3. cli.WithCredfile(path) explicit argument
//  4. Default $HOME/.skytime-credentials
//
// Lazy construction: cli.WithCredfile defers credfile.New() until the
// first credential is resolved. Binaries that never touch credentials
// (the public_repo_check headline demo, validate-only invocations,
// etc.) start cleanly even when no credfile exists on disk.
//
// For fully custom credential resolution (cloud secret managers, etc.),
// use cli.WithCredentialHandler(h) instead. The two options interact via
// last-wins semantics (D-7.4-02): whichever Option appears later in the
// NewRootCommand chain wins silently.
//
// # Build ID
//
// cli.WithBuildID(string) sets the worker's Temporal Build ID without
// the older `-ldflags "-X .../pkg/worker.defaultBuildID=$SHA"` pattern.
// Empty (or absent) preserves the default; production deployers
// typically pass the git SHA at build time. See pkg/worker for the
// underlying WorkerOptions.BuildID field and applyDefaults fallback.
//
// # Implementation notes
//
// pkg/cli is the ONLY library-side package permitted to import
// github.com/spf13/cobra, github.com/spf13/pflag, and
// charm.land/log/v2. The AST firewall in tests/firewall_cli_test.go
// gates this — see D4-13 (Phase 4 CONTEXT.md). Charm-log was renamed
// upstream from github.com/charmbracelet/log/v2 to charm.land/log/v2;
// the firewall forbidden list and this doc track the new module path.
package cli
