package cli

import (
	"log/slog"

	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// Option configures the command tree at construction time. Mirrors
// pkg/parser's Option style and pkg/worker's WorkerOptions ergonomics:
// each Option is a closure that mutates a *config and may return an
// error so option-time validation can short-circuit.
type Option func(*config) error

// config is the per-instance state shared between root and subcommands.
// PersistentPreRunE populates the runtime fields (logger, debug, the
// flag values reflecting env-var fallbacks); subcommand RunEs read them.
//
// Construction-time fields (set via Option) are immutable after
// NewRootCommand returns. Runtime fields are populated when cobra
// invokes PersistentPreRunE — i.e., once per Execute call.
type config struct {
	// Construction-time fields (set via Option):
	exts            []extension.Extension
	credHandler     extension.CredentialHandler
	scheduleFactory scheduleClientFactory // NEW (Phase 7.2 Plan 03) — test seam for ScheduleClient injection

	// PersistentPreRunE-populated runtime fields:
	debug      bool
	Verbose    bool         // exposed (capitalized) for white-box tests; persistent --verbose flag value
	logger     *slog.Logger // charm-log handler used for Skytime-side logging (slog.Default after PreRunE)
	sdkLogger  *slog.Logger // SDK-side handler — verbose=false → near-silent text/discard; verbose=true → same charm-log handler
	address    string
	namespace  string
	apiKey     string
	clientCert string
	clientKey  string
	serverCA   string
}

// WithExtensions registers extensions used by validate (and, after the
// later W4 plans land, run). Mirrors parser.WithExtensions and
// worker.WithExtensions: variadic, append-only, no error path.
func WithExtensions(exts ...extension.Extension) Option {
	return func(c *config) error {
		c.exts = append(c.exts, exts...)
		return nil
	}
}

// WithCredentialHandler wires the JIT resolver into skytime run's
// embedded worker. Phase 4 W3 (validate) does not invoke the handler;
// it is stored for API symmetry with the run subcommand (W4) and with
// pkg/validator's identical option.
func WithCredentialHandler(h extension.CredentialHandler) Option {
	return func(c *config) error {
		c.credHandler = h
		return nil
	}
}

// scheduleClientFactory is the package-private test seam for overriding
// how the cron reconciler obtains a ScheduleClient from the Temporal
// client. Production callers don't pass this option; tests use it to
// inject a fake ScheduleClient that records Create / Update / Delete
// calls.
//
// Per-call scope: this Option configures a single NewRootCommand
// invocation. Mirrors worker.WithSDKFactory's per-call isolation
// (Phase 7.1 D-7.1-13).
type scheduleClientFactory func(c client.Client) client.ScheduleClient

// WithScheduleClientFactory overrides how `skytime server
// --cron-reconcile` (and `skytime cron-plan`) obtain a ScheduleClient.
// Production: omit this option; the reconciler calls c.ScheduleClient()
// on the live Temporal client. Tests inject a FakeScheduleClient
// (pkg/extension/schedules.NewFakeScheduleClient) so the signal-loop
// tests run without a real Temporal cluster.
//
// Symmetric to worker.WithSDKFactory but lives in pkg/cli — the schedule
// client lifecycle is owned by the CLI subcommand, not the worker
// subsystem.
func WithScheduleClientFactory(f func(c client.Client) client.ScheduleClient) Option {
	return func(c *config) error {
		c.scheduleFactory = f
		return nil
	}
}

// WithCredfile installs a lazy credfile-backed CredentialHandler — the
// convenience option for the dotfile-backed credfile pattern (CLI-08).
// Path resolution order (D-7.4-03 — KEEP THIS PRECEDENCE EXACTLY):
//
//  1. SetCredfilePath(path) — the --credfile flag's setter hook
//     (applied via applyCredfileFlag in credfile.go before first Resolve())
//  2. WithCredfile(path)    — the explicit option argument
//  3. SKYTIME_CREDFILE_PATH — env var fallback when (1) and (2) are empty
//  4. credfile defaultPath  — $HOME/.skytime-credentials (D-7.4-05 LOCKED)
//
// Lazy construction (CLI-08 success criterion 1): credfile.New() is NOT
// called at NewRootCommand time — the underlying file is opened on first
// Resolve(). The headline demo (extbin run public_repo_check.star) uses
// the public GitHub API and never resolves any credential — eager
// construction would fail at startup with "stat: no such file" before
// any subcommand runs.
//
// Last-wins (D-7.4-02): If WithCredentialHandler also appears in the
// same NewRootCommand chain, whichever Option is invoked later wins
// silently. This is the standard Go option idiom.
//
// The handler implementation (struct, constructor, Resolve, SetCredfilePath,
// applyCredfileFlag helper) lives in pkg/cli/credfile.go — this declaration
// is intentionally placed here per D-7.4-04 LOCKED so the Option surface is
// discoverable in one file alongside WithExtensions / WithCredentialHandler /
// WithScheduleClientFactory.
func WithCredfile(path string) Option {
	return func(c *config) error {
		c.credHandler = newCredfileHandler(path)
		return nil
	}
}
