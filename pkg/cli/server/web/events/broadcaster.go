package events

import "sync"

// SnapshotFunc returns the current Snapshot (workflow list + delivery
// buffer) under the broadcaster mutex — Research Pitfall 1.
//
// The poller owns the workflow list; the receiver (Plan 02) owns the
// delivery buffer; wire-up happens in pkg/cli/server.go (Plan 04). A nil
// SnapshotFunc is legal — Subscribe then returns the zero-value Snapshot.
type SnapshotFunc func() Snapshot

// subscriberChanBuf is the per-subscriber buffer depth. 16 is enough to
// absorb ~30s of poll-cycle deltas at the default 2s cadence without
// blocking; drop-oldest takes over above that (D-7.3-03 + Research
// Pattern 2).
const subscriberChanBuf = 16

// publishChanBuf is the broadcaster's inbound buffer. 64 is generous
// for typical traffic; Publish blocks only if a producer outpaces the
// fan-out goroutine for more than 64 events.
const publishChanBuf = 64

// Broadcaster fans out Events from one Publish call to N subscribers via
// a single goroutine + bounded subscriber channels with drop-oldest
// semantics. Research Pattern 2.
//
// Concurrency model:
//   - One inbound `publish` channel (buffered, 64-deep).
//   - One fan-out goroutine (`run`) drains `publish` and forwards each
//     Event to every registered subscriber.
//   - Each subscriber has its own bounded channel (16-deep). When that
//     channel is full, drop-oldest fires: the oldest queued event is
//     discarded so the newest still arrives.
//   - All registry mutations (Subscribe / unsubscribe / Shutdown drain)
//     are mutex-guarded.
//
// Pitfall 1 (Snapshot-on-Subscribe race): Subscribe atomically builds the
// Snapshot AND registers the new subscriber channel under the
// broadcaster mutex. The SSE handler MUST send the returned Snapshot as
// the first SSE frame; it MUST NOT call a separate Snapshot() method
// afterward (which would race with Publish).
type Broadcaster struct {
	mu          sync.Mutex
	subscribers map[chan Event]struct{}
	publish     chan Event
	quit        chan struct{}
	quitOnce    sync.Once
	snapshot    SnapshotFunc

	// dispatched (test-only) fires once per fan-out — i.e., after each
	// Publish has been forwarded to all subscribers. Used by
	// broadcaster_test.go's drop-oldest test to wait DETERMINISTICALLY
	// for fan-out instead of time.Sleep (m1 from Phase 7.3 checker).
	// nil in production builds — non-test paths set it never, so there
	// is no production allocation beyond the field itself.
	dispatched chan struct{}
}

// NewBroadcaster starts the fan-out goroutine. The snapshot callback may
// be nil — Subscribe then returns an empty (zero-value) Snapshot.
func NewBroadcaster(snapshot SnapshotFunc) *Broadcaster {
	b := &Broadcaster{
		subscribers: map[chan Event]struct{}{},
		publish:     make(chan Event, publishChanBuf),
		quit:        make(chan struct{}),
		snapshot:    snapshot,
	}
	go b.run()
	return b
}

// Subscribe atomically captures a Snapshot under the broadcaster mutex
// and registers a new subscriber channel. Returns the snapshot, the
// channel, and an unsubscribe func.
//
// Pitfall 1 (Snapshot-on-Subscribe race): the snapshot is captured under
// the SAME lock that registers the subscriber, so no Publish can race in
// between snapshot-build and channel-attach. The SSE handler MUST use
// the returned Snapshot as its first frame; calling a separate
// Snapshot() method later would reopen the race.
//
// The returned unsubscribe func is idempotent (sync.Once-guarded) and
// MUST NOT close(ch) — the fan-out goroutine owns close, and the SSE
// handler (receiver) selects on `<-ch + ctx.Done`. Closing here would
// race the goroutine.
func (b *Broadcaster) Subscribe() (Snapshot, <-chan Event, func()) {
	ch := make(chan Event, subscriberChanBuf)
	b.mu.Lock()
	var snap Snapshot
	if b.snapshot != nil {
		snap = b.snapshot()
	}
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	var unsubOnce sync.Once
	unsubscribe := func() {
		unsubOnce.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, ch)
			b.mu.Unlock()
			// NB: do NOT close(ch) here — the fan-out goroutine owns
			// close, and the SSE handler (receiver) selects on `<-ch`
			// + `ctx.Done`. Closing here would race the goroutine.
		})
	}
	return snap, ch, unsubscribe
}

// Publish enqueues an event for fan-out. Non-blocking on quit (returns
// silently after Shutdown so producers never panic on a closed channel).
func (b *Broadcaster) Publish(ev Event) {
	select {
	case <-b.quit:
		return
	case b.publish <- ev:
	}
}

// Shutdown stops the fan-out goroutine and closes all subscriber
// channels. Idempotent.
func (b *Broadcaster) Shutdown() {
	b.quitOnce.Do(func() {
		close(b.quit)
	})
}

// run is the single fan-out goroutine. It drains `publish` and forwards
// each event to every subscriber with drop-oldest semantics; on quit it
// closes every subscriber channel and returns.
func (b *Broadcaster) run() {
	for {
		select {
		case <-b.quit:
			b.mu.Lock()
			for ch := range b.subscribers {
				close(ch)
			}
			b.subscribers = nil
			b.mu.Unlock()
			return
		case ev := <-b.publish:
			b.mu.Lock()
			for ch := range b.subscribers {
				// Try non-blocking send first (the common case).
				select {
				case ch <- ev:
				default:
					// Buffer full: drop OLDEST (non-blocking receive),
					// then retry. If still full, drop this event for
					// this subscriber (drop-newest fallback in worst
					// case — should never happen under the 16-deep
					// buffer + 2s poll cadence).
					select {
					case <-ch:
					default:
					}
					select {
					case ch <- ev:
					default:
					}
				}
			}
			b.mu.Unlock()
			// Non-blocking signal for tests waiting on fan-out
			// completion (m1 from Phase 7.3 checker — no time.Sleep).
			if b.dispatched != nil {
				select {
				case b.dispatched <- struct{}{}:
				default:
				}
			}
		}
	}
}
