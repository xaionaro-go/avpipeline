package router

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOnDemandActivator is a test double that records every Activate and
// Deactivate call and lets tests synchronize on those transitions.
type fakeOnDemandActivator struct {
	activateCount   atomic.Int32
	deactivateCount atomic.Int32
	activatedCh     chan struct{}
	deactivatedCh   chan struct{}

	mu           sync.Mutex
	activateErr  error
	onActivate   func()
	onDeactivate func()
}

func newFakeOnDemandActivator() *fakeOnDemandActivator {
	return &fakeOnDemandActivator{
		activatedCh:   make(chan struct{}, 16),
		deactivatedCh: make(chan struct{}, 16),
	}
}

func (f *fakeOnDemandActivator) Activate(ctx context.Context) error {
	f.mu.Lock()
	err := f.activateErr
	cb := f.onActivate
	f.mu.Unlock()
	if err != nil {
		return err
	}
	f.activateCount.Add(1)
	if cb != nil {
		cb()
	}
	f.activatedCh <- struct{}{}
	return nil
}

func (f *fakeOnDemandActivator) Deactivate(ctx context.Context) error {
	f.mu.Lock()
	cb := f.onDeactivate
	f.mu.Unlock()
	f.deactivateCount.Add(1)
	if cb != nil {
		cb()
	}
	f.deactivatedCh <- struct{}{}
	return nil
}

func (f *fakeOnDemandActivator) waitActivated(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-f.activatedCh:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for Activate; count=%d", f.activateCount.Load())
	}
}

func (f *fakeOnDemandActivator) waitDeactivated(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-f.deactivatedCh:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for Deactivate; count=%d", f.deactivateCount.Load())
	}
}

func TestOnDemandActivator_ActivateOnFirstConsumer(t *testing.T) {
	ctx := context.Background()
	fake := newFakeOnDemandActivator()
	tracker := NewOnDemandConsumerTracker(fake, time.Second)

	require.False(t, tracker.IsActive(), "tracker must start inactive")
	require.Equal(t, int32(0), fake.activateCount.Load(), "no activation before any consumer")

	require.NoError(t, tracker.OnConsumerAdded(ctx))
	fake.waitActivated(t, time.Second)
	assert.True(t, tracker.IsActive(), "first consumer must activate")
	assert.Equal(t, int32(1), fake.activateCount.Load(), "Activate called exactly once on 0->1")
	assert.Equal(t, 1, tracker.ConsumerCount())

	// Second consumer must not re-activate.
	require.NoError(t, tracker.OnConsumerAdded(ctx))
	assert.True(t, tracker.IsActive())
	assert.Equal(t, int32(1), fake.activateCount.Load(), "Activate must not be called again on 1->2")
	assert.Equal(t, 2, tracker.ConsumerCount())
}

func TestOnDemandActivator_DeactivateAfterIdleTimeout(t *testing.T) {
	ctx := context.Background()
	fake := newFakeOnDemandActivator()
	idleTimeout := 50 * time.Millisecond
	tracker := NewOnDemandConsumerTracker(fake, idleTimeout)

	require.NoError(t, tracker.OnConsumerAdded(ctx))
	fake.waitActivated(t, time.Second)

	require.NoError(t, tracker.OnConsumerRemoved(ctx))
	assert.Equal(t, 0, tracker.ConsumerCount(),
		"count must reach zero immediately on last consumer removal")
	assert.True(t, tracker.IsActive(),
		"tracker must still report active during the idle window")
	assert.Equal(t, int32(0), fake.deactivateCount.Load(),
		"Deactivate must not be called before the idle timeout elapses")

	fake.waitDeactivated(t, time.Second)
	assert.Equal(t, int32(1), fake.deactivateCount.Load(),
		"Deactivate called exactly once after idle timeout")

	// Settle the state flip.
	assert.Eventually(t, func() bool { return !tracker.IsActive() },
		time.Second, 5*time.Millisecond,
		"tracker must report inactive after Deactivate")
}

func TestOnDemandActivator_CancelIdleOnNewConsumer(t *testing.T) {
	ctx := context.Background()
	fake := newFakeOnDemandActivator()
	idleTimeout := 100 * time.Millisecond
	tracker := NewOnDemandConsumerTracker(fake, idleTimeout)

	require.NoError(t, tracker.OnConsumerAdded(ctx))
	fake.waitActivated(t, time.Second)

	require.NoError(t, tracker.OnConsumerRemoved(ctx))
	assert.True(t, tracker.IsActive(), "active during idle window")

	// Re-add a consumer before the idle timer elapses.
	time.Sleep(idleTimeout / 4)
	require.NoError(t, tracker.OnConsumerAdded(ctx))
	assert.True(t, tracker.IsActive(),
		"tracker must still be active after consumer re-adds during idle window")
	assert.Equal(t, int32(1), fake.activateCount.Load(),
		"Activate must not be called again — the pipeline is already running")

	// Wait longer than the original idle timeout — Deactivate must not fire.
	time.Sleep(2 * idleTimeout)
	assert.Equal(t, int32(0), fake.deactivateCount.Load(),
		"Deactivate must not be called: idle timer was cancelled")
	assert.True(t, tracker.IsActive())
	assert.Equal(t, 1, tracker.ConsumerCount())
}

func TestOnDemandActivator_RemoveWithoutAddReturnsError(t *testing.T) {
	ctx := context.Background()
	fake := newFakeOnDemandActivator()
	tracker := NewOnDemandConsumerTracker(fake, time.Second)

	err := tracker.OnConsumerRemoved(ctx)
	assert.ErrorIs(t, err, ErrConsumerNotFound{},
		"removing with zero consumers must return ErrConsumerNotFound")
	assert.Equal(t, int32(0), fake.activateCount.Load())
	assert.Equal(t, int32(0), fake.deactivateCount.Load())
}
