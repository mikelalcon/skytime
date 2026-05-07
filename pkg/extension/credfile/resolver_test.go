package credfile_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/extension/credfile"
)

// writeCredfile is a test helper: drop a TOML file at a temp path with
// the given content + mode, return the path. Cleanup is automatic via t.TempDir.
func writeCredfile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".skytime-credentials")
	require.NoError(t, os.WriteFile(path, []byte(content), mode))
	return path
}

const happyPathTOML = `
[credentials.github_token]
type  = "bearer"
token = "test-pat"

[credentials.basic_id]
type     = "basic"
username = "alice"
password = "secret"

[credentials.apikey_id]
type  = "apikey"
key   = "X-API-Key"
value = "k-secret"
`

// ----------------------------------------------------------------------------
// Happy path — one test per credential kind to verify each sealed type is
// constructed correctly with secrets wrapped via extension.NewSecret.
// ----------------------------------------------------------------------------

func TestResolver_HappyPath_BearerCredential(t *testing.T) {
	path := writeCredfile(t, happyPathTOML, 0o600)
	r, err := credfile.New(credfile.WithPath(path))
	require.NoError(t, err)

	cred, err := r.Resolve(context.Background(), "github_token")
	require.NoError(t, err)

	bearer, ok := cred.(*extension.BearerCredential)
	require.True(t, ok, "expected *extension.BearerCredential, got %T", cred)
	assert.Equal(t, "github_token", bearer.ID())
	assert.Equal(t, "test-pat", bearer.Token.Reveal())
}

func TestResolver_HappyPath_BasicCredential(t *testing.T) {
	path := writeCredfile(t, happyPathTOML, 0o600)
	r, err := credfile.New(credfile.WithPath(path))
	require.NoError(t, err)

	cred, err := r.Resolve(context.Background(), "basic_id")
	require.NoError(t, err)

	basic, ok := cred.(*extension.BasicCredential)
	require.True(t, ok, "expected *extension.BasicCredential, got %T", cred)
	assert.Equal(t, "basic_id", basic.ID())
	assert.Equal(t, "alice", basic.User)
	assert.Equal(t, "secret", basic.Password.Reveal())
}

func TestResolver_HappyPath_APIKeyCredential(t *testing.T) {
	path := writeCredfile(t, happyPathTOML, 0o600)
	r, err := credfile.New(credfile.WithPath(path))
	require.NoError(t, err)

	cred, err := r.Resolve(context.Background(), "apikey_id")
	require.NoError(t, err)

	apikey, ok := cred.(*extension.APIKeyCredential)
	require.True(t, ok, "expected *extension.APIKeyCredential, got %T", cred)
	assert.Equal(t, "apikey_id", apikey.ID())
	assert.Equal(t, "X-API-Key", apikey.HeaderName)
	assert.Equal(t, "k-secret", apikey.Key.Reveal())
}

// ----------------------------------------------------------------------------
// Resolve-time errors.
// ----------------------------------------------------------------------------

// TestResolver_UnknownID_WrapsErrUnknownCredential pins the D2-12 retry
// classification contract: the activity (pkg/activity/classify.go:48) checks
// errors.Is(err, ErrUnknownCredential) to mark failures as NonRetryable.
func TestResolver_UnknownID_WrapsErrUnknownCredential(t *testing.T) {
	path := writeCredfile(t, happyPathTOML, 0o600)
	r, err := credfile.New(credfile.WithPath(path))
	require.NoError(t, err)

	_, err = r.Resolve(context.Background(), "no-such-id")
	require.Error(t, err)
	assert.True(t, errors.Is(err, extension.ErrUnknownCredential),
		"expected errors.Is(err, ErrUnknownCredential) to be true, got: %v", err)
	assert.Contains(t, err.Error(), "no-such-id")
	assert.Contains(t, err.Error(), path)
}

// ----------------------------------------------------------------------------
// New-time errors — IO + parse + validation.
// ----------------------------------------------------------------------------

func TestResolver_MissingFile_ReturnsPathError(t *testing.T) {
	_, err := credfile.New(credfile.WithPath("/no/such/file/at/all"))
	require.Error(t, err)
	var pe *fs.PathError
	assert.True(t, errors.As(err, &pe), "expected wrapped *fs.PathError, got: %v", err)
	assert.Contains(t, err.Error(), "stat /no/such/file/at/all")
}

// TestResolver_RejectsMalformed_TableDriven covers the malformed-input matrix
// — TOML parse failures plus per-entry validation. Each case asserts both
// a substring in the error message AND that the credential ID (or filename)
// surfaces so multi-entry files remain debuggable.
func TestResolver_RejectsMalformed_TableDriven(t *testing.T) {
	cases := []struct {
		name           string
		toml           string
		wantSubstrings []string
	}{
		{
			name:           "MalformedTOML",
			toml:           "not valid toml [[[",
			wantSubstrings: []string{"parse"},
		},
		{
			name: "MissingTypeField",
			toml: `
[credentials.no_type]
token = "x"
`,
			wantSubstrings: []string{"no_type", "type is required"},
		},
		{
			name: "UnknownType",
			toml: `
[credentials.weird]
type = "saml"
`,
			wantSubstrings: []string{"weird", `unknown type "saml"`},
		},
		{
			name: "BearerMissingToken",
			toml: `
[credentials.gh]
type = "bearer"
`,
			wantSubstrings: []string{"gh", "(bearer): token is required"},
		},
		{
			name: "BasicMissingUsername",
			toml: `
[credentials.svc]
type = "basic"
password = "x"
`,
			wantSubstrings: []string{"svc", "(basic): username and password are required"},
		},
		{
			name: "BasicMissingPassword",
			toml: `
[credentials.svc]
type = "basic"
username = "alice"
`,
			wantSubstrings: []string{"svc", "(basic): username and password are required"},
		},
		{
			name: "APIKeyMissingKey",
			toml: `
[credentials.partner]
type = "apikey"
value = "x"
`,
			wantSubstrings: []string{"partner", "(apikey): key (header name) and value (secret) are required"},
		},
		{
			name: "APIKeyMissingValue",
			toml: `
[credentials.partner]
type = "apikey"
key = "X-API-Key"
`,
			wantSubstrings: []string{"partner", "(apikey): key (header name) and value (secret) are required"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := writeCredfile(t, tt.toml, 0o600)
			_, err := credfile.New(credfile.WithPath(path))
			require.Error(t, err, "expected error for case %s", tt.name)
			// The file path should appear in every parse/validation error so
			// users can locate the offending file.
			assert.Contains(t, err.Error(), path,
				"error should include file path for attribution; got: %v", err)
			for _, want := range tt.wantSubstrings {
				assert.Contains(t, err.Error(), want,
					"case %s: expected substring %q in: %v", tt.name, want, err)
			}
		})
	}
}

// TestResolver_MissingTypeField_Errors is a discrete top-level case mirroring
// the table entry; preserved so `go test -run TestResolver_MissingTypeField_Errors`
// works as referenced in PLAN.md acceptance grep checks.
func TestResolver_MissingTypeField_Errors(t *testing.T) {
	const tomlContent = `
[credentials.no_type]
token = "x"
`
	path := writeCredfile(t, tomlContent, 0o600)
	_, err := credfile.New(credfile.WithPath(path))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_type")
	assert.Contains(t, err.Error(), "type is required")
}

// ----------------------------------------------------------------------------
// File-mode policy — POSIX-only, skipped on Windows per Pitfall 4.
// ----------------------------------------------------------------------------

func TestResolver_WorldReadable_WarnsByDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file-mode check is POSIX-only (see Pitfall 4)")
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	path := writeCredfile(t, happyPathTOML, 0o644) // world-readable on purpose

	r, err := credfile.New(
		credfile.WithPath(path),
		credfile.WithLogger(logger),
	)
	require.NoError(t, err, "warn-mode should accept the file")
	require.NotNil(t, r)

	logged := buf.String()
	assert.Contains(t, logged, "world/group-readable",
		"expected world/group-readable warning in slog output; got: %q", logged)
	assert.Contains(t, logged, path,
		"expected file path in warning; got: %q", logged)
}

func TestResolver_WorldReadable_WithStrictMode_Refuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file-mode check is POSIX-only (see Pitfall 4)")
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	path := writeCredfile(t, happyPathTOML, 0o644)

	_, err := credfile.New(
		credfile.WithPath(path),
		credfile.WithStrictMode(),
		credfile.WithLogger(logger),
	)
	require.Error(t, err, "strict mode should refuse world-readable files")
	assert.Contains(t, err.Error(), "world/group-readable")
	assert.Contains(t, err.Error(), path)
}

// ----------------------------------------------------------------------------
// Path resolution — default $HOME path + WithPath override.
// ----------------------------------------------------------------------------

func TestResolver_DefaultPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skytime-credentials")
	require.NoError(t, os.WriteFile(path, []byte(happyPathTOML), 0o600))

	// Override HOME so $HOME/.skytime-credentials resolves into our temp dir.
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		// os.UserHomeDir on Windows reads USERPROFILE first.
		t.Setenv("USERPROFILE", dir)
	}

	r, err := credfile.New() // NO opts — must default to $HOME/.skytime-credentials
	require.NoError(t, err)
	assert.Equal(t, path, r.Path())

	// Sanity: the resolver actually loaded credentials from this path.
	cred, err := r.Resolve(context.Background(), "github_token")
	require.NoError(t, err)
	assert.Equal(t, "github_token", cred.ID())
}

func TestResolver_WithPathOverrides(t *testing.T) {
	path := writeCredfile(t, happyPathTOML, 0o600)
	r, err := credfile.New(credfile.WithPath(path))
	require.NoError(t, err)
	assert.Equal(t, path, r.Path(), "WithPath should override default")
}

// TestResolver_WithPath_EmptyStringFallsBackToDefault verifies the
// "empty env → fallback" convenience documented on WithPath.
func TestResolver_WithPath_EmptyStringFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skytime-credentials")
	require.NoError(t, os.WriteFile(path, []byte(happyPathTOML), 0o600))

	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}

	r, err := credfile.New(credfile.WithPath("")) // empty → use default
	require.NoError(t, err)
	assert.Equal(t, path, r.Path())
}
