package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	testingpkg "github.com/mikelalcon/skytime/pkg/testing"
)

// newTestCommand returns the skytime test subcommand.
//
// CLI-03: discovers *_test.star files in <dir> recursively, runs the
// Tier-3 harness, reports pass/fail with Starlark-callsite errors. NO
// Go stack traces in default output (D5-E1, D5-F3, CLI-03 explicit).
//
// Flags:
//
//	--run <regex>      Filter tests by `<file_basename>.<test_name>` (D5-E3).
//	--format human|json  Output format. Default human (D5-E1); json mirrors
//	                     cmd/test2json schema (D5-E2).
//
// Inherits the persistent --debug flag from root (D4-19); when set,
// RunE writes a brief failure-counts diagnostic to stderr after the
// human output.
//
// Exit code mapping (D5-E4):
//
//	failed == 0, err == nil  → exit 0 (RunE returns nil)
//	failed > 0               → exit 1 (RunE returns errSilent)
//	err != nil               → exit 1 (RunE prints err to stderr, returns errSilent)
//	bad arg count            → exit 2 (cobra ExactArgs(1) before RunE)
func newTestCommand(cfg *config) *cobra.Command {
	var runPattern string
	var format string

	cmd := &cobra.Command{
		Use:   "test <dir>",
		Short: "Run Tier-3 .star tests in <dir>",
		Long: "Discovers *_test.star files in <dir> recursively, runs each def test_*() " +
			"function under the Tier-3 harness (testsuite.TestWorkflowEnvironment + " +
			"Starlark mocks), and reports pass/fail. Errors point at Starlark callsites " +
			"(file:line:col); --debug surfaces a brief failure-counts diagnostic on stderr.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]

			opts := []testingpkg.Option{
				testingpkg.WithExtensions(cfg.exts...),
				testingpkg.WithOutput(cmd.OutOrStdout()),
			}
			if runPattern != "" {
				opts = append(opts, testingpkg.WithRunFilter(runPattern))
			}
			if format != "" && format != "human" {
				opts = append(opts, testingpkg.WithFormat(format))
			}

			passed, failed, err := testingpkg.RunCLI(dir, opts...)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "skytime test: %s\n", err.Error())
				return errSilent
			}
			// D5-E4: exit 1 on any failure.
			if failed > 0 {
				if cfg.debug {
					fmt.Fprintf(cmd.ErrOrStderr(), "skytime test: %d failed, %d passed\n", failed, passed)
				}
				return errSilent
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&runPattern, "run", "",
		"Filter tests by Go-regex against `<file_basename_without_ext>.<test_name>` (e.g. ^users_test\\.test_existing)")
	cmd.Flags().StringVar(&format, "format", "human",
		"Output format: human (default; --- PASS / --- FAIL lines) or json (cmd/test2json mirror)")

	return cmd
}
