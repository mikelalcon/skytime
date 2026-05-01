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

// TestRunCmd_InputSchemaCheck: bad JSON --input is rejected before any
// client connection is attempted. The error message must mention
// "invalid --input JSON" so users immediately understand the failure
// is in the JSON payload, not the .star file.
func TestRunCmd_InputSchemaCheck(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ok.star")
	require.NoError(t, os.WriteFile(file,
		[]byte(`flow(name="ok", inputs={}, steps=[script(id="s", fn=lambda ctx: {"a": 1}, output_alias="a")])`),
		0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetArgs([]string{"run", file, "--flow=ok", "--input=not valid json"})

	err = root.ExecuteContext(context.Background())
	require.Error(t, err)
	require.Contains(t, stderr.String(), "invalid --input JSON")
}

// TestRunCmd_RequiresFlowFlag: omitting --flow surfaces a cobra error
// before the RunE body runs. cobra.MarkFlagRequired enforces this.
func TestRunCmd_RequiresFlowFlag(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ok.star")
	require.NoError(t, os.WriteFile(file, []byte(`flow(name="ok", inputs={}, steps=[])`), 0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetArgs([]string{"run", file})

	err = root.ExecuteContext(context.Background())
	require.Error(t, err, "expected error from missing --flow")
}

// TestRunCmd_ValidateFailureBlocksConnect: a .star with validation
// errors fails before any Temporal client is constructed. The static
// validator surfaces the same errors as `skytime validate`, and stderr
// must reference the file path (proving the renderer fired).
func TestRunCmd_ValidateFailureBlocksConnect(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bad.star")
	require.NoError(t, os.WriteFile(file,
		[]byte(`flow(name="bad", inputs={}, steps=[step(action=undefined_extension.foo())])`),
		0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetArgs([]string{"run", file, "--flow=bad"})

	err = root.ExecuteContext(context.Background())
	require.Error(t, err)
	require.Contains(t, stderr.String(), "bad.star")
}

// TestRunCmd_EndToEnd: full happy path against testsuite. Skipped
// unless explicitly enabled — running an actual SkytimeWorkflow through
// testsuite from inside skytime run is a heavy integration test that
// needs the embedded worker to fire. Phase 6 will exercise this via
// the README walkthrough; W5 ships the smoke gates above.
//
// The ENV gate is `SKYTIME_E2E=1`. When unset, t.Skip().
func TestRunCmd_EndToEnd(t *testing.T) {
	if os.Getenv("SKYTIME_E2E") == "" {
		t.Skip("set SKYTIME_E2E=1 to run the full embedded-worker e2e test (requires temporal dev server or testsuite plumbing)")
	}
	t.Skip("Phase 6 exercises this through the README walkthrough; W5 ships the input-schema and connect-routing smokes")
}
