package testing

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// newRef is a tiny helper that builds an ActionRef with just the kind
// populated — sufficient for registry.Match input.
func newRef(kind string) *dag.ActionRef {
	return &dag.ActionRef{Kind_: kind}
}

// labelOf returns a recognizable string for a MockEntry; used to
// distinguish tier ordering across nested t.Run cases. We label via
// the RegisterPos.Filename() accessor so each Add can carry a free
// string (syntax.Position's filename field is unexported, accessed
// via the Filename() method, and constructed via syntax.MakePosition).
func labelOf(e MockEntry) string { return e.RegisterPos.Filename() }

func mkEntry(ext, op, label string, match map[string]*regexp.Regexp) MockEntry {
	file := label
	return MockEntry{
		Extension:   ext,
		Op:          op,
		Match:       match,
		RegisterPos: syntax.MakePosition(&file, 0, 0),
	}
}

// TestRegistry_TierPrecedence_TestVectors exercises the D5-B4 3-tier
// ladder via subtests. This is the named test VALIDATION.md cites for
// the 3-tier ladder.
func TestRegistry_TierPrecedence_TestVectors(t *testing.T) {
	t.Run("tier 1 wins over tier 2", func(t *testing.T) {
		r := NewMockRegistry()
		// Add tier-2-eligible entry (gh, get, no match) FIRST.
		require.NoError(t, r.Add(mkEntry("gh", "get", "tier-2-noop", nil)))
		// Then add tier-1-eligible (gh, get, match path).
		match := map[string]*regexp.Regexp{
			"path": regexp.MustCompile(`^/users/[a-z]+$`),
		}
		require.NoError(t, r.Add(mkEntry("gh", "get", "tier-1-winner", match)))

		got, ok := r.Match(newRef("gh.get"), map[string]string{"path": "/users/octocat"})
		require.True(t, ok, "expected a match")
		assert.Equal(t, "tier-1-winner", labelOf(got))
	})

	t.Run("tier 2 wins over tier 3 wildcard", func(t *testing.T) {
		r := NewMockRegistry()
		// Tier-3 wildcard registered first.
		require.NoError(t, r.Add(mkEntry("gh", "*", "tier-3-wild", nil)))
		// Tier-2 (gh, get, no match) registered after.
		require.NoError(t, r.Add(mkEntry("gh", "get", "tier-2-winner", nil)))

		got, ok := r.Match(newRef("gh.get"), nil)
		require.True(t, ok)
		assert.Equal(t, "tier-2-winner", labelOf(got))
	})

	t.Run("recency within tier 2", func(t *testing.T) {
		r := NewMockRegistry()
		require.NoError(t, r.Add(mkEntry("gh", "get", "A", nil)))
		require.NoError(t, r.Add(mkEntry("gh", "get", "B", nil)))

		got, ok := r.Match(newRef("gh.get"), nil)
		require.True(t, ok)
		assert.Equal(t, "B", labelOf(got),
			"most-recently-registered within a tier must win")
	})

	t.Run("per-test frame shadows file frame", func(t *testing.T) {
		r := NewMockRegistry()
		require.NoError(t, r.Add(mkEntry("gh", "get", "file", nil)))

		r.PushTestFrame()
		require.NoError(t, r.Add(mkEntry("gh", "get", "test", nil)))
		got, ok := r.Match(newRef("gh.get"), nil)
		require.True(t, ok)
		assert.Equal(t, "test", labelOf(got),
			"per-test frame must shadow file frame")

		r.PopTestFrame()
		got, ok = r.Match(newRef("gh.get"), nil)
		require.True(t, ok)
		assert.Equal(t, "file", labelOf(got),
			"after PopTestFrame, file-level entry surfaces again")
	})

	t.Run("tier 3 wildcard catches unmatched op", func(t *testing.T) {
		r := NewMockRegistry()
		require.NoError(t, r.Add(mkEntry("gh", "*", "wild", nil)))

		got, ok := r.Match(newRef("gh.delete"), nil)
		require.True(t, ok)
		assert.Equal(t, "wild", labelOf(got))

		_, ok = r.Match(newRef("other.x"), nil)
		assert.False(t, ok, "wildcard must not cross extensions")
	})
}

// TestRegistry_EmptyReturnsNoMatch — Test 1.
func TestRegistry_EmptyReturnsNoMatch(t *testing.T) {
	r := NewMockRegistry()
	_, ok := r.Match(newRef("gh.get"), nil)
	assert.False(t, ok, "empty registry must report no match")
	_, ok = r.Match(newRef("malformed"), nil)
	assert.False(t, ok, "malformed kind (no dot) yields no match")
}

// TestRegistry_NoCrossExtensionWildcard — Test 6.
func TestRegistry_NoCrossExtensionWildcard(t *testing.T) {
	r := NewMockRegistry()
	err := r.Add(mkEntry("*", "get", "bad", nil))
	require.Error(t, err, "extension=* must be rejected (D5-B3)")
	assert.ErrorIs(t, err, ErrCrossExtensionWildcard)
}

// TestRegistry_RegexCompileAtRegistration — Test 7.
func TestRegistry_RegexCompileAtRegistration(t *testing.T) {
	// CompileMatchRegex is the registration-time helper.
	good, err := CompileMatchRegex("path", `^/users/[a-z]+$`)
	require.NoError(t, err)
	require.NotNil(t, good)

	bad, err := CompileMatchRegex("path", `[unterminated`)
	require.Error(t, err, "bad regex must surface at registration time")
	assert.Nil(t, bad)
	assert.Contains(t, err.Error(), "match[\"path\"]")

	// Use the compiled regex in a tier-1 match.
	r := NewMockRegistry()
	require.NoError(t, r.Add(mkEntry("gh", "get", "regex-pin", map[string]*regexp.Regexp{"path": good})))
	got, ok := r.Match(newRef("gh.get"), map[string]string{"path": "/users/octocat"})
	require.True(t, ok)
	assert.Equal(t, "regex-pin", labelOf(got))

	_, ok = r.Match(newRef("gh.get"), map[string]string{"path": "/users/123"})
	assert.False(t, ok, "non-matching kwarg must yield no tier-1 hit")
}

// TestRegistry_PartialMatchByDefault — Test 8 / D5-B5.
func TestRegistry_PartialMatchByDefault(t *testing.T) {
	r := NewMockRegistry()
	// Partial match (no anchors).
	partial := regexp.MustCompile(`/users/oct`)
	require.NoError(t, r.Add(mkEntry("gh", "get", "partial",
		map[string]*regexp.Regexp{"path": partial})))

	got, ok := r.Match(newRef("gh.get"), map[string]string{"path": "/users/octocat"})
	require.True(t, ok)
	assert.Equal(t, "partial", labelOf(got))

	// Anchored regex.
	r2 := NewMockRegistry()
	anchored := regexp.MustCompile(`^/users/octocat$`)
	require.NoError(t, r2.Add(mkEntry("gh", "get", "anchored",
		map[string]*regexp.Regexp{"path": anchored})))

	_, ok = r2.Match(newRef("gh.get"), map[string]string{"path": "/users/octocatcat"})
	assert.False(t, ok, "anchored regex must not partial-match")
}

// TestRegistry_NonStringKwargAbsentFromMap — D5-B6: non-string kwargs
// don't appear in kwargsAsString, so a tier-1 entry keyed on a
// non-string kwarg fails to match.
func TestRegistry_NonStringKwargAbsentFromMap(t *testing.T) {
	r := NewMockRegistry()
	require.NoError(t, r.Add(mkEntry("gh", "get", "needs-id",
		map[string]*regexp.Regexp{"id": regexp.MustCompile(`42`)})))

	// Caller (tier-1 router) only passes string-valued kwargs into
	// kwargsAsString; an int-valued "id" kwarg simply isn't present.
	_, ok := r.Match(newRef("gh.get"), map[string]string{"path": "/x"})
	assert.False(t, ok, "absent match key must fail tier 1")
}

// TestRegistry_PopTestFramePanicsOnEmptyStack — defensive guard.
func TestRegistry_PopTestFramePanicsOnEmptyStack(t *testing.T) {
	r := NewMockRegistry()
	assert.Panics(t, func() {
		r.PopTestFrame()
	})
}
