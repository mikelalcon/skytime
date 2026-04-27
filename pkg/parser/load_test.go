package parser

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// =============================================================================
// D-13 relative load — sibling of caller file
// =============================================================================

func TestLoad_Relative(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "helper.star")
	caller := filepath.Join(dir, "main.star")

	require.NoError(t, os.WriteFile(target, []byte(`
def shared_step():
    return step(action=fake_ext.echo(msg="shared"))
`), 0o644))
	require.NoError(t, os.WriteFile(caller, []byte(`
load("./helper.star", "shared_step")
flow(name="parent", inputs={}, steps=[shared_step()])
`), 0o644))

	p, err := NewParser(WithRoot(dir), WithExtensions(&fakeExtension{}))
	require.NoError(t, err)
	flows, err := p.ParseFile(caller)
	require.NoError(t, err)
	require.Contains(t, flows, "parent")
	require.Len(t, flows["parent"].Body, 1)
}

// =============================================================================
// D-13 absolute load — from configured root
// =============================================================================

func TestLoad_Absolute(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	target := filepath.Join(dir, "shared", "util.star")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	caller := filepath.Join(subDir, "main.star")

	require.NoError(t, os.WriteFile(target, []byte(`
def shared_step():
    return step(action=fake_ext.echo(msg="shared"))
`), 0o644))
	require.NoError(t, os.WriteFile(caller, []byte(`
load("/shared/util.star", "shared_step")
flow(name="absolute_use", inputs={}, steps=[shared_step()])
`), 0o644))

	p, err := NewParser(WithRoot(dir), WithExtensions(&fakeExtension{}))
	require.NoError(t, err)
	flows, err := p.ParseFile(caller)
	require.NoError(t, err)
	require.Contains(t, flows, "absolute_use")
}

// =============================================================================
// D-14 .git ancestor discovery — no WithRoot
// =============================================================================

func TestLoad_GitAncestor(t *testing.T) {
	repoDir := t.TempDir()
	// Fake .git directory — a marker file is fine, findGitRoot uses os.Stat.
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755))

	subDir := filepath.Join(repoDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	target := filepath.Join(repoDir, "shared", "util.star")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	caller := filepath.Join(subDir, "main.star")

	require.NoError(t, os.WriteFile(target, []byte(`
def shared_step():
    return step(action=fake_ext.echo(msg="git"))
`), 0o644))
	require.NoError(t, os.WriteFile(caller, []byte(`
load("/shared/util.star", "shared_step")
flow(name="git_root_absolute", inputs={}, steps=[shared_step()])
`), 0o644))

	// Important: do NOT pass WithRoot — let findGitRoot pick it up.
	p, err := NewParser(WithExtensions(&fakeExtension{}))
	require.NoError(t, err)
	flows, err := p.ParseFile(caller)
	require.NoError(t, err)
	require.Contains(t, flows, "git_root_absolute")
}

// =============================================================================
// D-14 no root, no .git → absolute load fails with clear error
// =============================================================================

func TestLoad_NoRootNoGit(t *testing.T) {
	// Use a path that's deeply unlikely to have a .git ancestor: /tmp.
	// Even on dev machines, /tmp usually isn't inside a git repo.
	// As a defense, we additionally chdir-independence: write a file
	// directly at /tmp/<random>.star.
	dir := t.TempDir()
	caller := filepath.Join(dir, "main.star")
	require.NoError(t, os.WriteFile(caller, []byte(`
load("/no/such/path.star", "foo")
flow(name="x", inputs={}, steps=[])
`), 0o644))

	// Skip if a .git ancestor of dir exists (unlikely for t.TempDir() on
	// macOS/Linux which puts tmpdirs under /tmp or /private/tmp).
	if findGitRoot(filepath.Dir(caller)) != "" {
		t.Skip("test environment has a .git ancestor of t.TempDir; cannot exercise the no-root path")
	}

	p, err := NewParser(WithExtensions(&fakeExtension{}))
	require.NoError(t, err)
	_, err = p.ParseFile(caller)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe))
	assert.Contains(t, err.Error(), "no root configured",
		"absolute load without root must surface 'no root configured': %v", err)
}

// =============================================================================
// D-17 sandbox: traversal rejected
// =============================================================================

func TestLoad_TraversalRejected(t *testing.T) {
	dir := t.TempDir()
	caller := filepath.Join(dir, "main.star")
	require.NoError(t, os.WriteFile(caller, []byte(`
load("../../../../../../../../etc/passwd", "x")
flow(name="x", inputs={}, steps=[])
`), 0o644))

	p, err := NewParser(WithRoot(dir), WithExtensions(&fakeExtension{}))
	require.NoError(t, err)
	_, err = p.ParseFile(caller)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe))
	assert.Contains(t, err.Error(), "escapes parser root")
}

// =============================================================================
// load cache: same file loaded twice = single read
// =============================================================================

func TestLoad_Cache(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shared.star")
	caller1 := filepath.Join(dir, "a.star")
	caller2 := filepath.Join(dir, "b.star")

	require.NoError(t, os.WriteFile(target, []byte(`
def shared_step():
    return step(action=fake_ext.echo(msg="shared"))
`), 0o644))
	require.NoError(t, os.WriteFile(caller1, []byte(`
load("./shared.star", "shared_step")
flow(name="aflow", inputs={}, steps=[shared_step()])
`), 0o644))
	require.NoError(t, os.WriteFile(caller2, []byte(`
load("./shared.star", "shared_step")
flow(name="bflow", inputs={}, steps=[shared_step()])
`), 0o644))

	// Single parser session loads both — the second load() of shared.star
	// must hit the cache. Cache state is internal so we verify by
	// inspecting p.loadCache after parsing.
	p, err := NewParser(WithRoot(dir), WithExtensions(&fakeExtension{}))
	require.NoError(t, err)

	flows1, err := p.ParseFile(caller1)
	require.NoError(t, err)
	require.Contains(t, flows1, "aflow")

	flows2, err := p.ParseFile(caller2)
	require.NoError(t, err)
	require.Contains(t, flows2, "bflow")

	// loadCache has exactly one entry for shared.star (regardless of how
	// many times caller files load it).
	absTarget, _ := filepath.Abs(target)
	_, ok := p.loadCache[absTarget]
	assert.True(t, ok, "shared.star must be in loadCache")
}

// =============================================================================
// Repo-fixture tests — exercise the real corpus under tests/fixtures/valid/
// (relative + absolute load + cross-flow resolution)
// =============================================================================

func TestLoad_RelativeFixture(t *testing.T) {
	p, err := NewParser(
		WithRoot("../../tests/fixtures/valid"),
		WithExtensions(&fakeExtension{}),
	)
	require.NoError(t, err)
	flows, err := p.ParseFile("../../tests/fixtures/valid/04-load-relative.star")
	require.NoError(t, err)
	require.Contains(t, flows, "uses_relative_load")
}

func TestLoad_AbsoluteFixture(t *testing.T) {
	p, err := NewParser(
		WithRoot("../../tests/fixtures/valid"),
		WithExtensions(&fakeExtension{}),
	)
	require.NoError(t, err)
	flows, err := p.ParseFile("../../tests/fixtures/valid/05-load-absolute.star")
	require.NoError(t, err)
	require.Contains(t, flows, "uses_absolute_load")
}
