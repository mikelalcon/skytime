package extension

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// makeOpSpec builds an OperationSpec with the supplied Idempotent value
// (pass nil to test the registry's D-12 enforcement).
func makeOpSpec(name string, idempotent *bool) *OperationSpec {
	return &OperationSpec{
		Name:       name,
		Idempotent: idempotent,
		Func: func(ctx context.Context, args any, cred Credential) (dag.OperationOutput, error) {
			return nil, nil
		},
		KwargsType: reflect.TypeOf(struct{}{}),
	}
}

// makeFakeExt builds a fakeExtension with the supplied operations.
func makeFakeExt(name string, ops map[string]*OperationSpec) *fakeExtension {
	return &fakeExtension{name: name, ops: ops}
}

// TestRegistry_NewIsEmpty — sanity that NewRegistry returns a usable
// non-nil registry with no extensions.
func TestRegistry_NewIsEmpty(t *testing.T) {
	r := NewRegistry()
	require.NotNil(t, r)
	assert.Empty(t, r.All())
}

// TestRegistry_RegisterSucceedsForValidExtension — the happy path.
func TestRegistry_RegisterSucceedsForValidExtension(t *testing.T) {
	r := NewRegistry()
	ext := makeFakeExt("github", map[string]*OperationSpec{
		"create_issue": makeOpSpec("create_issue", Ptr(false)),
		"close_issue":  makeOpSpec("close_issue", Ptr(true)),
	})
	require.NoError(t, r.Register(ext))

	got, ok := r.Get("github")
	require.True(t, ok)
	assert.Equal(t, "github", got.Name())

	all := r.All()
	assert.Len(t, all, 1)
	assert.Contains(t, all, "github")
}

// TestRegistration_RequiresIdempotent — D-12 enforcement. An OperationSpec
// with Idempotent == nil MUST cause Register to fail with an error wrapping
// ErrIdempotentRequired so callers can errors.Is(err, ErrIdempotentRequired).
func TestRegistration_RequiresIdempotent(t *testing.T) {
	r := NewRegistry()
	ext := makeFakeExt("github", map[string]*OperationSpec{
		"create_issue": makeOpSpec("create_issue", nil), // <-- the offending op
	})

	err := r.Register(ext)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrIdempotentRequired),
		"expected errors.Is(err, ErrIdempotentRequired); got %v", err)

	// Failed registration MUST NOT leave the extension partially
	// installed — the registry should still be empty.
	assert.Empty(t, r.All())
}

// TestRegistry_RejectsDuplicateNames — second Register call for the same
// name MUST fail with a clear "already registered" error.
func TestRegistry_RejectsDuplicateNames(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(makeFakeExt("github", map[string]*OperationSpec{
		"x": makeOpSpec("x", Ptr(true)),
	})))
	err := r.Register(makeFakeExt("github", map[string]*OperationSpec{
		"y": makeOpSpec("y", Ptr(true)),
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

// TestRegistry_RejectsEmptyName — defensive check: extension name must be
// non-empty.
func TestRegistry_RejectsEmptyName(t *testing.T) {
	r := NewRegistry()
	err := r.Register(makeFakeExt("", map[string]*OperationSpec{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty")
}

// TestRegistry_RejectsNilSpec — defensive check: a nil OperationSpec is
// invalid (caller bug — usually a typo in the operations map).
func TestRegistry_RejectsNilSpec(t *testing.T) {
	r := NewRegistry()
	ext := makeFakeExt("github", map[string]*OperationSpec{
		"create_issue": nil,
	})
	err := r.Register(ext)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil spec")
}

// TestRegistry_RejectsNilFunc — defensive check: an OperationSpec without a
// Func is incoherent.
func TestRegistry_RejectsNilFunc(t *testing.T) {
	r := NewRegistry()
	spec := &OperationSpec{
		Name:       "x",
		Idempotent: Ptr(true),
		KwargsType: reflect.TypeOf(struct{}{}),
		// Func intentionally nil
	}
	ext := makeFakeExt("github", map[string]*OperationSpec{"x": spec})
	err := r.Register(ext)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil Func")
}

// TestRegistry_RejectsNilKwargsType — defensive check: every operation MUST
// declare a KwargsType (a struct type) so the parser can reflect on it.
func TestRegistry_RejectsNilKwargsType(t *testing.T) {
	r := NewRegistry()
	spec := &OperationSpec{
		Name:       "x",
		Idempotent: Ptr(true),
		Func: func(ctx context.Context, args any, cred Credential) (dag.OperationOutput, error) {
			return nil, nil
		},
		// KwargsType intentionally nil
	}
	ext := makeFakeExt("github", map[string]*OperationSpec{"x": spec})
	err := r.Register(ext)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil KwargsType")
}

// TestRegistration_StaticAndDynamic — EXT-06 contract: the Registry supports
// BOTH "register before parse" (static, package-init style) AND "register
// after parse" (dynamic, e.g., test-time injection). Both go through the
// same Register API; the difference is purely temporal.
func TestRegistration_StaticAndDynamic(t *testing.T) {
	r := NewRegistry()

	// "Static" — register before any parse work.
	require.NoError(t, r.Register(makeFakeExt("github", map[string]*OperationSpec{
		"create_issue": makeOpSpec("create_issue", Ptr(false)),
	})))

	// (Imagine a parse happening here in a real client.)

	// "Dynamic" — register a different extension later.
	require.NoError(t, r.Register(makeFakeExt("slack", map[string]*OperationSpec{
		"post_message": makeOpSpec("post_message", Ptr(false)),
	})))

	all := r.All()
	require.Len(t, all, 2)
	assert.Contains(t, all, "github")
	assert.Contains(t, all, "slack")
}

// TestRegistry_ConcurrentRegisterAndGet — exercise the sync.RWMutex by
// concurrent reads + writes. Run with -race to surface data races.
func TestRegistry_ConcurrentRegisterAndGet(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	const writers = 8
	const readers = 16
	const opsPerReader = 50

	var wg sync.WaitGroup

	// Pre-populate one extension so readers always have something to find.
	require.NoError(t, r.Register(makeFakeExt("seed", map[string]*OperationSpec{
		"x": makeOpSpec("x", Ptr(true)),
	})))

	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(idx int) {
			defer wg.Done()
			name := "ext-" + string(rune('a'+idx))
			_ = r.Register(makeFakeExt(name, map[string]*OperationSpec{
				"op": makeOpSpec("op", Ptr(true)),
			}))
		}(i)
	}

	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerReader; j++ {
				_, _ = r.Get("seed")
				_ = r.All()
			}
		}()
	}
	wg.Wait()

	// At minimum the seed extension is present; some writers may have
	// succeeded too. We only assert no panic and seed survival.
	got, ok := r.Get("seed")
	require.True(t, ok)
	require.Equal(t, "seed", got.Name())
}

// TestRegistry_GetMissingReturnsFalse — defensive: Get on an unknown name
// returns (nil, false), not panic.
func TestRegistry_GetMissingReturnsFalse(t *testing.T) {
	r := NewRegistry()
	got, ok := r.Get("nope")
	assert.False(t, ok)
	assert.Nil(t, got)
}

// Sanity: the All() snapshot is a *copy* — mutating it must not affect the
// registry's internal state.
func TestRegistry_AllReturnsSnapshot(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(makeFakeExt("a", map[string]*OperationSpec{
		"x": makeOpSpec("x", Ptr(true)),
	})))
	snap := r.All()
	require.Len(t, snap, 1)

	// Mutate the snapshot — internal state must remain.
	delete(snap, "a")
	got, ok := r.Get("a")
	require.True(t, ok, "registry must retain extension after caller mutated the All() snapshot")
	assert.Equal(t, "a", got.Name())
}

// Reference unused starlark import to satisfy linters; the registry tests
// build *fakeExtension which embeds a starlark-returning Initialize.
var _ = starlark.None
