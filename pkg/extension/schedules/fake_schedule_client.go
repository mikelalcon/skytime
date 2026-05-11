package schedules

import (
	"context"
	"fmt"
	"sync"

	"go.temporal.io/sdk/client"
)

// FakeScheduleClient is a deterministic test double for
// client.ScheduleClient. It records every Create/Update/Delete call so
// tests can assert exact call shapes; failures are injectable via the
// *Err fields and per-id maps.
//
// Test-only — exported only because Go has no out-of-package whitelist
// mechanism, and Plan 03's pkg/cli tests need it.
type FakeScheduleClient struct {
	mu sync.Mutex

	// Configurable inputs (set by tests before invoking the reconciler).
	ListEntries       []*client.ScheduleListEntry
	ListErr           error
	CreateErr         error
	DescribeResponses map[string]*client.ScheduleDescription
	DescribeErrs      map[string]error
	UpdateErrs        map[string]error
	DeleteErrs        map[string]error

	// Recorded outputs (callers read after-the-fact).
	CreateCalls   []client.ScheduleOptions
	UpdateCalls   []FakeUpdateCall
	DeleteCalls   []string
	DescribeCalls []string
}

// FakeUpdateCall captures one ScheduleHandle.Update invocation.
type FakeUpdateCall struct {
	ScheduleID string
	// Result is whatever DoUpdate returned — the post-callback Schedule
	// shape the reconciler intends to persist.
	Result *client.ScheduleUpdate
}

// NewFakeScheduleClient constructs a fresh fake with empty maps.
func NewFakeScheduleClient() *FakeScheduleClient {
	return &FakeScheduleClient{
		DescribeResponses: map[string]*client.ScheduleDescription{},
		DescribeErrs:      map[string]error{},
		UpdateErrs:        map[string]error{},
		DeleteErrs:        map[string]error{},
	}
}

// Create records the options and returns a handle keyed by options.ID.
// When CreateErr is non-nil, returns (nil, CreateErr) without recording.
func (f *FakeScheduleClient) Create(ctx context.Context, options client.ScheduleOptions) (client.ScheduleHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	f.CreateCalls = append(f.CreateCalls, options)
	return &fakeScheduleHandle{client: f, id: options.ID}, nil
}

// List returns an iterator over a snapshot of ListEntries. The snapshot
// shields concurrent test code from races between iteration and field
// mutation.
func (f *FakeScheduleClient) List(ctx context.Context, options client.ScheduleListOptions) (client.ScheduleListIterator, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return &fakeScheduleListIterator{entries: append([]*client.ScheduleListEntry(nil), f.ListEntries...)}, nil
}

// GetHandle returns a handle bound to the supplied scheduleID. The SDK
// contract does NOT validate existence on GetHandle — methods like
// Describe will surface an error if the schedule does not exist; we
// mirror that here (handles are always returned; methods can fail).
func (f *FakeScheduleClient) GetHandle(ctx context.Context, scheduleID string) client.ScheduleHandle {
	return &fakeScheduleHandle{client: f, id: scheduleID}
}

type fakeScheduleListIterator struct {
	entries []*client.ScheduleListEntry
	idx     int
}

func (it *fakeScheduleListIterator) HasNext() bool { return it.idx < len(it.entries) }

func (it *fakeScheduleListIterator) Next() (*client.ScheduleListEntry, error) {
	if it.idx >= len(it.entries) {
		// Pitfall 4 hygiene: callers that misuse the iterator get a
		// clear nil + no-error rather than a panic.
		return nil, nil
	}
	e := it.entries[it.idx]
	it.idx++
	return e, nil
}

type fakeScheduleHandle struct {
	client *FakeScheduleClient
	id     string
}

func (h *fakeScheduleHandle) GetID() string { return h.id }

func (h *fakeScheduleHandle) Delete(ctx context.Context) error {
	h.client.mu.Lock()
	if err := h.client.DeleteErrs[h.id]; err != nil {
		h.client.mu.Unlock()
		return err
	}
	h.client.DeleteCalls = append(h.client.DeleteCalls, h.id)
	h.client.mu.Unlock()
	return nil
}

func (h *fakeScheduleHandle) Update(ctx context.Context, options client.ScheduleUpdateOptions) error {
	// Mimic the SDK's contract: call DoUpdate with an input.Description
	// synthesized from the Describe map (if present); record what
	// DoUpdate returns. Forced-failure (UpdateErrs[id]) wins over a
	// successful DoUpdate so tests can model gRPC-side failures
	// independently of the callback.
	var desc client.ScheduleDescription
	h.client.mu.Lock()
	if d, ok := h.client.DescribeResponses[h.id]; ok && d != nil {
		desc = *d
	}
	h.client.mu.Unlock()

	input := client.ScheduleUpdateInput{Description: desc}
	upd, err := options.DoUpdate(input)
	if err != nil {
		return err
	}

	h.client.mu.Lock()
	defer h.client.mu.Unlock()
	if forced := h.client.UpdateErrs[h.id]; forced != nil {
		return forced
	}
	h.client.UpdateCalls = append(h.client.UpdateCalls, FakeUpdateCall{ScheduleID: h.id, Result: upd})
	return nil
}

func (h *fakeScheduleHandle) Describe(ctx context.Context) (*client.ScheduleDescription, error) {
	h.client.mu.Lock()
	defer h.client.mu.Unlock()
	h.client.DescribeCalls = append(h.client.DescribeCalls, h.id)
	if err := h.client.DescribeErrs[h.id]; err != nil {
		return nil, err
	}
	d, ok := h.client.DescribeResponses[h.id]
	if !ok {
		return nil, fmt.Errorf("fake schedule client: no Describe response configured for %q", h.id)
	}
	return d, nil
}

// The fake intentionally omits Backfill/Trigger/Pause/Unpause — the
// reconciler does not call them, and stub implementations would just
// complicate the call-shape assertions. If a future task needs them,
// panic with "not implemented" to surface the gap explicitly.
func (h *fakeScheduleHandle) Backfill(ctx context.Context, options client.ScheduleBackfillOptions) error {
	panic("FakeScheduleHandle.Backfill: not implemented (Plan 02 reconciler does not call this)")
}

func (h *fakeScheduleHandle) Trigger(ctx context.Context, options client.ScheduleTriggerOptions) error {
	panic("FakeScheduleHandle.Trigger: not implemented (Plan 02 reconciler does not call this)")
}

func (h *fakeScheduleHandle) Pause(ctx context.Context, options client.SchedulePauseOptions) error {
	panic("FakeScheduleHandle.Pause: not implemented (Plan 02 reconciler does not call this)")
}

func (h *fakeScheduleHandle) Unpause(ctx context.Context, options client.ScheduleUnpauseOptions) error {
	panic("FakeScheduleHandle.Unpause: not implemented (Plan 02 reconciler does not call this)")
}

// Compile-time satisfaction checks.
var _ client.ScheduleClient = (*FakeScheduleClient)(nil)
var _ client.ScheduleHandle = (*fakeScheduleHandle)(nil)
var _ client.ScheduleListIterator = (*fakeScheduleListIterator)(nil)
