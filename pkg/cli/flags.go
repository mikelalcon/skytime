package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// registerPersistentFlags adds D4-08 flags to the root command's
// PersistentFlags so every subcommand inherits them. Pointer-bound
// vars on cfg keep the flow value-of-truth uniform: cobra writes the
// flag, PersistentPreRunE may overwrite from env vars, the subcommand
// reads cfg.
func registerPersistentFlags(root *cobra.Command, cfg *config) {
	root.PersistentFlags().BoolVar(&cfg.debug, "debug", false,
		"reveal Go internals in error output (VAL-03 / D4-19)")
	root.PersistentFlags().StringVar(&cfg.address, "address", "",
		"Temporal address (env: SKYTIME_TEMPORAL_ADDRESS)")
	root.PersistentFlags().StringVar(&cfg.namespace, "namespace", "",
		"Temporal namespace (env: SKYTIME_TEMPORAL_NAMESPACE)")
	root.PersistentFlags().StringVar(&cfg.apiKey, "api-key", "",
		"Temporal Cloud API key (env: SKYTIME_TEMPORAL_API_KEY)")
	root.PersistentFlags().StringVar(&cfg.clientCert, "client-cert", "",
		"mTLS client cert file (env: SKYTIME_TEMPORAL_CLIENT_CERT)")
	root.PersistentFlags().StringVar(&cfg.clientKey, "client-key", "",
		"mTLS client key file (env: SKYTIME_TEMPORAL_CLIENT_KEY)")
	root.PersistentFlags().StringVar(&cfg.serverCA, "server-ca", "",
		"mTLS server CA file (env: SKYTIME_TEMPORAL_SERVER_CA)")
}

// envBindings is the (flag-name, env-var, target) table consumed by
// bindEnvVars. Keeping the list table-driven makes it trivial to add a
// new persistent flag in lockstep with its env fallback.
type envBinding struct {
	flag   string
	envVar string
	target *string
}

// bindEnvVars fills cfg fields from SKYTIME_TEMPORAL_* env vars when
// the corresponding flag was NOT supplied on the command line. cobra
// exposes Flag().Changed for this — when it is false, the user did not
// pass --flag, and the env var (if non-empty) wins. When the user did
// pass the flag, we MUST NOT overwrite it.
//
// Only string flags participate; --debug is bool and has no env var.
func bindEnvVars(cmd *cobra.Command, cfg *config) {
	bindings := []envBinding{
		{"address", "SKYTIME_TEMPORAL_ADDRESS", &cfg.address},
		{"namespace", "SKYTIME_TEMPORAL_NAMESPACE", &cfg.namespace},
		{"api-key", "SKYTIME_TEMPORAL_API_KEY", &cfg.apiKey},
		{"client-cert", "SKYTIME_TEMPORAL_CLIENT_CERT", &cfg.clientCert},
		{"client-key", "SKYTIME_TEMPORAL_CLIENT_KEY", &cfg.clientKey},
		{"server-ca", "SKYTIME_TEMPORAL_SERVER_CA", &cfg.serverCA},
	}
	for _, b := range bindings {
		f := cmd.PersistentFlags().Lookup(b.flag)
		if f == nil || f.Changed {
			continue
		}
		if v := os.Getenv(b.envVar); v != "" {
			*b.target = v
		}
	}
}
