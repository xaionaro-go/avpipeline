// eviction_recreate_retry.go implements the periodic retry loop that
// re-fires the no-sibling eviction-recovery path for SenderKeys whose
// backoff has elapsed without a fresh eviction event.
//
// After the first eviction the dead Output is detached from s.Outputs
// and the OutputSwitch is demoted to math.MinInt32; subsequent
// in-flight frames are dropped at the SwitchOutput.GetState
// defense-in-depth path WITHOUT producing any new node-level error
// events. Without a recurring trigger the backoff state stays at
// consecutiveFailures=1 forever — only attempt 1/MaxAttempts ever
// fires for a given fault era, and a transient fault that needs more
// than InitialBackoff to clear stays orphaned until the next user
// action drives a fresh eviction.
//
// The retry loop closes that gap: every tick interval it scans
// lastEvictionRecreateState and re-fires the recreate hook for any
// SenderKey whose:
//
//   - backoff has elapsed (state.lastFailureTime + backoffFor() <= now);
//   - permanentlyFailed flag is not yet latched;
//   - corresponding input(s) are still demoted (OutputSwitch ==
//     math.MinInt32) — once a recreate succeeds and OutputSwitch
//     advances, the input is no longer eligible (eliminates the burn-
//     CPU-on-already-resolved-entry case until the MaxAge sliding-
//     window reset cleans the entry).
//
// Determinism: the per-tick logic is exposed as
// evictionRecreateRetryTick(ctx) so tests can drive it synchronously
// against a fake clock. The Serve-side ticker just wraps that call in
// a time.NewTicker loop.

package streammux

import (
	"context"
	"math"
	"time"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/observability"
)

// minRetryTickInterval is the floor for the retry loop's poll cadence,
// independent of EvictionRecreatePolicy.InitialBackoff. A pathological
// InitialBackoff of 0 (rejected by applyDefaults but defended here too)
// or sub-millisecond (test) would otherwise burn CPU spinning on the
// ticker.
const minRetryTickInterval = 100 * time.Millisecond

// evictionRecreateRetryInterval returns the interval at which the
// retry loop scans lastEvictionRecreateState. Half of InitialBackoff
// gives the gate at most one extra interval of latency past the
// scheduled retry instant. Floored at minRetryTickInterval.
func (s *StreamMux[C]) evictionRecreateRetryInterval() time.Duration {
	policy := s.EvictionRecreatePolicy.applyDefaults()
	interval := policy.InitialBackoff / 2
	if interval < minRetryTickInterval {
		return minRetryTickInterval
	}
	return interval
}

// evictionRecreateRetryLoop is the Serve-side goroutine that drives
// evictionRecreateRetryTick on a fixed cadence. Exits when ctx is
// canceled (i.e. the StreamMux is being torn down).
func (s *StreamMux[C]) evictionRecreateRetryLoop(
	ctx context.Context,
) {
	logger.Tracef(ctx, "evictionRecreateRetryLoop started")
	defer logger.Tracef(ctx, "/evictionRecreateRetryLoop")

	interval := s.evictionRecreateRetryInterval()
	t := time.NewTicker(interval)
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

// evictionRecreateRetryTick performs one retry-scan pass. Snapshots
// lastEvictionRecreateState under evictionRecreateLocker, then for each
// candidate that is past its backoff and still has a demoted input,
// fires evaluateEvictionRecreateLocked + recreateEvictedOutputFunc just
// like the on-eviction path (handleNoSiblingEviction).
//
// Snapshot-then-iterate avoids holding evictionRecreateLocker across
// the recreate-hook call (matches the on-eviction path's locking
// discipline; the hook can take s.Locker which itself can take
// evictionRecreateLocker indirectly via test seams).
func (s *StreamMux[C]) evictionRecreateRetryTick(
	ctx context.Context,
) {
	logger.Tracef(ctx, "evictionRecreateRetryTick")
	defer logger.Tracef(ctx, "/evictionRecreateRetryTick")

	if !s.IsAllowedDifferentOutputs() {
		// Without per-input switching, the recreate path is a no-op
		// even when triggered — handleNoSiblingEviction logs and
		// returns. Skip the scan to keep the log quiet.
		return
	}

	policy := s.EvictionRecreatePolicy.applyDefaults()
	now := s.nowFunc()

	candidates := s.snapshotEvictionRecreateRetryCandidates(now, policy)
	for _, key := range candidates {
		s.tryRecreateForRetry(ctx, key, policy, now)
	}
}

// snapshotEvictionRecreateRetryCandidates returns the subset of keys
// in lastEvictionRecreateState that are eligible for a retry pass at
// `now`: not permanently failed, last-failure-time set, and elapsed
// time at-or-past the per-key backoff. Runs under
// evictionRecreateLocker so the snapshot is consistent against
// concurrent on-eviction state mutations.
func (s *StreamMux[C]) snapshotEvictionRecreateRetryCandidates(
	now time.Time,
	policy EvictionRecreatePolicy,
) []SenderKey {
	s.evictionRecreateLocker.Lock()
	defer s.evictionRecreateLocker.Unlock()

	var keys []SenderKey
	for key, state := range s.lastEvictionRecreateState {
		if state.permanentlyFailed {
			continue
		}
		if state.lastFailureTime.IsZero() {
			continue
		}
		backoff := policy.backoffFor(state.consecutiveFailures)
		if now.Sub(state.lastFailureTime) < backoff {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

// tryRecreateForRetry runs the retry path for a single SenderKey: it
// resolves which input(s) the key maps to, verifies at least one is
// still demoted (OutputSwitch == MinInt32), then runs the same
// evaluate-decide-fire sequence as handleNoSiblingEviction.
//
// "Still demoted" is the success-recovery signal: once the prior
// recreate or a sibling recommit advanced OutputSwitch off MinInt32,
// the input has recovered — no need to fire again. The state map
// entry is left in place (stale entries get cleaned by the MaxAge
// sliding-window reset on the next eviction or by the next retry tick
// once permanentlyFailed has latched).
func (s *StreamMux[C]) tryRecreateForRetry(
	ctx context.Context,
	deadOutputKey SenderKey,
	policy EvictionRecreatePolicy,
	now time.Time,
) {
	// Resolve the input that owns this SenderKey under the current
	// MuxMode. getInputsForSenderKey is the same decomposition used
	// at output-creation time, so retry threads through the same
	// input-selection contract.
	inputsAndKeys, err := s.getInputsForSenderKey(ctx, deadOutputKey)
	if err != nil {
		logger.Debugf(ctx, "evictionRecreateRetryTick: getInputsForSenderKey(%s): %v", deadOutputKey, err)
		return
	}

	// Find any input that is still demoted. SplitAV inputs map 1:1
	// to a split SenderKey, so the loop is at most 1-1 in practice.
	var demotedInput *Input[C]
	for _, ik := range inputsAndKeys {
		if ik.Input == nil {
			continue
		}
		if ik.Input.OutputSwitch.CurrentValue.Load() == math.MinInt32 {
			demotedInput = ik.Input
			break
		}
	}
	if demotedInput == nil {
		// Input(s) recovered (sibling recommit, prior retry, or
		// external action moved the switch off MinInt32) — nothing
		// to do this tick. Leaving the state entry in place is fine:
		// MaxAge will reset it via the on-eviction path's sliding
		// window, and a fresh fault era starts cleanly.
		return
	}

	// Same evaluate-decide-fire ceremony as handleNoSiblingEviction
	// so the state machine is unified across the eviction-driven and
	// timer-driven entry points.
	decision, state := s.evaluateEvictionRecreateLocked(deadOutputKey, policy, now)
	switch decision.kind {
	case evictionRecreateDecisionAlreadyPermanent:
		// Latched between snapshot and lock — drop quietly.
		return
	case evictionRecreateDecisionSkipBackoff:
		// Race against concurrent on-eviction tick — drop quietly.
		return
	case evictionRecreateDecisionFire, evictionRecreateDecisionFireFinal:
		// fall through
	}

	logger.Warnf(ctx,
		"input %s is still orphaned (timer-driven retry tick); recreating fresh Output under SenderKey %s (attempt %d/%d)",
		demotedInput.GetType(), deadOutputKey, state.consecutiveFailures, policy.MaxAttempts)
	if err := s.recreateEvictedOutputFunc(ctx, demotedInput, deadOutputKey); err != nil {
		logger.Warnf(ctx,
			"unable to recreate output for orphaned input %s on retry tick (key %s): %v; remaining demoted until backoff elapses",
			demotedInput.GetType(), deadOutputKey, err)
	} else {
		logger.Debugf(ctx, "retry-tick recreated and re-attached output %s for orphaned input %s", deadOutputKey, demotedInput.GetType())
	}

	if decision.kind == evictionRecreateDecisionFireFinal {
		logger.Warnf(ctx,
			"input %s reached EvictionRecreatePolicy.MaxAttempts=%d on timer-driven retry for SenderKey %s; marked permanently failed, recreate disabled until quiescent for MaxAge=%s",
			demotedInput.GetType(), policy.MaxAttempts, deadOutputKey, policy.MaxAge)
	}
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

