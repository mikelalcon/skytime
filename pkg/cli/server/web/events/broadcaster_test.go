package events

import "testing"

func TestBroadcaster_SnapshotOnSubscribe(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 03. Asserts new subscribers receive a snapshot captured under the same lock that registers their channel — no race window per Research Pitfall 1.")
}

func TestBroadcaster_Publish_FanOut(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 03. Asserts Publish(ev) delivers to all current subscribers.")
}

func TestBroadcaster_DropOldest(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 03. Asserts when a subscriber's 16-deep buffer is full, the OLDEST queued event is discarded so the newest event still arrives.")
}

func TestBroadcaster_Shutdown_ClosesSubscriberChannels(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 03. Asserts Shutdown() closes all subscriber channels and the fan-out goroutine exits.")
}

func TestBroadcaster_UnsubscribeIsIdempotent(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 03. Asserts calling the returned unsubscribe func twice does not panic and does not double-close.")
}
