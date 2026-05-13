package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtbin_BuildsAndShowsHelp is the subprocess smoke. Builds the
// binary in a temp dir, runs `--help`, asserts the four inherited
// subcommand names are listed. This test takes a few seconds due to
// the `go build` step; mark it short-skippable for tight inner loops.
//
// Per D-7.4-15 the lazy-credfile-handler tests have moved to
// pkg/cli/credfile_test.go (alongside the lifted handler implementation).
// What remains here is extbin-specific subprocess wiring — the help
// subcommand list is the single user-visible behavior unique to this
// binary's compose-and-build shape.
func TestExtbin_BuildsAndShowsHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess build smoke in -short")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "extbin")

	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	require.NoError(t, build.Run(), "go build failed for extbin")

	out, err := exec.Command(bin, "--help").CombinedOutput()
	require.NoError(t, err, "--help should exit 0; output: %s", string(out))

	s := string(out)
	for _, sub := range []string{"validate", "run", "dev-temporal", "server", "test"} {
		assert.Contains(t, s, sub, "expected subcommand %q in --help output", sub)
	}
}
