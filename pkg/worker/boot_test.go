package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// trivialFlowSrc is a parse-clean .star file with one flow + one inline
// script (no extensions required). Used across boot tests.
const trivialFlowSrc = `flow(
    name="trivial",
    steps=[
        script(id="bump", fn=lambda ctx: {"x": 1}, output_alias="x_plus_one"),
    ],
)
`

const trivialFlowSrc2 = `flow(
    name="trivial2",
    steps=[
        script(id="noop", fn=lambda ctx: {"y": 2}, output_alias="y_doubled"),
    ],
)
`

// writeStarFile writes content to dir/name and returns the absolute path.
func writeStarFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	return abs
}

// ---------------------------------------------------------------------------
// bootRegistry
// ---------------------------------------------------------------------------

func TestBootRegistry_ParsesAllStarFiles(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)
	writeStarFile(t, dir, "b.star", trivialFlowSrc2)

	reg, err := bootRegistry(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, reg)

	// Both flows should be registered with their own content_hash.
	hash1, ok := reg.ContentHashFor("trivial")
	require.True(t, ok, "trivial flow must be registered")
	require.NotEmpty(t, hash1)

	hash2, ok := reg.ContentHashFor("trivial2")
	require.True(t, ok, "trivial2 flow must be registered")
	require.NotEmpty(t, hash2)

	assert.NotEqual(t, hash1, hash2, "different .star files have different content_hashes")
}

func TestBootRegistry_ContentHashIsSha256OfFileBytes(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)

	reg, err := bootRegistry(dir, nil)
	require.NoError(t, err)

	gotHash, ok := reg.ContentHashFor("trivial")
	require.True(t, ok)

	expectedSum := sha256.Sum256([]byte(trivialFlowSrc))
	expectedHash := hex.EncodeToString(expectedSum[:])
	assert.Equal(t, expectedHash, gotHash, "content_hash must equal sha256(fileBytes)")
}

func TestBootRegistry_RegistryFrozenAfterBoot(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)

	reg, err := bootRegistry(dir, nil)
	require.NoError(t, err)

	err = reg.Register("anything", "h", &interpreter.ParsedFlow{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, interpreter.ErrRegistryFrozen), "registry must be frozen after boot")
}

func TestBootRegistry_FailsOnUnparseable(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)
	writeStarFile(t, dir, "z_broken.star", `this is not valid starlark @@@@@@`)

	_, err := bootRegistry(dir, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "z_broken.star", "error must mention the offending file")
}

func TestBootRegistry_EmptyDirReturnsEmptyRegistry(t *testing.T) {
	dir := t.TempDir()
	// No .star files written.

	reg, err := bootRegistry(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, reg)

	// Frozen + empty: any Lookup miss + Register fails with ErrRegistryFrozen.
	_, ok := reg.Lookup("nonexistent", "h")
	assert.False(t, ok)
	err = reg.Register("late", "h", &interpreter.ParsedFlow{})
	assert.True(t, errors.Is(err, interpreter.ErrRegistryFrozen))
}

func TestBootRegistry_IgnoresNonStarFiles(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("docs"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("notes"), 0644))

	reg, err := bootRegistry(dir, nil)
	require.NoError(t, err)
	_, ok := reg.ContentHashFor("trivial")
	assert.True(t, ok)
}

func TestBootRegistry_RecursesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	require.NoError(t, os.MkdirAll(sub, 0755))
	writeStarFile(t, sub, "sub.star", trivialFlowSrc)

	reg, err := bootRegistry(dir, nil)
	require.NoError(t, err)
	_, ok := reg.ContentHashFor("trivial")
	assert.True(t, ok, "bootRegistry must walk subdirectories")
}

func TestBootRegistry_RootDirRequired(t *testing.T) {
	_, err := bootRegistry("", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rootDir")
}

func TestBootRegistry_RootDirMissing(t *testing.T) {
	_, err := bootRegistry("/nonexistent/dir/that/should/not/exist", nil)
	require.Error(t, err)
}
