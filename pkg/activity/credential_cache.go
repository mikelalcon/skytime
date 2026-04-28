package activity

import (
	"context"
	"sync"
	"time"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// cachedEntry holds one resolved Credential plus the time it was cached.
// Lazy TTL eviction (RESEARCH §"Pattern 4"): we never run a background sweep
// — every resolve checks `now().Sub(entry.cachedAt) >= ttl` and refreshes if
// expired. Stale entries linger until next read; the worker process is the
// cache's effective lifetime, and a worker has no eviction pressure in v1.
type cachedEntry struct {
	cred     extension.Credential
	cachedAt time.Time
}

// credentialCache is a per-worker, process-local cache wrapping a
// CredentialHandler. Default TTL is 5 minutes (D2-10), settable via
// activity.WithCredentialCacheTTL on the worker registration. Cache is
// bypassed when the activity is on retry attempt > 1 (D2-11).
//
// Cooperation with the attempt seam (D2-11): the cache itself does NOT know
// about Attempt. ExecuteBatch (02-03) calls attemptFn(ctx) and then either
// passes bypass=false (Attempt == 1, normal path) or pre-invalidates every
// credential ID in the batch + passes bypass=true (Attempt > 1, retry path).
// The split keeps the cache type-narrow and the bypass policy local to
// ExecuteBatch.
//
// Concurrency: read path takes RLock; write path takes Lock. Resolve calls
// the handler OUTSIDE any lock so a slow handler doesn't block unrelated
// readers. There IS a benign double-resolve window where two goroutines miss
// the cache simultaneously and both call the handler; this is acceptable
// (handler.Resolve is idempotent by contract) and keeps the cache lock-free
// during the slow path.
//
// Type-safety choice (RESEARCH §"sync.Map vs RWMutex"): RWMutex + plain map
// preferred over sync.Map for compile-time typing and clearer TTL invariant
// management.
type credentialCache struct {
	handler extension.CredentialHandler
	ttl     time.Duration
	now     func() time.Time // injectable for tests; defaults to time.Now

	mu      sync.RWMutex
	entries map[string]cachedEntry
}

// newCredentialCache constructs an empty cache wrapping handler with the
// supplied TTL. ttl=0 means "never cache" — every resolve calls the handler
// directly (used for sanity tests; the production path uses 5min).
func newCredentialCache(handler extension.CredentialHandler, ttl time.Duration) *credentialCache {
	return &credentialCache{
		handler: handler,
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]cachedEntry),
	}
}

// resolve returns the credential for id, hitting the cache when possible.
// bypass=true (retry attempts per D2-11) skips the cache read but still
// writes a fresh entry on success — so a follow-up non-bypass call hits the
// cache. ttl=0 means "never cache" — every call goes to the handler and
// nothing is written.
//
// Errors are NOT cached: the next call retries the handler. Caching errors
// would mask transient backend failures by making them look permanent until
// TTL expiry; D2-12 leaves classification to classifyResolveError, not the
// cache.
func (c *credentialCache) resolve(ctx context.Context, id string, bypass bool) (extension.Credential, error) {
	if !bypass && c.ttl > 0 {
		c.mu.RLock()
		entry, ok := c.entries[id]
		c.mu.RUnlock()
		if ok && c.now().Sub(entry.cachedAt) < c.ttl {
			return entry.cred, nil
		}
	}

	// Miss / expired / bypass / ttl=0 — resolve OUTSIDE any lock so a slow
	// handler never blocks unrelated readers.
	fresh, err := c.handler.Resolve(ctx, id)
	if err != nil {
		return nil, err
	}

	if c.ttl > 0 {
		c.mu.Lock()
		c.entries[id] = cachedEntry{cred: fresh, cachedAt: c.now()}
		c.mu.Unlock()
	}
	return fresh, nil
}

// invalidate drops the cached entry for id. Used on retry-attempt bypass
// (D2-11): on Attempt > 1, ExecuteBatch invalidates every credential ID in
// the batch before resolving (so any in-flight readers see a clean miss
// rather than a stale entry).
func (c *credentialCache) invalidate(id string) {
	c.mu.Lock()
	delete(c.entries, id)
	c.mu.Unlock()
}
