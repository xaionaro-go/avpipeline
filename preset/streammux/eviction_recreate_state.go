// eviction_recreate_state.go declares the per-SenderKey state tracked
// by the no-sibling eviction-recovery path. State is keyed by the dead
// output's SenderKey so simultaneous evictions of audio + video
// outputs (SplitAV) do not interfere with each other.

package streammux

import "time"

// evictionRecreateState tracks the per-SenderKey recreate budget so
// the no-sibling recovery path can retire a SenderKey after the
// configured MaxAttempts and rearm it once the configured MaxAge
// quiescent window has elapsed.
//
// consecutiveFailures counts attempts since the last MaxAge-long
// quiescent window (NOT total attempts ever). lastFailureTime is the
// wall time of the most recent attempt — both successful and failed
// attempts refresh it, since the sliding window measures "time since
// last try", not "time since last failure". permanentlyFailed latches
// once consecutiveFailures hits MaxAttempts so the terminal Warn fires
// exactly once per fault era.
type evictionRecreateState struct {
	consecutiveFailures int
	lastFailureTime     time.Time
	permanentlyFailed   bool
}
