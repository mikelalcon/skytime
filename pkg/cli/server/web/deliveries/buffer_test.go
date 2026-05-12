package deliveries

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRingBuffer_AppendUnderCap(t *testing.T) {
	buf := NewRingBuffer(10)
	for i := 0; i < 3; i++ {
		buf.Append(Delivery{ID: idForTest(i)})
	}
	require.Equal(t, 3, buf.Len())
	got := buf.Snapshot(10)
	require.Len(t, got, 3)
	// Newest-first: most recently appended is index 0.
	require.Equal(t, idForTest(2), got[0].ID)
	require.Equal(t, idForTest(1), got[1].ID)
	require.Equal(t, idForTest(0), got[2].ID)
}

func TestRingBuffer_LastHundred(t *testing.T) {
	buf := NewRingBuffer(100)
	for i := 0; i < 150; i++ {
		buf.Append(Delivery{ID: idForTest(i)})
	}
	require.Equal(t, 100, buf.Len())
	got := buf.Snapshot(200)
	require.Len(t, got, 100)
	// Newest-first: should be ids 149..50 (last 100).
	require.Equal(t, idForTest(149), got[0].ID)
	require.Equal(t, idForTest(50), got[99].ID)
}

func TestRingBuffer_ConcurrentAppendSnapshot(t *testing.T) {
	buf := NewRingBuffer(100)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				buf.Append(Delivery{ID: "x"})
			}
		}()
	}
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				_ = buf.Snapshot(20)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 100, buf.Len())
}

func TestRingBuffer_LenAccurate(t *testing.T) {
	buf := NewRingBuffer(5)
	require.Equal(t, 0, buf.Len())
	buf.Append(Delivery{})
	require.Equal(t, 1, buf.Len())
	for i := 0; i < 10; i++ {
		buf.Append(Delivery{})
	}
	require.Equal(t, 5, buf.Len())
}

func TestRingBuffer_NewRingBuffer_PanicsOnBadCap(t *testing.T) {
	require.PanicsWithValue(t, "deliveries: RingBuffer cap must be > 0", func() {
		NewRingBuffer(0)
	})
	require.PanicsWithValue(t, "deliveries: RingBuffer cap must be > 0", func() {
		NewRingBuffer(-1)
	})
}

// idForTest is a tiny helper kept in this file to avoid an import of
// strconv at the package-init layer. Use fmt.Sprintf (not a hand-
// rolled itoa) so we don't collide with any other test file's helper.
func idForTest(i int) string { return fmt.Sprintf("id-%d", i) }
