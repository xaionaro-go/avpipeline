// eviction_recreate_retry.go implements the 1 Hz indefinite retry
// loop for the no-sibling eviction-recovery path.
//
// After the on-eviction recreate fails, the orphaned input stays at
// OutputSwitch.CurrentValue == math.MinInt32 and produces no further
// node-level errors (the SwitchOutput.GetState defense-in-depth path
// drops in-flight frames silently). The retry loop is the only path
// that re-fires the recreate hook in that fault era.
//
// Design constraints (see RETRY_SEMANTICS.md for the full rationale):
//   - 1 Hz cadence, no backoff.
//   - No max-attempts ceiling, no permanently-failed latch.
//   - Indefinite while the input remains orphaned.
//
// Determinism: the per-tick logic is exposed as
// evictionRecreateRetryTick(ctx) so tests can drive it synchronously.
// The Serve-side ticker just wraps that call in a time.Ticker loop.

package streammux

import (
	"context"
	"math"
	"time"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/observability"
)

// retryTickInterval is the cadence of the no-sibling recreate retry
// loop: 1 Hz. See RETRY_SEMANTICS.md "Recreate cadence" for the
// rationale (live-streaming UX cannot tolerate exponential backoff).
const retryTickInterval = time.Second

// evictionRecreateRetryLoop is the Serve-side goroutine that drives
// evictionRecreateRetryTick on a fixed cadence. Exits when ctx is
// canceled (i.e. the StreamMux is being torn down).
func (s *StreamMux[C]) evictionRecreateRetryLoop(
	ctx context.Context,
) {
	logger.Tracef(ctx, "evictionRecreateRetryLoop started")
	defer logger.Tracef(ctx, "/evictionRecreateRetryLoop")

	t := time.NewTicker(retryTickInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.evictionRecreateRetryTick(ctx)
		}
	}
}

// evictionRecreateRetryTick performs one retry-scan pass: walks every
// input, and for each input that is still orphaned (OutputSwitch ==
// MinInt32) re-fires the recreate hook against the SenderKey of the
// most-recently-evicted output for that input.
//
// "Still orphaned" is the success-recovery signal: once a recreate
// (this tick or a prior tick) advances OutputSwitch off MinInt32, the
// input is healed and the next tick is silent.
func (s *StreamMux[C]) evictionRecreateRetryTick(
	ctx context.Context,
) {
	logger.Tracef(ctx, "evictionRecreateRetryTick")
	defer logger.Tracef(ctx, "/evictionRecreateRetryTick")

	if !s.IsAllowedDifferentOutputs() {
		// Without per-input switching the recreate path is a no-op
		// (handleNoSiblingEviction returns early on the same guard).
		// Skip the scan to keep the log quiet.
		return
	}

	if err := s.syncRetryTrackerFromEvictedKeys(ctx); err != nil {
		logger.Debugf(ctx, "evictionRecreateRetryTick: sync retry tracker: %v", err)
	}
	if s.retryTracker == nil {
		return
	}
	if err := s.retryTracker.Tick(ctx, s.currentRouteMember); err != nil {
		logger.Debugf(ctx, "evictionRecreateRetryTick: retry tracker: %v", err)
	}
}

func (s *StreamMux[C]) syncRetryTrackerFromEvictedKeys(
	ctx context.Context,
) error {
	if s.retryTracker == nil {
		return nil
	}
	return s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
		routeID, err := s.routeIDForInput(input)
		if err != nil {
			return err
		}
		if input.OutputSwitch.CurrentValue.Load() != math.MinInt32 {
			s.lastEvictedKey.Delete(input)
			s.retryTracker.MarkRecovered(ctx, routeID)
			return nil
		}
		deadOutputKey, ok := s.lastEvictedKeyFor(input)
		if !ok {
			return nil
		}
		s.retryTracker.RecordDemotion(ctx, routeID, deadOutputKey)
		return nil
	})
}

func (s *StreamMux[C]) currentRouteMember(
	_ context.Context,
	routeID id.RouteID,
) (id.MemberID, bool) {
	input, ok := s.inputForRouteID(routeID)
	if !ok {
		return 0, false
	}
	return id.MemberID(input.OutputSwitch.CurrentValue.Load()), true
}

func (s *StreamMux[C]) recreateEvictedOutputForRoute(
	ctx context.Context,
	routeID id.RouteID,
	deadOutputKey SenderKey,
) error {
	input, ok := s.inputForRouteID(routeID)
	if !ok {
		return nil
	}
	return s.fireRetryRecreate(ctx, input, deadOutputKey)
}

func (s *StreamMux[C]) recordEvictedKey(
	ctx context.Context,
	input *Input[C],
	deadOutputKey SenderKey,
) {
	s.lastEvictedKey.Store(input, deadOutputKey)
	if s.retryTracker == nil {
		return
	}
	routeID, err := s.routeIDForInput(input)
	if err != nil {
		logger.Debugf(ctx, "unable to record retry route for input %s: %v", input.GetType(), err)
		return
	}
	s.retryTracker.RecordDemotion(ctx, routeID, deadOutputKey)
}

// fireRetryRecreate invokes recreateEvictedOutputFunc once for an
// orphaned input on the retry path, logging at Debug on every attempt.
// The once-per-orphan-era Warn is emitted upstream in
// handleNoSiblingEviction; per-tick Debug avoids flooding the log at
// ~86k Warn lines/day while a destination is hard-down.
func (s *StreamMux[C]) fireRetryRecreate(
	ctx context.Context,
	input *Input[C],
	deadOutputKey SenderKey,
) error {
	logger.Debugf(ctx,
		"input %s is still orphaned (1 Hz retry tick); recreating fresh Output under SenderKey %s",
		input.GetType(), deadOutputKey)
	if err := s.recreateEvictedOutputFunc(ctx, input, deadOutputKey); err != nil {
		logger.Debugf(ctx,
			"unable to recreate output for orphaned input %s on retry tick (key %s): %v; will retry on the next 1 Hz tick",
			input.GetType(), deadOutputKey, err)
		return err
	}
	logger.Debugf(ctx,
		"retry-tick recreated and re-attached output %s for orphaned input %s",
		deadOutputKey, input.GetType())
	return nil
}

// startEvictionRecreateRetryLoop spawns the retry-loop goroutine on
// observability.Go so the goroutine inherits cancellation + tracing
// semantics consistent with the rest of the Serve fan-out. Called once
// from StreamMux.Serve.
func (s *StreamMux[C]) startEvictionRecreateRetryLoop(
	ctx context.Context,
) {
	observability.Go(ctx, func(ctx context.Context) {
		s.evictionRecreateRetryLoop(ctx)
	})
}
