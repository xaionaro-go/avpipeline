// on_demand_activator.go defines the OnDemandActivator interface and a
// consumer-count tracker that applies idle-timeout hysteresis before
// deactivating an activator.

package router

import (
	"context"
	"sync"
	"time"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/observability"
)

// OnDemandActivator is the abstract contract for an entity that can be
// activated and deactivated on demand. It exists so that the routing layer
// can bring pipelines online only while consumers are attached, and tear
// them down after the last consumer leaves.
type OnDemandActivator interface {
	Activate(ctx context.Context) error
	Deactivate(ctx context.Context) error
}

// OnDemandConsumerTracker wraps an OnDemandActivator with consumer-count
// bookkeeping and an idle timer. It is driven by OnConsumerAdded /
// OnConsumerRemoved calls from whatever layer tracks consumers (usually
// Route[T]). Activation happens synchronously when the consumer count
// transitions from zero to non-zero; deactivation is deferred by
// IdleTimeout so that a brief gap between consumers does not tear down
// the underlying pipeline.
type OnDemandConsumerTracker struct {
	Activator   OnDemandActivator
	IdleTimeout time.Duration

	mu            sync.Mutex
	consumerCount int
	isActive      bool
	idleTimer     *time.Timer
}

// NewOnDemandConsumerTracker creates a tracker that starts deactivated,
// with zero consumers, and armed for the given idle timeout.
func NewOnDemandConsumerTracker(
	activator OnDemandActivator,
	idleTimeout time.Duration,
) *OnDemandConsumerTracker {
	return &OnDemandConsumerTracker{
		Activator:   activator,
		IdleTimeout: idleTimeout,
	}
}

// OnConsumerAdded must be called every time a consumer attaches to the
// associated route. It activates the underlying activator on the 0 -> 1
// transition and cancels any pending idle-timer deactivation.
func (t *OnDemandConsumerTracker) OnConsumerAdded(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "OnDemandConsumerTracker.OnConsumerAdded")
	defer func() { logger.Debugf(ctx, "/OnDemandConsumerTracker.OnConsumerAdded: %v", _err) }()

	t.mu.Lock()
	t.consumerCount++
	if t.idleTimer != nil {
		t.idleTimer.Stop()
		t.idleTimer = nil
	}
	shouldActivate := !t.isActive
	if shouldActivate {
		t.isActive = true
	}
	t.mu.Unlock()

	if !shouldActivate {
		return nil
	}
	if err := t.Activator.Activate(ctx); err != nil {
		t.mu.Lock()
		t.consumerCount--
		t.isActive = false
		t.mu.Unlock()
		return err
	}
	return nil
}

// OnConsumerRemoved must be called every time a consumer detaches. On
// the N -> 0 transition it arms an idle timer; if the timer elapses with
// the count still at zero, the underlying activator is deactivated.
func (t *OnDemandConsumerTracker) OnConsumerRemoved(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "OnDemandConsumerTracker.OnConsumerRemoved")
	defer func() { logger.Debugf(ctx, "/OnDemandConsumerTracker.OnConsumerRemoved: %v", _err) }()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.consumerCount <= 0 {
		return ErrConsumerNotFound{}
	}
	t.consumerCount--
	if t.consumerCount > 0 {
		return nil
	}
	if !t.isActive {
		return nil
	}
	if t.idleTimer != nil {
		t.idleTimer.Stop()
	}
	t.idleTimer = time.AfterFunc(t.IdleTimeout, func() {
		// AfterFunc runs the callback on its own goroutine — route it
		// through observability.Go so it participates in the normal
		// goroutine tracking.
		observability.Go(ctx, func(ctx context.Context) {
			t.deactivateIfStillIdle(ctx)
		})
	})
	return nil
}

// deactivateIfStillIdle is invoked from the idle timer. It double-checks
// that no consumer reattached before the timer fired and only then calls
// Deactivate.
func (t *OnDemandConsumerTracker) deactivateIfStillIdle(ctx context.Context) {
	logger.Debugf(ctx, "OnDemandConsumerTracker.deactivateIfStillIdle")
	defer logger.Debugf(ctx, "/OnDemandConsumerTracker.deactivateIfStillIdle")

	t.mu.Lock()
	if t.consumerCount > 0 || !t.isActive {
		t.mu.Unlock()
		return
	}
	t.idleTimer = nil
	t.isActive = false
	t.mu.Unlock()

	if err := t.Activator.Deactivate(ctx); err != nil {
		logger.Errorf(ctx, "unable to deactivate: %v", err)
	}
}

// IsActive returns whether the wrapped activator is currently active.
// Intended for tests and observability.
func (t *OnDemandConsumerTracker) IsActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.isActive
}

// ConsumerCount returns the current number of tracked consumers.
// Intended for tests and observability.
func (t *OnDemandConsumerTracker) ConsumerCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.consumerCount
}
