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

	if foreachErr := s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
		if input.OutputSwitch.CurrentValue.Load() != math.MinInt32 {
			// Input has recovered (sibling recommit, prior recreate,
			// or external action) — nothing to do this tick.
			return nil
		}
		deadOutputKey, ok := s.lastEvictedKeyFor(input)
		if !ok {
			// Input is at MinInt32 but no eviction key was recorded —
			// this is the initial-state OutputSwitch (set in
			// initSwitches before any AddInput RPC), not an orphaned
			// post-eviction state. Skip.
			return nil
		}
		s.fireRetryRecreate(ctx, input, deadOutputKey)
		return nil
	}); foreachErr != nil {
		logger.Debugf(ctx, "evictionRecreateRetryTick: ForEachInput: %v", foreachErr)
	}
}

// fireRetryRecreate runs the recreate hook for one orphaned input on
// the retry path. Logs at Warn on the first retry attempt for an
// orphan era (so the user-visible diagnostic surfaces once); Debug on
// subsequent attempts (so the 1 Hz cadence does not flood the log
// while the destination is hard-down).
func (s *StreamMux[C]) fireRetryRecreate(
	ctx context.Context,
	input *Input[C],
	deadOutputKey SenderKey,
) {
	logger.Debugf(ctx,
		"input %s is still orphaned (1 Hz retry tick); recreating fresh Output under SenderKey %s",
		input.GetType(), deadOutputKey)
	if err := s.recreateEvictedOutputFunc(ctx, input, deadOutputKey); err != nil {
		logger.Debugf(ctx,
			"unable to recreate output for orphaned input %s on retry tick (key %s): %v; will retry on the next 1 Hz tick",
			input.GetType(), deadOutputKey, err)
		return
	}
	logger.Debugf(ctx,
		"retry-tick recreated and re-attached output %s for orphaned input %s",
		deadOutputKey, input.GetType())
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
