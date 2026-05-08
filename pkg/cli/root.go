// Package cli is the reusable cobra command tree for Skytime.
//
// pkg/cli is the ONLY library-side package permitted to import cobra,
// pflag, and charm.land/log/v2. The AST firewall in
// tests/firewall_cli_test.go enforces this; see D4-13 (Phase 4
// CONTEXT.md).
package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// NewRootCommand constructs the skytime root command with the validate,
// run, dev-temporal, server, info, and test subcommands wired. Options
// follow the per-instance pattern from D-07: no globals, no init() side
// effects.
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
		cfg.sdkLogger = buildSDKSlogLogger(cfg)
		return nil
	}

	root.AddCommand(newValidateCommand(cfg))
	root.AddCommand(newRunCommand(cfg))
	root.AddCommand(newDevTemporalCommand(cfg)) // renamed from dev-server in Phase 7 per D-07-21
	root.AddCommand(newServerCommand(cfg))      // NEW (Phase 7)
	root.AddCommand(newInfoCommand(cfg))        // Quick 260504-k9c
	root.AddCommand(newTestCommand(cfg))        // Phase 5 Plan 06

	return root, nil
}

// ErrAlreadyRendered is the exported alias of the package-private
// errSilent sentinel. RunE handlers in pkg/cli return errSilent when
// they have already written user-visible output to stderr; external
// callers (cmd/skytime/main.go) test for this via errors.Is to skip
// re-rendering. Exporting the alias keeps the lowercase name stable
// for in-package use while giving cmd/skytime a stable handle without
// leaking the lowercase identifier.
//
// See Quick 260504-jtr.
var ErrAlreadyRendered = errSilent

// RenderRootError formats a top-level cobra error to out with a
// human-friendly message and (where applicable) a Skytime-shaped
// suggestion plus the cobra usage block. Returns true iff something
// was written.
//
// Behavior:
//
//   - err == nil → returns false, writes nothing.
//   - errors.Is(err, ErrAlreadyRendered) → returns false, writes
//     nothing. The subcommand's RunE already printed its diagnostic
//     via render.go; main.go just needs the non-zero exit.
//   - err.Error() starts with "unknown command " → renders
//
//     Error: <err.Error()>
//
//     [optional] did you mean:
//     skytime run <arg>
//     skytime validate <arg>
//
//     <root.UsageString()>
//
//     The "did you mean" block fires when the offending arg ends in
//     ".star" (case-insensitive). Extract the arg by parsing the
//     fmt.Errorf format: `unknown command "X" for "skytime"...` —
//     the first quoted token is X.
//
//   - any other error → renders just `Error: <err.Error()>` plus the
//     usage block. Defensive default for future cobra error shapes
//     (e.g., "requires at least N args" if root ever gets RunE).
//
// The signature is (io.Writer, error) — NO *cobra.Command parameter —
// so callers (and tests) do not have to thread the live root in. The
// usage block is obtained by building a fresh root via NewRootCommand
// (no opts) and calling .UsageString(); cheap, and avoids leaking the
// live command tree across the API.
//
// See Quick 260504-jtr.
func RenderRootError(out io.Writer, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAlreadyRendered) {
		return false
	}

	msg := err.Error()
	fmt.Fprintf(out, "Error: %s\n", msg)

	// Detect cobra's "unknown command "<arg>" for "skytime"..." shape
	// and offer a `.star`-path suggestion when applicable.
	if strings.HasPrefix(msg, "unknown command ") {
		arg := extractFirstQuoted(msg)
		if arg != "" && strings.HasSuffix(strings.ToLower(arg), ".star") {
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "did you mean:")
			fmt.Fprintf(out, "  skytime run %s\n", arg)
			fmt.Fprintf(out, "  skytime validate %s\n", arg)
		}
	}

	// Append the usage block. Build a fresh, optionless root for the
	// template — sufficient because the usage block is identical
	// regardless of which Options the original was constructed with
	// (Options affect handler/extension wiring, not the cobra command
	// tree shape).
	fmt.Fprintln(out, "")
	if root, rootErr := NewRootCommand(); rootErr == nil {
		fmt.Fprint(out, root.UsageString())
	}
	return true
}

// extractFirstQuoted returns the first %q-style "..."-quoted substring
// in s, unquoted. Returns "" if no quoted token is found or if the
// quote is malformed. Used to recover the offending arg from cobra's
// `unknown command %q for %q...` error string. strconv.Unquote handles
// the standard %q escape rules.
func extractFirstQuoted(s string) string {
	i := strings.IndexByte(s, '"')
	if i < 0 {
		return ""
	}
	rest := s[i:]
	for j := 1; j < len(rest); j++ {
		if rest[j] == '\\' && j+1 < len(rest) {
			j++ // skip the escaped char
			continue
		}
		if rest[j] == '"' {
			if v, uErr := strconv.Unquote(rest[:j+1]); uErr == nil {
				return v
			}
			return ""
		}
	}
	return ""
}
