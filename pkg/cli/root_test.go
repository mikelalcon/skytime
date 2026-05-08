package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// executeRootCapture is the shared helper for Quick 260504-jtr regression
// tests: build a fresh root, set buffered stdout/stderr, run with the
// supplied args, return the captured streams plus the ExecuteContext
// error. Mirrors validate_test.go's pattern but factored for reuse.
func executeRootCapture(t *testing.T, args []string) (stdout, stderr bytes.Buffer, runErr error) {
	t.Helper()

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	runErr = root.ExecuteContext(context.Background())
	return stdout, stderr, runErr
}

// TestRootCommand_BareInvocationPrintsHelp pins the existing-correct
// behavior: bare `skytime` invocation prints the help block on stdout
// and exits 0. This is cobra's default-when-no-RunE-and-no-subcommand
// branch — the Quick 260504-jtr fix MUST NOT alter it.
func TestRootCommand_BareInvocationPrintsHelp(t *testing.T) {
	stdout, stderr, err := executeRootCapture(t, []string{})
	require.NoError(t, err, "bare invocation must exit 0")
	require.Contains(t, stdout.String(), "Available Commands:", "help block should land on stdout")
	require.Contains(t, stdout.String(), "validate")
	require.Contains(t, stdout.String(), "run")
	require.Contains(t, stdout.String(), "dev-server")
	require.Empty(t, stderr.String(), "bare invocation must not write to stderr")
}

// TestRootCommand_UnknownCommandRendersError exercises the Quick
// 260504-jtr fix on a non-`.star` unknown subcommand. ExecuteContext
// returns the cobra-built error; RenderRootError formats it onto
// stderr with the `Error:` prefix and the cobra usage block, but no
// `did you mean` suggestion (the arg has no `.star` suffix).
func TestRootCommand_UnknownCommandRendersError(t *testing.T) {
	_, _, err := executeRootCapture(t, []string{"nonexistent"})
	require.Error(t, err, "ExecuteContext must return cobra's unknown-command error")

	var rendered bytes.Buffer
	require.True(t, cli.RenderRootError(&rendered, err), "RenderRootError must report it rendered")

	got := rendered.String()
	require.Contains(t, got, `Error: unknown command "nonexistent" for "skytime"`,
		"stderr must surface the cobra error verbatim under the Error: prefix")
	require.Contains(t, got, "Usage:", "cobra usage block must accompany the error")
	require.Contains(t, got, "Available Commands:", "usage block must list available commands")
	require.NotContains(t, got, "did you mean",
		"non-.star args must NOT trigger the run/validate suggestion line")
}

// TestRootCommand_UnknownCommandStarFileSuggestsRun exercises the
// motivating bug from the Quick 260504-jtr brief: a consultant types
// `skytime path/to/flow.star ...` (Pythonic muscle memory). The
// suggestion block must point them at `skytime run` AND
// `skytime validate` so they pick the right subcommand.
func TestRootCommand_UnknownCommandStarFileSuggestsRun(t *testing.T) {
	_, _, err := executeRootCapture(t, []string{"examples/foo.star", "--flow", "x"})
	require.Error(t, err, "ExecuteContext must return cobra's unknown-command error for the .star arg")

	var rendered bytes.Buffer
	require.True(t, cli.RenderRootError(&rendered, err))

	got := rendered.String()
	require.Contains(t, got, `Error: unknown command "examples/foo.star" for "skytime"`,
		"the offending positional arg must appear in the rendered error")
	require.Contains(t, got, "did you mean", "a .star arg must trigger the run/validate suggestion line")
	require.Contains(t, got, "skytime run examples/foo.star",
		"`skytime run <file>` must appear as a candidate")
	require.Contains(t, got, "skytime validate examples/foo.star",
		"`skytime validate <file>` must appear as a candidate")
	require.Contains(t, got, "Usage:", "the usage block must follow the suggestion lines")
}

// TestRootCommand_RenderRootErrorRespectsSilentSentinel verifies that
// the helper does NOT double-render an error chain that the subcommand's
// renderer already wrote. validate / run RunE handlers return
// errSilent (re-exported as cli.ErrAlreadyRendered) on already-rendered
// failures; main.go must not append a second usage block.
func TestRootCommand_RenderRootErrorRespectsSilentSentinel(t *testing.T) {
	t.Run("DirectSentinel", func(t *testing.T) {
		var buf bytes.Buffer
		require.False(t, cli.RenderRootError(&buf, cli.ErrAlreadyRendered),
			"RenderRootError must report it skipped")
		require.Empty(t, buf.String(), "no bytes must be written for ErrAlreadyRendered")
	})

	t.Run("WrappedSentinel", func(t *testing.T) {
		wrapped := fmt.Errorf("wrap: %w", cli.ErrAlreadyRendered)
		require.True(t, errors.Is(wrapped, cli.ErrAlreadyRendered),
			"errors.Is must traverse the wrap chain")

		var buf bytes.Buffer
		require.False(t, cli.RenderRootError(&buf, wrapped))
		require.Empty(t, buf.String(),
			"wrapping must not defeat the skip — %s",
			buf.String())
	})

	t.Run("NilError", func(t *testing.T) {
		var buf bytes.Buffer
		require.False(t, cli.RenderRootError(&buf, nil))
		require.Empty(t, buf.String())
	})
}

// TestRootCommand_ValidSubcommandUnaffected pins that adding the
// RenderRootError helper does not regress the validate happy path.
// This duplicates a sliver of TestValidateCmd_HappyPath (in
// validate_test.go) intentionally — the duplication keeps a regression
// signal proximate to the helper's source.
func TestRootCommand_ValidSubcommandUnaffected(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ok.star")
	require.NoError(t, os.WriteFile(file,
		[]byte(`flow(name="ok", inputs={}, steps=[script(id="s", fn=lambda ctx: {"a": 1}, output_alias="a")])`),
		0o644))

	stdout, stderr, err := executeRootCapture(t, []string{"validate", file})
	require.NoError(t, err, "valid .star must exit 0")
	require.Empty(t, stderr.String(),
		"validate happy path stderr must remain empty; got: %s", stderr.String())
	_ = stdout // help text is not relevant on a happy validate run
}

// TestRoot_HasServerSubcommand asserts skytime has a server subcommand
// (Phase 7 Plan 05). The presence test pins the registration line in
// pkg/cli/root.go so a regression that drops the AddCommand call surfaces
// here rather than at runtime.
func TestRoot_HasServerSubcommand(t *testing.T) {
	root, err := cli.NewRootCommand()
	require.NoError(t, err)
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "server" {
			found = true
			break
		}
	}
	require.True(t, found, "skytime server subcommand must be registered on root")
}
