package events

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/cli/server/web/deliveries"
)

// newTestBroadcaster wires up the dispatched channel so tests can wait
// deterministically for fan-out completion (m1 from Phase 7.3 checker:
// no time.Sleep in concurrency tests).
func newTestBroadcaster(t *testing.T, snap SnapshotFunc) *Broadcaster {
	t.Helper()
	b := NewBroadcaster(snap)
	b.dispatched = make(chan struct{}, 64)
	return b
}

// waitDispatched waits for n fan-out completions (1 per Publish call) or
// fails the test if the deadline passes.
func waitDispatched(t *testing.T, b *Broadcaster, n int, deadline time.Duration) {
	t.Helper()
	end := time.After(deadline)
	for i := 0; i < n; i++ {
		select {
		case <-b.dispatched:
		case <-end:
			t.Fatalf("waitDispatched: only saw %d of %d fan-outs before %s", i, n, deadline)
		}
	}
}

func TestBroadcaster_SnapshotOnSubscribe(t *testing.T) {
	snap := Snapshot{
		Workflows:  []WorkflowState{{WorkflowID: "wf-1", FlowName: "f"}},
		Deliveries: []deliveries.Delivery{{ID: "d-1"}},
	}
	b := NewBroadcaster(func() Snapshot { return snap })
	defer b.Shutdown()

	got, _, unsub := b.Subscribe()
	defer unsub()
	require.Len(t, got.Workflows, 1)
	require.Equal(t, "wf-1", got.Workflows[0].WorkflowID)
	require.Len(t, got.Deliveries, 1)
	require.Equal(t, "d-1", got.Deliveries[0].ID)
}

func TestBroadcaster_Publish_FanOut(t *testing.T) {
	b := NewBroadcaster(nil)
	defer b.Shutdown()
	_, ch1, u1 := b.Subscribe()
	defer u1()
	_, ch2, u2 := b.Subscribe()
	defer u2()

	b.Publish(Event{Name: "x"})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			require.Equal(t, "x", ev.Name)
		case <-time.After(200 * time.Millisecond):
			t.Fatal("subscriber did not receive event within 200ms")
		}
	}
}

func TestBroadcaster_DropOldest(t *testing.T) {
	// m1 from checker: use dispatched-channel barrier instead of
	// time.Sleep. The test-only newTestBroadcaster installs the
	// dispatched signal so each Publish is observable.
	b := newTestBroadcaster(t, nil)
	defer b.Shutdown()
	_, ch, unsub := b.Subscribe()
	defer unsub()

	// Publish 16 events and wait for each fan-out to complete.
	for i := 0; i < 16; i++ {
		b.Publish(Event{Name: "x", Payload: i})
	}
	waitDispatched(t, b, 16, time.Second)
	// After fan-out, the subscriber's 16-deep buffer is full with
	// events 0..15.

	// Publish a 17th — drop-oldest kicks in: event 0 falls out, event
	// 16 is enqueued.
	b.Publish(Event{Name: "x", Payload: 16})
	waitDispatched(t, b, 1, time.Second)

	// Drain: oldest visible should be event 1 (event 0 dropped), newest
	// should be event 16.
	first := <-ch
	require.Equal(t, 1, first.Payload, "expected oldest in buffer to be event #1 after drop-oldest fired")

	// Drain remainder; last one is event 16.
	var last Event
drain:
	for {
		select {
		case ev := <-ch:
			last = ev
		case <-time.After(100 * time.Millisecond):
			break drain
		}
	}
	require.Equal(t, 16, last.Payload)
}

func TestBroadcaster_Shutdown_ClosesSubscriberChannels(t *testing.T) {
	b := NewBroadcaster(nil)
	_, ch, unsub := b.Subscribe()
	defer unsub()
	b.Shutdown()

	select {
	case _, ok := <-ch:
		require.False(t, ok, "channel should be closed after Shutdown")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("subscriber channel was not closed within 200ms of Shutdown")
	}
}

func TestBroadcaster_UnsubscribeIsIdempotent(t *testing.T) {
	b := NewBroadcaster(nil)
	defer b.Shutdown()
	_, _, unsub := b.Subscribe()
	unsub()
	require.NotPanics(t, unsub) // second call must not panic
}

// TestBroadcaster_ConcurrentPublishSubscribe is a race-detector smoke
// test that hammers Subscribe/Publish concurrently. Helps catch
// lock-misuse under -race.
func TestBroadcaster_ConcurrentPublishSubscribe(t *testing.T) {
	b := NewBroadcaster(func() Snapshot { return Snapshot{} })
	defer b.Shutdown()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ch, unsub := b.Subscribe()
			defer unsub()
			for j := 0; j < 50; j++ {
				select {
				case <-ch:
				default:
				}
			}
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				b.Publish(Event{Name: "x"})
			}
		}()
	}
	wg.Wait()
}
