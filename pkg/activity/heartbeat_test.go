package activity

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBatchProgress_JSONSerializable verifies the heartbeat payload round-trips
// through encoding/json (Temporal's default DataConverter). D2-16 + Pitfall 6
// (non-serializable heartbeat) prevention.
func TestBatchProgress_JSONSerializable(t *testing.T) {
	bp := BatchProgress{Action: 3, Total: 5}
	b, err := json.Marshal(bp)
	require.NoError(t, err)
	require.JSONEq(t, `{"action":3,"total":5}`, string(b))

	var got BatchProgress
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, bp, got)
}

// TestBatchProgress_NoFunctionsOrChannels is a sentinel that catches a future
// contributor adding a non-serializable field to BatchProgress. Funcs, channels,
// and unsafe.Pointer cannot pass through encoding/json — Temporal's heartbeat
// converter would panic at runtime. Catch it at compile/test time instead.
func TestBatchProgress_NoFunctionsOrChannels(t *testing.T) {
	ty := reflect.TypeOf(BatchProgress{})
	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		switch f.Type.Kind() {
		case reflect.Func, reflect.Chan, reflect.UnsafePointer:
			t.Fatalf("BatchProgress.%s has non-serializable kind %v — would panic in activity.RecordHeartbeat through default JSON converter (Pitfall 6)",
				f.Name, f.Type.Kind())
		}
	}
}

// fakeHeartbeatEmitter is a test double used by 02-03's ExecuteBatch tests to
// capture per-action emit calls without spinning up TestActivityEnvironment.
// Defined here (next to BatchProgress) so the cache/options tests can also
// reach it inside package activity.
type fakeHeartbeatEmitter struct {
	mu    sync.Mutex
	calls []BatchProgress
}

func (f *fakeHeartbeatEmitter) emit(_ context.Context, progress BatchProgress) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, progress)
}

func (f *fakeHeartbeatEmitter) snapshot() []BatchProgress {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]BatchProgress, len(f.calls))
	copy(out, f.calls)
	return out
}

// TestFakeHeartbeatEmitter_CapturesCalls is a smoke test for the test double
// itself. The real realHeartbeatEmitter integration test (which goes through
// activity.RecordHeartbeat → testsuite listener) is DEFERRED to 02-03 where
// TestActivityEnvironment is already in use. Wiring testsuite here just for
// one assertion inflates 02-02's surface for no extra signal.
func TestFakeHeartbeatEmitter_CapturesCalls(t *testing.T) {
	f := &fakeHeartbeatEmitter{}
	f.emit(context.Background(), BatchProgress{Action: 0, Total: 2})
	f.emit(context.Background(), BatchProgress{Action: 1, Total: 2})
	got := f.snapshot()
	require.Len(t, got, 2)
	require.Equal(t, BatchProgress{Action: 0, Total: 2}, got[0])
	require.Equal(t, BatchProgress{Action: 1, Total: 2}, got[1])
}

// Ensure realHeartbeatEmitter satisfies heartbeatEmitter at compile time. If
// the interface signature drifts, this assignment fails to compile.
var _ heartbeatEmitter = realHeartbeatEmitter{}
var _ heartbeatEmitter = (*fakeHeartbeatEmitter)(nil)
