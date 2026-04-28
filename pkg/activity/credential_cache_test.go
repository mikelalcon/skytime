package activity

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
	extensiontesting "github.com/mikelalcon/skytime/pkg/extension/testing"
)

// counterHandler wraps another handler and counts Resolve calls. Used
// throughout cache tests to assert hit/miss ratios.
type counterHandler struct {
	inner extension.CredentialHandler
	calls atomic.Int32
}

func (c *counterHandler) Resolve(ctx context.Context, id string) (extension.Credential, error) {
	c.calls.Add(1)
	return c.inner.Resolve(ctx, id)
}

// erroringHandler always returns the same error (no Credential). Used to
// verify that errors are NOT cached (which would mask transient backend
// failures by making them look permanent until TTL expiry).
type erroringHandler struct {
	err   error
	calls atomic.Int32
}

func (e *erroringHandler) Resolve(_ context.Context, _ string) (extension.Credential, error) {
	e.calls.Add(1)
	return nil, e.err
}

// newWarmCache builds a credentialCache wrapping a counterHandler over an
// in-memory FakeCredentialHandler with three Bearer credentials.
func newWarmCache(t *testing.T, ttl time.Duration) (*credentialCache, *counterHandler) {
	t.Helper()
	inner := &extensiontesting.FakeCredentialHandler{
		Creds: map[string]extension.Credential{
			"admin":  &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("ghp_admin")},
			"ci":     &extension.BearerCredential{ID_: "ci", Token: extension.NewSecret("ghp_ci")},
			"deploy": &extension.BearerCredential{ID_: "deploy", Token: extension.NewSecret("ghp_deploy")},
		},
	}
	ch := &counterHandler{inner: inner}
	return newCredentialCache(ch, ttl), ch
}

// TestCredentialCache_HitsAfterFirstResolve: second resolve hits the cache,
// handler is called only once. Default TTL=1h so no expiry interferes.
func TestCredentialCache_HitsAfterFirstResolve(t *testing.T) {
	c, ch := newWarmCache(t, time.Hour)
	_, err := c.resolve(context.Background(), "admin", false)
	require.NoError(t, err)
	_, err = c.resolve(context.Background(), "admin", false)
	require.NoError(t, err)
	require.Equal(t, int32(1), ch.calls.Load(), "second resolve should hit cache")
}

// TestCredentialCache_ExpiresAfterTTL: with the injectable now() clock at
// start+15ms (TTL=10ms), the second resolve treats the entry as expired and
// re-resolves.
func TestCredentialCache_ExpiresAfterTTL(t *testing.T) {
	c, ch := newWarmCache(t, 10*time.Millisecond)
	start := time.Now()
	c.now = func() time.Time { return start }
	_, err := c.resolve(context.Background(), "admin", false)
	require.NoError(t, err)
	c.now = func() time.Time { return start.Add(15 * time.Millisecond) }
	_, err = c.resolve(context.Background(), "admin", false)
	require.NoError(t, err)
	require.Equal(t, int32(2), ch.calls.Load(), "expired entry must trigger re-resolve")
}

// TestCredentialCache_BypassForcesFreshResolve: bypass=true ignores cached
// entry; the bypass call also writes a fresh entry, so a follow-up
// non-bypass call hits the cache.
func TestCredentialCache_BypassForcesFreshResolve(t *testing.T) {
	c, ch := newWarmCache(t, time.Hour)
	_, _ = c.resolve(context.Background(), "admin", false) // warm: 1 call
	_, _ = c.resolve(context.Background(), "admin", true)  // bypass: 2 calls
	require.Equal(t, int32(2), ch.calls.Load())
	_, _ = c.resolve(context.Background(), "admin", false) // hits the bypass write: still 2
	require.Equal(t, int32(2), ch.calls.Load())
}

// TestCredentialCache_Invalidate: invalidate() drops the entry; next resolve
// re-calls the handler.
func TestCredentialCache_Invalidate(t *testing.T) {
	c, ch := newWarmCache(t, time.Hour)
	_, _ = c.resolve(context.Background(), "admin", false)
	c.invalidate("admin")
	_, _ = c.resolve(context.Background(), "admin", false)
	require.Equal(t, int32(2), ch.calls.Load())
}

// TestCredentialCache_HandlerError_NotCached: errors are NOT cached. Caching
// errors would mask transient backend failures by making them look permanent
// until TTL expiry — D2-12 leaves classification to classifyResolveError, not
// to the cache.
func TestCredentialCache_HandlerError_NotCached(t *testing.T) {
	h := &erroringHandler{err: errors.New("backend unreachable")}
	c := newCredentialCache(h, time.Hour)
	_, err := c.resolve(context.Background(), "admin", false)
	require.Error(t, err)
	_, err = c.resolve(context.Background(), "admin", false)
	require.Error(t, err)
	require.Equal(t, int32(2), h.calls.Load(), "errors must NOT be cached")
}

// TestCredentialCache_RaceParallelBatches: 8 parallel goroutines each
// resolving 3 IDs 50 times = 1200 total resolve calls. Bounds:
//   - lower: >= 3 (at least one resolve per ID — proves nothing dropped)
//   - upper: <= 48 (goroutines * len(ids) * 2 — generous cold-start ceiling
//     for slow CI; typical observed value on dev hardware is 3–6)
//
// Run with -race to catch any data races in the RWMutex/map interaction.
func TestCredentialCache_RaceParallelBatches(t *testing.T) {
	c, ch := newWarmCache(t, 5*time.Minute)
	var wg sync.WaitGroup
	const goroutines = 8
	const iterationsPer = 50
	ids := []string{"admin", "ci", "deploy"}
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterationsPer; j++ {
				for _, id := range ids {
					_, err := c.resolve(context.Background(), id, false)
					require.NoError(t, err)
				}
			}
		}()
	}
	wg.Wait()
	got := ch.calls.Load()
	require.GreaterOrEqual(t, got, int32(3),
		"at least one resolve per ID required — saw %d", got)
	require.LessOrEqual(t, got, int32(48),
		"cache thundering-herd should bound calls — saw %d, expected <= 48 (goroutines*ids*2)", got)
}

// TestCredentialCache_ZeroTTLBehavior: ttl=0 means "never cache" — every
// resolve calls the handler. Defensive default; the production path uses
// 5min via WithCredentialCacheTTL.
func TestCredentialCache_ZeroTTLBehavior(t *testing.T) {
	c, ch := newWarmCache(t, 0)
	_, _ = c.resolve(context.Background(), "admin", false)
	_, _ = c.resolve(context.Background(), "admin", false)
	require.Equal(t, int32(2), ch.calls.Load(), "ttl=0 means never cache")
}
