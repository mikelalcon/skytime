package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// TestCredfileHandler_DoesNotTouchFileAtConstruction asserts that
// constructing the handler with a missing-file path does NOT trigger
// credfile.New() — verified by checking the file STILL does not exist
// after construction (proves no os.Open / os.Stat in the constructor).
//
// This is the "headline demo needs no credentials" guarantee: the
// public_repo_check.star path must not fail at startup if the user
// hasn't set up ~/.skytime-credentials yet. Migrated from
// examples/http-github-webhook/cmd/extbin/main_test.go (D-7.4-15).
func TestCredfileHandler_DoesNotTouchFileAtConstruction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skytime-credentials") // intentionally NOT created

	h := newCredfileHandler(path)
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

// TestCredfileHandler_FirstResolveSurfacesMissingFile asserts the
// user-recovery hint is present on the error message and the error is
// cached (idempotent across multiple Resolve calls). Migrated from
// extbin/main_test.go (D-7.4-15).
func TestCredfileHandler_FirstResolveSurfacesMissingFile(t *testing.T) {
	// Ensure env var doesn't leak from the host shell into this test.
	t.Setenv(EnvCredfilePath, "")

	h := newCredfileHandler("/no/such/credfile/exists")
	ctx := context.Background()

	_, err1 := h.Resolve(ctx, "x")
	require.Error(t, err1)
	assert.Contains(t, err1.Error(), "credfile")
	assert.Contains(t, err1.Error(), "SKYTIME_CREDFILE_PATH")
	assert.Contains(t, err1.Error(), ".skytime-credentials.example")

	_, err2 := h.Resolve(ctx, "y")
	require.Error(t, err2)
	// Second call must surface the SAME error (cache holds the failure).
	assert.Equal(t, err1.Error(), err2.Error())
}

// TestCredfileHandler_HappyPathWithRealFile asserts that when the
// credfile DOES exist and contains a valid bearer entry, Resolve
// returns the BearerCredential. Also verifies that an unknown ID still
// surfaces extension.ErrUnknownCredential after first construction
// (i.e. the cache holds the resolver, not just the error). Migrated
// from extbin/main_test.go (D-7.4-15).
func TestCredfileHandler_HappyPathWithRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skytime-credentials")
	require.NoError(t, os.WriteFile(path, []byte(`
[credentials.github_token]
type  = "bearer"
token = "test-pat"
`), 0o600))

	h := newCredfileHandler(path)
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

// TestCredfileHandler_SetCredfilePath_OverridesBeforeInit asserts that
// SetCredfilePath replaces the construction-time path when called
// before any Resolve(). Resolve then uses the new path. Migrated from
// extbin/main_test.go (D-7.4-15).
func TestCredfileHandler_SetCredfilePath_OverridesBeforeInit(t *testing.T) {
	dir := t.TempDir()
	overridePath := filepath.Join(dir, ".skytime-credentials")
	require.NoError(t, os.WriteFile(overridePath, []byte(`
[credentials.github_token]
type  = "bearer"
token = "override-pat"
`), 0o600))

	h := newCredfileHandler("/no/such/path")
	require.NoError(t, h.SetCredfilePath(overridePath))

	cred, err := h.Resolve(context.Background(), "github_token")
	require.NoError(t, err)
	bearer, ok := cred.(*extension.BearerCredential)
	require.True(t, ok)
	assert.Equal(t, "override-pat", bearer.Token.Reveal())
}

// TestCredfileHandler_SetCredfilePath_ErrorsAfterInit asserts that
// SetCredfilePath refuses to mutate the path once Resolve has fired the
// underlying resolver. This guards against silent split-brain (early
// callers see one credfile, later callers see another). Migrated from
// extbin/main_test.go (D-7.4-15).
func TestCredfileHandler_SetCredfilePath_ErrorsAfterInit(t *testing.T) {
	h := newCredfileHandler("/no/such/path")
	_, _ = h.Resolve(context.Background(), "x") // fires the init

	err := h.SetCredfilePath("/some/other/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already initialized")
}

// TestWithCredfile_LazyConstruction pins CLI-08 success criterion 1:
// credfile.New() is NOT called at WithCredfile / NewRootCommand time.
func TestWithCredfile_LazyConstruction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skytime-credentials") // intentionally NOT created

	h := newCredfileHandler(path)
	require.NotNil(t, h)

	// The constructor must NOT touch the file.
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr),
		"newCredfileHandler must NOT touch/open the credfile; got: %v", statErr)

	// Now trigger the lazy init.
	_, err := h.Resolve(context.Background(), "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credfile")
}

// TestWithCredfile_EmptyPath_UsesEnvVar pins D-7.4-03: when WithCredfile("")
// is passed, SKYTIME_CREDFILE_PATH env var is consulted at Resolve() time.
func TestWithCredfile_EmptyPath_UsesEnvVar(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".skytime-credentials")
	require.NoError(t, os.WriteFile(envPath, []byte(`
[credentials.github_token]
type  = "bearer"
token = "env-pat"
`), 0o600))

	t.Setenv(EnvCredfilePath, envPath)
	h := newCredfileHandler("") // empty → env var fallback
	cred, err := h.Resolve(context.Background(), "github_token")
	require.NoError(t, err)
	bearer, ok := cred.(*extension.BearerCredential)
	require.True(t, ok)
	assert.Equal(t, "env-pat", bearer.Token.Reveal())
}

// TestWithCredfile_ExplicitWinsOverEnv pins D-7.4-01: explicit arg beats env var.
func TestWithCredfile_ExplicitWinsOverEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env-creds")
	require.NoError(t, os.WriteFile(envPath, []byte(`
[credentials.github_token]
type  = "bearer"
token = "env-pat"
`), 0o600))
	explicitPath := filepath.Join(dir, "explicit-creds")
	require.NoError(t, os.WriteFile(explicitPath, []byte(`
[credentials.github_token]
type  = "bearer"
token = "explicit-pat"
`), 0o600))

	t.Setenv(EnvCredfilePath, envPath)
	h := newCredfileHandler(explicitPath) // explicit wins
	cred, err := h.Resolve(context.Background(), "github_token")
	require.NoError(t, err)
	bearer := cred.(*extension.BearerCredential)
	assert.Equal(t, "explicit-pat", bearer.Token.Reveal(),
		"explicit WithCredfile arg must beat SKYTIME_CREDFILE_PATH env var")
}

// TestWithCredfile_LastWinsWithWithCredentialHandler pins D-7.4-02:
// WithCredfile and WithCredentialHandler in the same chain — last call wins silently.
func TestWithCredfile_LastWinsWithWithCredentialHandler(t *testing.T) {
	type customH struct{ extension.CredentialHandler }
	custom := customH{}

	cfg1 := &config{}
	require.NoError(t, WithCredfile("/a")(cfg1))
	require.NoError(t, WithCredentialHandler(custom)(cfg1))
	assert.Equal(t, extension.CredentialHandler(custom), cfg1.credHandler,
		"WithCredentialHandler called LAST must win over earlier WithCredfile")

	cfg2 := &config{}
	require.NoError(t, WithCredentialHandler(custom)(cfg2))
	require.NoError(t, WithCredfile("/a")(cfg2))
	_, isCredfile := cfg2.credHandler.(*credfileHandler)
	assert.True(t, isCredfile,
		"WithCredfile called LAST must win over earlier WithCredentialHandler")
}

// TestApplyCredfileFlag_EmptyValue_NoOp pins the short-circuit that lets
// server.go and cron_plan.go call applyCredfileFlag unconditionally
// (without an outer if credfilePath != "" guard).
func TestApplyCredfileFlag_EmptyValue_NoOp(t *testing.T) {
	cfg := &config{credHandler: nil}
	require.NoError(t, applyCredfileFlag(cfg, ""),
		"empty flagValue must short-circuit even with nil handler")
}

// TestApplyCredfileFlag_NilHandler_FriendlyError pins the error message
// the user sees when --credfile is passed against a binary that wasn't
// built with WithCredfile / WithCredentialHandler.
func TestApplyCredfileFlag_NilHandler_FriendlyError(t *testing.T) {
	cfg := &config{credHandler: nil}
	err := applyCredfileFlag(cfg, "/some/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/some/path")
	assert.Contains(t, err.Error(), "cli.WithCredfile or cli.WithCredentialHandler")
}

// TestApplyCredfileFlag_NonSetterHandler_FriendlyError pins the error path
// for a fully-custom WithCredentialHandler that doesn't implement
// SetCredfilePath. The friendly error tells the user to either rebuild
// with cli.WithCredfile or implement the setter.
func TestApplyCredfileFlag_NonSetterHandler_FriendlyError(t *testing.T) {
	type customH struct{ extension.CredentialHandler }
	cfg := &config{credHandler: customH{}}
	err := applyCredfileFlag(cfg, "/some/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/some/path")
	assert.Contains(t, err.Error(), "rebuild with cli.WithCredfile")
}

// TestApplyCredfileFlag_SetterError_Wrapped pins the error wrapping
// when SetCredfilePath itself returns an error (e.g., the handler has
// already initialized). The caller sees "--credfile=...: already initialized".
func TestApplyCredfileFlag_SetterError_Wrapped(t *testing.T) {
	h := newCredfileHandler("/no/such/path")
	// Force initialization so the next SetCredfilePath returns an error.
	_, _ = h.Resolve(context.Background(), "x")

	cfg := &config{credHandler: h}
	err := applyCredfileFlag(cfg, "/new/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--credfile=/new/path")
	assert.Contains(t, err.Error(), "already initialized")
}

// TestApplyCredfileFlag_HappyPath pins the success path: the handler's
// internal path is updated, and the next Resolve reads the new file.
func TestApplyCredfileFlag_HappyPath(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "new-creds")
	require.NoError(t, os.WriteFile(newPath, []byte(`
[credentials.github_token]
type  = "bearer"
token = "applied-pat"
`), 0o600))

	cfg := &config{}
	require.NoError(t, WithCredfile("")(cfg)) // installs a credfileHandler with empty path
	require.NoError(t, applyCredfileFlag(cfg, newPath))

	h, ok := cfg.credHandler.(*credfileHandler)
	require.True(t, ok, "credHandler should be *credfileHandler after WithCredfile install")
	assert.Equal(t, newPath, h.path, "applyCredfileFlag should have updated h.path")

	cred, err := h.Resolve(context.Background(), "github_token")
	require.NoError(t, err)
	bearer := cred.(*extension.BearerCredential)
	assert.Equal(t, "applied-pat", bearer.Token.Reveal())
}
