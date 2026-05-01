package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/cli"
)

// TestRootCommand_FlagsRegistered asserts every D4-08 persistent flag
// is registered on the root command. The exact names are load-bearing
// for the env-var binding pattern in flags.go.
func TestRootCommand_FlagsRegistered(t *testing.T) {
	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	expected := []string{
		"debug",
		"address",
		"namespace",
		"api-key",
		"client-cert",
		"client-key",
		"server-ca",
	}
	for _, name := range expected {
		flag := root.PersistentFlags().Lookup(name)
		require.NotNil(t, flag, "expected persistent flag --%s registered on root", name)
	}
}

// TestRootCommand_HasValidateSubcommand asserts skytime has a validate
// subcommand. Run + dev-server are added in W4 (plans 04-05 / 04-06).
func TestRootCommand_HasValidateSubcommand(t *testing.T) {
	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	found := false
	for _, sub := range root.Commands() {
		if sub.Name() == "validate" {
			found = true
			break
		}
	}
	require.True(t, found, "expected validate subcommand on root")
}

// TestRootCommand_SilencesErrorsAndUsage verifies D4-18: cobra's
// built-in error printing is disabled so the renderer owns output.
func TestRootCommand_SilencesErrorsAndUsage(t *testing.T) {
	root, err := cli.NewRootCommand()
	require.NoError(t, err)
	require.True(t, root.SilenceErrors, "SilenceErrors must be true (D4-18)")
	require.True(t, root.SilenceUsage, "SilenceUsage must be true (D4-18)")
}
