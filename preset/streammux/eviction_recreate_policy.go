// eviction_recreate_policy.go declares the configurable retry policy for
// the no-sibling eviction-recovery path in StreamMux.
// The policy combines exponential backoff (so persistent faults stop
// hammering the encoder while transient ones recover quickly), a hard
// MaxAttempts ceiling (so a permanent fault retires after a bounded
// number of tries), and a MaxAge sliding-window reset (so a previously-
// retired SenderKey rearms after a quiescent period). All three knobs
// are needed: backoff alone retries forever, ceiling alone retries
// every InitialBackoff seconds without slowing down, sliding-window
// alone has nothing to reset.

package streammux

import (
	"math"
	"time"
)

const (
	defaultEvictionRecreateInitialBackoff    = 5 * time.Second
	defaultEvictionRecreateBackoffMultiplier = 2.0
	defaultEvictionRecreateMaxBackoff        = 60 * time.Second
	defaultEvictionRecreateMaxAttempts       = 5
	defaultEvictionRecreateMaxAge            = 5 * time.Minute
)

// EvictionRecreatePolicy controls how StreamMux retries recreating an
// orphaned Output after an eviction with no surviving sibling.
//
// Backoff escalates exponentially per consecutive-failure count, capped
// at MaxBackoff. After MaxAttempts consecutive failures without a
// MaxAge-long quiescent window, the SenderKey is marked permanently
// failed: a terminal Warn is logged and no further recreate is
// attempted until either (a) a fault-clearing orchestrator action, or
// (b) the SenderKey-specific lastFailureTime is older than MaxAge —
// the sliding window then resets the consecutive-failure counter and
// the next eviction rearms recovery.
//
// The zero value is a valid policy (each field falls back to its
// default via applyDefaults). Set by callers that want different
// pacing — e.g. ffstream's CLI knobs, or tests that need sub-second
// gating.
type EvictionRecreatePolicy struct {
	// InitialBackoff is the wait before the first retry. Default 5s.
	InitialBackoff time.Duration

	// BackoffMultiplier multiplies the wait per consecutive failure.
	// Default 2.0 (5s, 10s, 20s, 40s, 60s under the default cap).
	// Values <= 1.0 fall back to the default — a multiplier of 1
	// means "linear backoff" which is what the policy is replacing,
	// and < 1 has no useful semantics (waits would shrink toward
	// zero).
	BackoffMultiplier float64

	// MaxBackoff caps the exponential growth so the wait does not
	// stretch into hours after enough failures. Default 60s.
	MaxBackoff time.Duration

	// MaxAttempts is the consecutive-failure count at which the
	// SenderKey is marked permanentlyFailed. Default 5. A value of 0
	// or negative falls back to the default — an unbounded retry
	// budget defeats the purpose of the policy.
	MaxAttempts int

	// MaxAge is the sliding-window length: a SenderKey whose last
	// failure is older than MaxAge gets a fresh start (counter and
	// permanentlyFailed flag reset) on the next eviction. Default 5m.
	MaxAge time.Duration
}

// applyDefaults fills in the zero-value fields with their defaults so
// callers can supply a partial policy. Returns the resolved policy by
// value — leaves the receiver untouched (callers may want to inspect
// what was passed in vs. what was used).
func (p EvictionRecreatePolicy) applyDefaults() EvictionRecreatePolicy {
	if p.InitialBackoff <= 0 {
		p.InitialBackoff = defaultEvictionRecreateInitialBackoff
	}
	if p.BackoffMultiplier <= 1.0 {
		p.BackoffMultiplier = defaultEvictionRecreateBackoffMultiplier
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = defaultEvictionRecreateMaxBackoff
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = defaultEvictionRecreateMaxAttempts
	}
	if p.MaxAge <= 0 {
		p.MaxAge = defaultEvictionRecreateMaxAge
	}
	// MaxBackoff < InitialBackoff is a misconfiguration: the cap
	// kicks in before the first wait can complete, defeating the
	// "growing" intent. Promote MaxBackoff to InitialBackoff so the
	// first retry still waits InitialBackoff.
	if p.MaxBackoff < p.InitialBackoff {
		p.MaxBackoff = p.InitialBackoff
	}
	return p
}

// backoffFor returns the wait before the next retry given the count of
// consecutive failures already recorded. consecutiveFailures==0 maps to
// InitialBackoff (the wait before the first retry); consecutiveFailures==1
// to InitialBackoff*Multiplier; etc. The result is clamped at MaxBackoff.
//
// The math.Pow result is finite for all reasonable inputs (Multiplier
// in [1, 10], consecutiveFailures in [0, 100]); for paranoia,
// non-finite or non-positive results fall through to MaxBackoff.
func (p EvictionRecreatePolicy) backoffFor(consecutiveFailures int) time.Duration {
	if consecutiveFailures < 0 {
		// Defensive: a negative count cannot produce a meaningful
		// backoff; treat as the initial wait.
		return p.InitialBackoff
	}
	multiplier := math.Pow(p.BackoffMultiplier, float64(consecutiveFailures))
	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier <= 0 {
		return p.MaxBackoff
	}
	wait := time.Duration(float64(p.InitialBackoff) * multiplier)
	if wait <= 0 || wait > p.MaxBackoff {
		return p.MaxBackoff
	}
	return wait
}
