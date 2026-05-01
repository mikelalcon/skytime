package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/cli"
)

// TestValidateCmd_HappyPath: a minimal valid .star produces zero errors
// and the command exits cleanly with empty stderr.
func TestValidateCmd_HappyPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ok.star")
	require.NoError(t, os.WriteFile(file,
		[]byte(`flow(name="ok", inputs={}, steps=[script(id="s", fn=lambda ctx: {"a": 1}, output_alias="a")])`),
		0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"validate", file})

	err = root.ExecuteContext(context.Background())
	require.NoError(t, err)
	require.Empty(t, stderr.String(), "stderr should be empty on happy path; got: %s", stderr.String())
}

// TestValidateCmd_ExitNonZeroOnError: a .star referencing an undeclared
// extension fails; RunE returns non-nil; stderr contains the file path
// (proving the renderer fired with the typed *dag.ParseError shape).
func TestValidateCmd_ExitNonZeroOnError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bad.star")
	require.NoError(t, os.WriteFile(file,
		[]byte(`flow(name="bad", inputs={}, steps=[step(action=unknown_extension.foo())])`),
		0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"validate", file})

	err = root.ExecuteContext(context.Background())
	require.Error(t, err, "RunE must return error on validation failure")
	require.Contains(t, stderr.String(), file, "stderr should reference the file path")
}

// TestValidateCmd_UnknownExtensionHint: D4-16 — when the parser fails on
// an undefined name (which is how it reports unregistered extensions),
// the hint pointing to docs/cli-binary.md surfaces.
func TestValidateCmd_UnknownExtensionHint(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bad.star")
	require.NoError(t, os.WriteFile(file,
		[]byte(`flow(name="bad", inputs={}, steps=[step(action=github.create_issue())])`),
		0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"validate", file})

	_ = root.ExecuteContext(context.Background())
	// The hint is appended to stderr after the error rendering.
	require.Contains(t, stderr.String(), "docs/cli-binary.md",
		"expected D4-16 hint pointing at docs/cli-binary.md")
}
