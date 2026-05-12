package deliveries

import "sync"

// DefaultCap is the default ring-buffer capacity per D-7.3-12 / UI-02
// ("last 100 deliveries").
const DefaultCap = 100

// RingBuffer is a fixed-capacity, concurrent-safe newest-first ring
// buffer for webhook deliveries. Append rolls over; Snapshot copies
// out. Per Research Pattern 4: simple slice-with-head over
// container/ring.
//
// Pitfall 6: callers MUST pass a Delivery whose Headers map has
// already been processed by RedactHeaders. Append takes by value but
// the map field is a reference type — passing the raw http.Header
// (or any caller-retained map) would leak secrets and allow mutation
// to corrupt stored views.
type RingBuffer struct {
	mu    sync.RWMutex
	items []Delivery
	head  int
	count int
	cap   int
}

// NewRingBuffer pre-sizes the backing slice. Panics on cap <= 0
// because that is a programmer error, not a runtime concern.
func NewRingBuffer(cap int) *RingBuffer {
	if cap <= 0 {
		panic("deliveries: RingBuffer cap must be > 0")
	}
	return &RingBuffer{items: make([]Delivery, cap), cap: cap}
}

// Append writes d at the head index, advances head, increments
// count up to cap.
func (b *RingBuffer) Append(d Delivery) {
	b.mu.Lock()
	b.items[b.head] = d
	b.head = (b.head + 1) % b.cap
	if b.count < b.cap {
		b.count++
	}
	b.mu.Unlock()
}

// Snapshot returns the last n deliveries newest-first. If n > count,
// returns all count entries. Always returns a fresh slice the caller
// may keep and mutate freely.
func (b *RingBuffer) Snapshot(n int) []Delivery {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if n > b.count {
		n = b.count
	}
	if n == 0 {
		return nil
	}
	out := make([]Delivery, n)
	idx := b.head - 1
	if idx < 0 {
		idx = b.cap - 1
	}
	for i := 0; i < n; i++ {
		out[i] = b.items[idx]
		idx--
		if idx < 0 {
			idx = b.cap - 1
		}
	}
	return out
}

// Len returns the number of valid entries (0..cap).
func (b *RingBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}
