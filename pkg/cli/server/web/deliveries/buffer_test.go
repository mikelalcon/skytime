package deliveries

import "testing"

func TestRingBuffer_AppendUnderCap(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 02. Asserts Append + Snapshot(n) returns newest-first slice when count < cap.")
}

func TestRingBuffer_LastHundred(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 02. Asserts 150 appends with cap=100 retain the last 100 in newest-first order.")
}

func TestRingBuffer_ConcurrentAppendSnapshot(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 02. Asserts concurrent Append + Snapshot is race-free under -race.")
}

func TestRingBuffer_LenAccurate(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 02. Asserts Len() == min(appends, cap).")
}
