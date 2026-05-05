package testing

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAttemptCounter_IncrementsPerKey(t *testing.T) {
	a := NewAttemptCounter()
	keyA := ActionKey{FlowName: "users", StepIdx: 0, ActionIdx: 0}
	keyB := ActionKey{FlowName: "users", StepIdx: 1, ActionIdx: 0}

	assert.Equal(t, 1, a.NextFor(keyA))
	assert.Equal(t, 2, a.NextFor(keyA))
	assert.Equal(t, 3, a.NextFor(keyA))

	// Independent slot
	assert.Equal(t, 1, a.NextFor(keyB))
	assert.Equal(t, 4, a.NextFor(keyA))
}

func TestAttemptCounter_RaceClean(t *testing.T) {
	a := NewAttemptCounter()
	key := ActionKey{FlowName: "x"}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.NextFor(key) }()
	}
	wg.Wait()
	snap := a.Snapshot()
	assert.Equal(t, 100, snap[key])
}

func TestAttemptCounter_SnapshotIsCopy(t *testing.T) {
	a := NewAttemptCounter()
	key := ActionKey{FlowName: "users"}
	a.NextFor(key)
	snap := a.Snapshot()
	snap[key] = 9999
	// Internal counter unchanged.
	assert.Equal(t, 2, a.NextFor(key))
}

func TestAttemptCounter_FreshCounterStartsAtOne(t *testing.T) {
	a := NewAttemptCounter()
	key := ActionKey{FlowName: "x"}
	assert.Equal(t, 1, a.NextFor(key))
}
