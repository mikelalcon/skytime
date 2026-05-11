package schedules_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/extension/schedules"
)

// TestFakeScheduleClient_CreateRecords — Create() records the supplied
// ScheduleOptions in .CreateCalls and returns a handle whose GetID() is
// the option's ID.
func TestFakeScheduleClient_CreateRecords(t *testing.T) {
	f := schedules.NewFakeScheduleClient()
	opts := client.ScheduleOptions{ID: "skytime/foo/abc12345"}

	h, err := f.Create(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, h)
	require.Equal(t, "skytime/foo/abc12345", h.GetID())

	require.Len(t, f.CreateCalls, 1)
	require.Equal(t, "skytime/foo/abc12345", f.CreateCalls[0].ID)
}

// TestFakeScheduleClient_CreateErr — configurable CreateErr forces every
// Create call to fail (used in TestReconcile_AccumulatesFailures /
// TestReconcile_AlreadyExistsIsNonFatal).
func TestFakeScheduleClient_CreateErr(t *testing.T) {
	f := schedules.NewFakeScheduleClient()
	f.CreateErr = errors.New("forced create failure")

	h, err := f.Create(context.Background(), client.ScheduleOptions{ID: "x"})
	require.Error(t, err)
	require.Equal(t, "forced create failure", err.Error())
	require.Nil(t, h)
	require.Empty(t, f.CreateCalls, "failed Create should NOT be recorded")
}

// TestFakeScheduleClient_ListEmpty — empty ListEntries yields an iterator
// whose HasNext() is immediately false; Next() returns (nil, nil) without
// panicking (Pitfall 4 hygiene).
func TestFakeScheduleClient_ListEmpty(t *testing.T) {
	f := schedules.NewFakeScheduleClient()

	it, err := f.List(context.Background(), client.ScheduleListOptions{})
	require.NoError(t, err)
	require.NotNil(t, it)
	require.False(t, it.HasNext())

	entry, nextErr := it.Next()
	require.NoError(t, nextErr)
	require.Nil(t, entry)
}

// TestFakeScheduleClient_ListIterates — 3 entries are yielded in
// registration order via HasNext/Next; a 4th Next() after HasNext==false
// returns (nil, nil) (no panic).
func TestFakeScheduleClient_ListIterates(t *testing.T) {
	f := schedules.NewFakeScheduleClient()
	f.ListEntries = []*client.ScheduleListEntry{
		{ID: "skytime/a/1"},
		{ID: "skytime/b/2"},
		{ID: "skytime/c/3"},
	}

	it, err := f.List(context.Background(), client.ScheduleListOptions{})
	require.NoError(t, err)

	got := make([]string, 0, 3)
	for it.HasNext() {
		entry, nextErr := it.Next()
		require.NoError(t, nextErr)
		require.NotNil(t, entry)
		got = append(got, entry.ID)
	}
	assert.Equal(t, []string{"skytime/a/1", "skytime/b/2", "skytime/c/3"}, got)

	// Misuse: another Next() after the iterator is drained must NOT
	// panic — Pitfall 4 hygiene contract.
	entry, nextErr := it.Next()
	require.NoError(t, nextErr)
	require.Nil(t, entry)
}

// TestFakeScheduleClient_GetHandleUpdate — Update(opts.DoUpdate) is
// invoked with a synthesized ScheduleUpdateInput whose Description.Schedule
// matches the configured DescribeResponses entry; the returned *ScheduleUpdate
// is recorded in .UpdateCalls.
func TestFakeScheduleClient_GetHandleUpdate(t *testing.T) {
	f := schedules.NewFakeScheduleClient()

	// Configure a Describe response so the synthesized input.Description
	// is non-zero — exercises the State preservation pathway tested by
	// Task 3's TestReconcile_UpdatePreservesState.
	state := &client.ScheduleState{Paused: true, Note: "operator paused"}
	f.DescribeResponses["skytime/foo/1"] = &client.ScheduleDescription{
		Schedule: client.Schedule{State: state},
	}

	h := f.GetHandle(context.Background(), "skytime/foo/1")
	require.Equal(t, "skytime/foo/1", h.GetID())

	var seenInputState *client.ScheduleState
	err := h.Update(context.Background(), client.ScheduleUpdateOptions{
		DoUpdate: func(in client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			seenInputState = in.Description.Schedule.State
			return &client.ScheduleUpdate{
				Schedule: &client.Schedule{State: in.Description.Schedule.State},
			}, nil
		},
	})
	require.NoError(t, err)

	// DoUpdate must have observed the configured State.
	require.NotNil(t, seenInputState)
	assert.True(t, seenInputState.Paused)
	assert.Equal(t, "operator paused", seenInputState.Note)

	// Update call recorded with the DoUpdate result.
	require.Len(t, f.UpdateCalls, 1)
	assert.Equal(t, "skytime/foo/1", f.UpdateCalls[0].ScheduleID)
	require.NotNil(t, f.UpdateCalls[0].Result)
	require.NotNil(t, f.UpdateCalls[0].Result.Schedule)
	require.NotNil(t, f.UpdateCalls[0].Result.Schedule.State)
	assert.True(t, f.UpdateCalls[0].Result.Schedule.State.Paused)
}

// TestFakeScheduleClient_GetHandleDelete — Delete records the ID in
// .DeleteCalls; injected DeleteErrs[id] forces a per-id failure.
func TestFakeScheduleClient_GetHandleDelete(t *testing.T) {
	f := schedules.NewFakeScheduleClient()

	h := f.GetHandle(context.Background(), "skytime/foo/abc")
	require.NoError(t, h.Delete(context.Background()))
	require.Equal(t, []string{"skytime/foo/abc"}, f.DeleteCalls)

	// Injected per-id failure.
	f.DeleteErrs["skytime/foo/bad"] = errors.New("forced delete failure")
	h2 := f.GetHandle(context.Background(), "skytime/foo/bad")
	err := h2.Delete(context.Background())
	require.Error(t, err)
	require.Equal(t, "forced delete failure", err.Error())
	// Failure should NOT add a successful entry.
	require.Equal(t, []string{"skytime/foo/abc"}, f.DeleteCalls)
}
