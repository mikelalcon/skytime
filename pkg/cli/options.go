package cli

import (
	"log/slog"

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
	exts        []extension.Extension
	credHandler extension.CredentialHandler

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
