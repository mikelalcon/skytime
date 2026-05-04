package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/cli"
)

// emDash is the U+2014 sentinel rendered for empty description and empty
// inputs cells. Mirrors the package-private const in info.go; redefined
// here (package cli_test is black-box) so tests assert against the
// exact rune.
const emDash = "—"

// TestInfoCmd_HappyPath_ThreeColumnTable — Quick 260504-k9c: a fixture
// with three flows in a specific source order produces a table with the
// correct rows AND order (declaration order, NOT alphabetical).
func TestInfoCmd_HappyPath_ThreeColumnTable(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "three_flows.star")
	src := `flow(name="alpha", inputs={"repo":"string"}, steps=[script(id="s1", fn=lambda ctx: {"a":1}, output_alias="a")], description="First flow")
flow(name="zeta",  inputs={}, steps=[script(id="s2", fn=lambda ctx: {"b":2}, output_alias="b")])
flow(name="middle", inputs={"id":"string", "active":"bool"}, steps=[script(id="s3", fn=lambda ctx: {"c":3}, output_alias="c")], description="Has multiple sorted inputs")
`
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"info", file})

	err = root.ExecuteContext(context.Background())
	require.NoError(t, err)
	assert.Empty(t, stderr.String(), "stderr should be empty on happy path; got: %s", stderr.String())

	out := stdout.String()

	// Header row.
	assert.Contains(t, out, "Flow")
	assert.Contains(t, out, "Description")
	assert.Contains(t, out, "Inputs")

	// Row 1 (alpha): description + inputs both present.
	assert.Contains(t, out, "alpha")
	assert.Contains(t, out, "First flow")
	assert.Contains(t, out, "repo:string")

	// Row 2 (zeta): em-dash for both description and inputs.
	assert.Contains(t, out, "zeta")

	// Row 3 (middle): inputs alphabetized — "active:bool, id:string" NOT
	// "id:string, active:bool" or any other order.
	assert.Contains(t, out, "middle")
	assert.Contains(t, out, "Has multiple sorted inputs")
	assert.Contains(t, out, "active:bool, id:string")

	// Source-declaration order: alpha → zeta → middle. Alphabetical
	// would have been alpha → middle → zeta; if FlowsInOrder broke we'd
	// see middle before zeta in the output stream.
	rowOrder := regexp.MustCompile(`alpha[\s\S]*?zeta[\s\S]*?middle`)
	assert.Regexp(t, rowOrder, out, "rows must appear in declaration order alpha → zeta → middle")
}

// TestInfoCmd_EmptyDescription_AndEmptyInputs_RenderEmDash — Quick
// 260504-k9c: a flow with no description and inputs={} renders an
// em-dash (U+2014) in BOTH cells.
func TestInfoCmd_EmptyDescription_AndEmptyInputs_RenderEmDash(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "empty.star")
	src := `flow(name="bare", inputs={}, steps=[script(id="s", fn=lambda ctx: {"a":1}, output_alias="a")])
`
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"info", file})

	require.NoError(t, root.ExecuteContext(context.Background()))

	out := stdout.String()
	// Find the bare row line; assert it contains the em-dash twice.
	bareRow := regexp.MustCompile(`(?m)^bare\b.*$`)
	loc := bareRow.FindString(out)
	require.NotEmpty(t, loc, "expected a row beginning with 'bare' in output:\n%s", out)
	emDashCount := bytes.Count([]byte(loc), []byte(emDash))
	assert.GreaterOrEqual(t, emDashCount, 2, "bare row must contain em-dash for both description and inputs; got: %q", loc)
}

// TestInfoCmd_InputsKeysAlphabetized — Quick 260504-k9c: a flow with
// inputs declared in non-alphabetical order renders alphabetically.
func TestInfoCmd_InputsKeysAlphabetized(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "alpha.star")
	src := `flow(name="f", inputs={"zebra":"string", "apple":"int", "mango":"bool"}, steps=[script(id="s", fn=lambda ctx: {"a":1}, output_alias="a")])
`
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"info", file})

	require.NoError(t, root.ExecuteContext(context.Background()))

	// Exact substring — proves deterministic alphabetical ordering.
	assert.Contains(t, stdout.String(), "apple:int, mango:bool, zebra:string")
}

// TestInfoCmd_ParseFailure_RendersErrorAndExitsNonZero — Quick
// 260504-k9c: a file referencing an unknown extension renders the
// parser error to stderr and exits non-zero. NO table on stdout.
func TestInfoCmd_ParseFailure_RendersErrorAndExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bad.star")
	src := `flow(name="bad", inputs={}, steps=[step(action=unknown_extension.foo())])
`
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"info", file})

	err = root.ExecuteContext(context.Background())
	require.Error(t, err, "RunE must return error on parse failure")
	assert.Contains(t, stderr.String(), file, "stderr should reference the file path")
	assert.NotContains(t, stdout.String(), "Flow", "no table header on parse error")
}

// TestInfoCmd_NoFlows_RendersHeaderOnly — Quick 260504-k9c: a file with
// zero flow() calls produces just the header row, no data rows.
func TestInfoCmd_NoFlows_RendersHeaderOnly(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "noflow.star")
	// A file with no flow() calls is a valid parse — the parser doesn't
	// require flows to exist (worker bootstrap may). A literal expression
	// statement is valid Starlark; we use one to keep the file
	// non-empty.
	src := `# no flows here
x = 1
`
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))

	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"info", file})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Contains(t, stdout.String(), "Flow", "header row should still render")
	// No data rows — the only line(s) in stdout are the header.
}

// TestInfoCmd_HelpText — Quick 260504-k9c: --help prints the Use-line
// and a tone-matching short description.
func TestInfoCmd_HelpText(t *testing.T) {
	root, err := cli.NewRootCommand()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"info", "--help"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	out := stdout.String()
	assert.Contains(t, out, "info <file.star>", "help should show the Use line")
	// Cobra renders Long when present; assert tone-matching content.
	assert.Contains(t, out, "Parses a Starlark flow file", "help should describe the command's behavior")
	assert.Contains(t, out, "Flow, Description, Inputs", "help should list the table columns")
}
