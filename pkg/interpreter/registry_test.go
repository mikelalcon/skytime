package interpreter

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// helperNewParsedFlow returns a minimal *ParsedFlow for registry tests. The
// flow has an empty Body; tests only need the registry-side identity.
func helperNewParsedFlow(name string) *ParsedFlow {
	filename := "test.star"
	return &ParsedFlow{
		Flow: &dag.Flow{
			Pos:    syntax.MakePosition(&filename, 1, 1),
			Name:   name,
			Inputs: map[string]string{},
			Body:   nil,
		},
		Lambdas: map[string]*dag.CapturedLambda{},
	}
}

// TestRegistry_RegisterAndLookup is the happy path: register one flow,
// freeze, lookup by exact (name, hash) returns the parsed pointer; lookup
// for a non-registered hash returns (nil, false).
func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	parsed := helperNewParsedFlow("greet")
	require.NoError(t, r.Register("greet", "abcd1234", parsed))
	r.Freeze()

	got, ok := r.Lookup("greet", "abcd1234")
	require.True(t, ok)
	require.Same(t, parsed, got, "Lookup must return the exact ParsedFlow registered")

	miss, ok := r.Lookup("greet", "deadbeef")
	require.False(t, ok)
	require.Nil(t, miss)

	miss2, ok := r.Lookup("missing", "abcd1234")
	require.False(t, ok)
	require.Nil(t, miss2)
}

// TestRegistry_FreezeBlocksRegister: post-Freeze, Register must return
// ErrRegistryFrozen. Worker boot is the only legitimate Register caller;
// any post-freeze call is a bug we want to surface.
func TestRegistry_FreezeBlocksRegister(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register("a", "h1", helperNewParsedFlow("a")))
	r.Freeze()

	err := r.Register("b", "h2", helperNewParsedFlow("b"))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRegistryFrozen),
		"post-Freeze Register must return ErrRegistryFrozen, got: %v", err)

	// Idempotent freeze: calling Freeze twice is a no-op.
	r.Freeze()
	err2 := r.Register("c", "h3", helperNewParsedFlow("c"))
	require.True(t, errors.Is(err2, ErrRegistryFrozen))
}

// TestRegistry_LookupBeforeFreezeAllowed: Lookup is permitted before
// Freeze. Tests register-then-lookup without freezing in between (used
// by some worker-boot flows that lookup their own registrations during
// validation prior to Freeze).
func TestRegistry_LookupBeforeFreezeAllowed(t *testing.T) {
	r := NewRegistry()
	parsed := helperNewParsedFlow("x")
	require.NoError(t, r.Register("x", "h1", parsed))

	got, ok := r.Lookup("x", "h1")
	require.True(t, ok)
	require.Same(t, parsed, got)
}

// TestRegistry_DuplicateNameSameHash: registering the same (name, hash)
// pair twice returns ErrDuplicateFlow. Defense in depth — boot loops
// shouldn't double-register.
func TestRegistry_DuplicateNameSameHash(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register("greet", "h1", helperNewParsedFlow("greet")))

	err := r.Register("greet", "h1", helperNewParsedFlow("greet"))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrDuplicateFlow),
		"duplicate (name, hash) must return ErrDuplicateFlow, got: %v", err)
}

// TestRegistry_DuplicateNameDifferentHash: registering "greet"@h1 and
// "greet"@h2 is ALLOWED (multi-version registry shape per RESEARCH).
// Both lookups succeed, lookups for unrelated hashes fail.
func TestRegistry_DuplicateNameDifferentHash(t *testing.T) {
	r := NewRegistry()
	p1 := helperNewParsedFlow("greet")
	p2 := helperNewParsedFlow("greet")
	require.NoError(t, r.Register("greet", "h1", p1))
	require.NoError(t, r.Register("greet", "h2", p2))
	r.Freeze()

	got1, ok := r.Lookup("greet", "h1")
	require.True(t, ok)
	require.Same(t, p1, got1)

	got2, ok := r.Lookup("greet", "h2")
	require.True(t, ok)
	require.Same(t, p2, got2)

	_, ok = r.Lookup("greet", "h3")
	require.False(t, ok)
}

// TestRegistry_ContentHashFor: when exactly one version of a flow_name
// is registered, ContentHashFor returns (hash, true). Zero or multiple
// versions return ("", false) — call_flow walker (plan 03-03) treats
// the false case as a clean error.
func TestRegistry_ContentHashFor(t *testing.T) {
	r := NewRegistry()

	// Zero versions registered for "missing" → ("", false).
	hash, ok := r.ContentHashFor("missing")
	require.False(t, ok)
	require.Empty(t, hash)

	// Exactly one version → returns its hash.
	require.NoError(t, r.Register("only", "abc123", helperNewParsedFlow("only")))
	hash, ok = r.ContentHashFor("only")
	require.True(t, ok)
	require.Equal(t, "abc123", hash)

	// Multi-version → ("", false).
	require.NoError(t, r.Register("multi", "h1", helperNewParsedFlow("multi")))
	require.NoError(t, r.Register("multi", "h2", helperNewParsedFlow("multi")))
	hash, ok = r.ContentHashFor("multi")
	require.False(t, ok, "ContentHashFor with two versions must return (\"\", false)")
	require.Empty(t, hash)
}

// TestRegistry_ConcurrentLookupSafe stresses post-Freeze concurrency.
// 100 goroutines hammer Lookup; must complete without -race findings.
// The internal RWMutex handles read concurrency; Freeze guarantees the
// inner map is read-only during the Lookup phase.
func TestRegistry_ConcurrentLookupSafe(t *testing.T) {
	r := NewRegistry()
	parsed := helperNewParsedFlow("conc")
	require.NoError(t, r.Register("conc", "h1", parsed))
	r.Freeze()

	const N = 100
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			got, ok := r.Lookup("conc", "h1")
			assert.True(t, ok)
			assert.Same(t, parsed, got)

			// Negative-path concurrency: lookups for missing hashes.
			_, ok = r.Lookup("conc", "h2")
			assert.False(t, ok)
		}()
	}
	wg.Wait()
}

// TestRegistry_RegisterRequiresAllFields: nil parsed, empty name, or
// empty hash must be rejected with a clear error (defensive guard
// against accidental zero-value Register calls during worker-boot
// scaffolding).
func TestRegistry_RegisterRequiresAllFields(t *testing.T) {
	r := NewRegistry()

	err := r.Register("", "h", helperNewParsedFlow("x"))
	require.Error(t, err)

	err = r.Register("name", "", helperNewParsedFlow("x"))
	require.Error(t, err)

	err = r.Register("name", "h", nil)
	require.Error(t, err)
}
