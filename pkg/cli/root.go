// Package cli is the reusable cobra command tree for Skytime.
//
// pkg/cli is the ONLY library-side package permitted to import cobra,
// pflag, and charm.land/log/v2. The AST firewall in
// tests/firewall_cli_test.go enforces this; see D4-13 (Phase 4
// CONTEXT.md).
package cli

import (
	"github.com/spf13/cobra"
)

// NewRootCommand constructs the skytime root command with the validate,
// run, and dev-server subcommands wired. Options follow the
// per-instance pattern from D-07: no globals, no init() side effects.
//
// Returns (*cobra.Command, error) so option failures (e.g., a
// misconfigured handler) surface explicitly to the caller.
//
// SilenceErrors and SilenceUsage are TRUE so the renderer
// (pkg/cli/render.go) owns error output — D4-18 requires Starlark-first
// formatting, which would conflict with cobra's default error/usage
// dump.
func NewRootCommand(opts ...Option) (*cobra.Command, error) {
	cfg := &config{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	root := &cobra.Command{
		Use:           "skytime",
		Short:         "Starlark-defined durable workflows on Temporal",
		SilenceErrors: true, // D4-18: WE render errors
		SilenceUsage:  true, // D4-18: no usage dump on validation errors
	}

	registerPersistentFlags(root, cfg)

	// PersistentPreRunE chain: env-var binding → init slog handler.
	// Inherited by every subcommand so initialization order is
	// identical regardless of which subcommand runs.
	//
	// setupLogging lives in render.go (Task 2 of plan 04-04).
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		bindEnvVars(cmd, cfg)
		cfg.logger = setupLogging(cfg.debug)
		return nil
	}

	root.AddCommand(newValidateCommand(cfg))
	root.AddCommand(newRunCommand(cfg))
	root.AddCommand(newDevServerCommand(cfg))

	return root, nil
}
