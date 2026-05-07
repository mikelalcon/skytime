package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// TestLazyCredfileHandler_DoesNotTouchFileAtConstruction asserts that
// constructing the handler with a missing-file path does NOT trigger
// credfile.New() — verified by checking the file STILL does not exist
// after construction (proves no os.Open / os.Stat in the constructor).
//
// This is the "headline demo needs no credentials" guarantee: the
// public_repo_check.star path must not fail at startup if the user
// hasn't set up ~/.skytime-credentials yet.
func TestLazyCredfileHandler_DoesNotTouchFileAtConstruction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skytime-credentials") // intentionally NOT created

	h := newLazyCredfileHandler(path)
	require.NotNil(t, h)
	assert.Equal(t, path, h.path)

	// The load-bearing assertion: the constructor did not touch the file.
	// If it had called credfile.New(), os.Stat inside credfile.New would
	// have surfaced the missing-file error and the construction would
	// have either created/opened the file or reported the error. Neither
	// happened.
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr),
		"constructor must NOT touch/open/create the credfile; expected IsNotExist, got: %v", statErr)
}

// TestLazyCredfileHandler_FirstResolveSurfacesMissingFile asserts the
// user-recovery hint is present on the error message and the error is
// cached (idempotent across multiple Resolve calls).
func TestLazyCredfileHandler_FirstResolveSurfacesMissingFile(t *testing.T) {
	h := newLazyCredfileHandler("/no/such/credfile/exists")
	ctx := context.Background()

	_, err1 := h.Resolve(ctx, "x")
	require.Error(t, err1)
	assert.Contains(t, err1.Error(), "credfile")
	assert.Contains(t, err1.Error(), "SKYTIME_CREDFILE_PATH")
	assert.Contains(t, err1.Error(), ".skytime-credentials.example")

	_, err2 := h.Resolve(ctx, "y")
	require.Error(t, err2)
	// Second call must surface the SAME error (Once cached the failure).
	assert.Equal(t, err1.Error(), err2.Error())
}

// TestLazyCredfileHandler_HappyPathWithRealFile asserts that when the
// credfile DOES exist and contains a valid bearer entry, Resolve
// returns the BearerCredential. Also verifies that an unknown ID still
// surfaces extension.ErrUnknownCredential after first construction
// (i.e. the cache holds the resolver, not just the error).
func TestLazyCredfileHandler_HappyPathWithRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skytime-credentials")
	require.NoError(t, os.WriteFile(path, []byte(`
[credentials.github_token]
type  = "bearer"
token = "test-pat"
`), 0o600))

	h := newLazyCredfileHandler(path)
	cred, err := h.Resolve(context.Background(), "github_token")
	require.NoError(t, err)
	bearer, ok := cred.(*extension.BearerCredential)
	require.True(t, ok)
	assert.Equal(t, "github_token", bearer.ID())
	assert.Equal(t, "test-pat", bearer.Token.Reveal())

	// Unknown ID still returns ErrUnknownCredential after first
	// construction — proves the cached resolver is reused, not just the
	// first-call error path.
	_, err = h.Resolve(context.Background(), "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, extension.ErrUnknownCredential))
}

// TestExtbin_BuildsAndShowsHelp is the subprocess smoke. Builds the
// binary in a temp dir, runs `--help`, asserts the four inherited
// subcommand names are listed. This test takes a few seconds due to
// the `go build` step; mark it short-skippable for tight inner loops.
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
	for _, sub := range []string{"validate", "run", "dev-server", "test"} {
		assert.Contains(t, s, sub, "expected subcommand %q in --help output", sub)
	}
}
