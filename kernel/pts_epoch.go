// pts_epoch.go provides a process-wide monotonic-clock baseline for
// anchoring first-frame PTS values across independently-opened media
// kernels (camera, microphone, …).
//
// Without a shared epoch, each kernel that seeds its first PTS to 0 at
// open time produces a "first frame appears at t=0" assumption that is
// actually offset by the kernel's own cold-start latency. When a video
// kernel takes 200-2000ms longer to emit its first frame than an audio
// kernel — typical on Android Camera2 vs AAudio — the encoder/muxer
// labels the two first frames as simultaneous, producing a constant
// audio-leads-video desync of that delta.
//
// Anchoring every kernel's first PTS to a shared CLOCK_MONOTONIC origin
// turns the cold-start delta into a benign "first frame at t≈Δ"
// observation, which encoders/muxers handle natively via DTS gaps.
// The clock is monotonic (not wall-clock) so it is immune to NTP
// adjustments, suspend, and other sources of wall-clock discontinuity
// that would corrupt PTS deltas mid-stream.

package kernel

import (
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// MonotonicClock is the abstract monotonic time source used by the
// shared-epoch helpers. Production code uses the real CLOCK_MONOTONIC
// reading (via Go's runtime, which on Linux maps to clock_gettime with
// CLOCK_MONOTONIC); tests can swap in a deterministic fake via
// SetMonotonicClock without racing on real wall-clock advancement.
type MonotonicClock interface {
	// NanosSinceBoot returns nanoseconds elapsed on the monotonic
	// clock since some fixed reference point chosen by the
	// implementation. Successive calls must return non-decreasing
	// values; the absolute value is meaningful only as a difference
	// against another reading from the same clock.
	NanosSinceBoot() int64
}

// realMonotonicClock is the production MonotonicClock backed by Go's
// runtime monotonic reading (time.Now carries a monotonic component;
// time.Since uses it exclusively when the receiver carries one). On
// Linux the Go runtime services this via clock_gettime(CLOCK_MONOTONIC)
// through the VDSO, which is what gives the helpers NTP-step immunity.
type realMonotonicClock struct {
	// origin is captured at process start (or first SetMonotonicClock
	// install of a real clock) and used as the fixed reference point
	// for the NanosSinceBoot delta. The Time value carries the
	// monotonic reading, so time.Since(origin) ignores wall-clock
	// changes between the two readings.
	origin time.Time
}

func newRealMonotonicClock() realMonotonicClock {
	return realMonotonicClock{origin: time.Now()}
}

// NanosSinceBoot implements MonotonicClock by returning nanoseconds
// elapsed on the monotonic clock since the realMonotonicClock origin.
// time.Since walks the monotonic component of the receiver Time, so
// the result is unaffected by wall-clock adjustments.
func (c realMonotonicClock) NanosSinceBoot() int64 {
	return time.Since(c.origin).Nanoseconds()
}

// monotonicClock is the active clock source consulted by every helper
// in this file. atomic.Pointer storage lets callers in any package
// install a fake via SetMonotonicClock without a lock, which keeps the
// hot path (PTSEpochNanos, PTSSinceEpochInTimeBase) lock-free.
var monotonicClock atomic.Pointer[MonotonicClock]

func init() {
	var c MonotonicClock = newRealMonotonicClock()
	monotonicClock.Store(&c)
}

// SetMonotonicClock installs a custom MonotonicClock as the source for
// every helper in this file. It returns the previously-installed clock
// so callers (typically tests) can restore the prior source at the end
// of their scope, e.g.
//
//	prev := kernel.SetMonotonicClock(fake)
//	defer kernel.SetMonotonicClock(prev)
//
// The function is exported so cross-package consumers (microphone,
// future kernels) can inject a fake without touching unexported state.
func SetMonotonicClock(c MonotonicClock) MonotonicClock {
	prev := monotonicClock.Swap(&c)
	if prev == nil {
		return nil
	}
	return *prev
}

// CurrentMonotonicClock returns the currently-installed MonotonicClock.
// Exposed primarily so a test can capture the production clock before
// installing a fake, then restore it via SetMonotonicClock.
func CurrentMonotonicClock() MonotonicClock {
	p := monotonicClock.Load()
	if p == nil {
		return nil
	}
	return *p
}

// ResetPTSEpochForTesting clears the cached process-wide epoch so a
// subsequent PTSEpochNanos call re-initialises against the currently-
// installed MonotonicClock. Exported so cross-package tests (e.g. the
// microphone seed in kernel/extra/android) can drive the helper into
// a clean state without touching unexported atomics. Production code
// must never call this — the epoch is a one-shot baseline for the
// process lifetime.
func ResetPTSEpochForTesting() {
	ptsEpochNanos.Store(0)
}

// nowMonotonicNanos reads the active MonotonicClock. Centralising the
// dereference here keeps the rest of the file free of atomic.Pointer
// noise and gives every PTS helper the same testing seam.
func nowMonotonicNanos() int64 {
	return (*monotonicClock.Load()).NanosSinceBoot()
}

// ptsEpochNanos is the process-wide monotonic-clock epoch used to
// rebase first-frame PTS values from independently-started media
// kernels onto a common origin. The first kernel to call
// PTSEpochNanos initialises this value via CompareAndSwap; every
// subsequent caller observes the same value for the lifetime of the
// process. Values are CLOCK_MONOTONIC nanoseconds (not Unix epoch),
// so deltas survive any wall-clock adjustment.
var ptsEpochNanos atomic.Int64

// PTSEpochNanos returns the shared monotonic-clock baseline. The first
// call performs a CompareAndSwap from 0 to the current monotonic
// nanos; concurrent callers race on the CAS but all observe the
// winning value via the subsequent Load. Subsequent callers return
// the cached epoch directly. The returned value is meaningful only
// as a delta against another reading from the same MonotonicClock.
//
// CROSS-PACKAGE / CROSS-BUILD-TAG CONTRACT
//
// This helper is the cross-package boundary between the
// architecture-neutral kernel package and the build-tagged CGo
// subpackages (kernel/extra/android microphone path runs under
// `//go:build android && cgo` and calls PTSEpochNanos to seed
// initial PTS). The contract observed at that boundary:
//
//   - Goroutine-safe and lock-free on the hot path: the call performs
//     one atomic.Int64 Load and (only on first call per process) one
//     CompareAndSwap. No mutex is taken, no Go runtime scheduler
//     interaction is required beyond the atomics. Safe to call from
//     any goroutine, concurrently, including goroutines started under
//     CGo callback paths (AAudio capture loop in microphone.go).
//
//   - No CGo calls inside this function. The MonotonicClock interface
//     hides the time source; the production realMonotonicClock is
//     pure Go (time.Since on a captured time.Time). CGo subpackages
//     therefore do not pay any cgocall overhead by calling this.
//
//   - One-shot baseline initialization. The first caller in the
//     process wins the CAS and pins the epoch; every later call
//     returns the same value. Tests can reset via
//     ResetPTSEpochForTesting; production code must not.
//
//   - Result is monotonic-clock nanoseconds, NOT Unix epoch. Adding
//     this value to time.Now().UnixNano() is meaningless. Subtract
//     two readings from the same MonotonicClock to get a duration in
//     nanoseconds.
func PTSEpochNanos() int64 {
	if v := ptsEpochNanos.Load(); v != 0 {
		return v
	}
	now := nowMonotonicNanos()
	if now == 0 {
		// A monotonic reading of exactly zero would alias the
		// "uninitialised" sentinel and break every subsequent CAS-vs-
		// Load consumer. Bump it by 1ns so the stored epoch is
		// always non-zero. Callers see a 1ns shift, which is below
		// any audio/video timebase resolution.
		now = 1
	}
	if ptsEpochNanos.CompareAndSwap(0, now) {
		return now
	}
	return ptsEpochNanos.Load()
}

// PTSSinceEpochInTimeBase exposes the monotonic-since-epoch
// conversion in the supplied stream timebase to external kernels
// (e.g. the Android microphone) that seed their first PTS without
// going through the Input.applyPerStreamShift path. The shared epoch
// is initialised on first call to PTSEpochNanos.
//
// Inherits the cross-package / cross-build-tag contract documented on
// PTSEpochNanos: lock-free, no CGo calls, monotonic-nanos delta. Safe
// to call from CGo-subpackage goroutines (microphone capture loop).
func PTSSinceEpochInTimeBase(timeBase astiav.Rational) int64 {
	return ptsSinceEpochInTimeBase(nowMonotonicNanos(), PTSEpochNanos(), timeBase)
}

// resolvePTSShiftTarget translates a configured ForceStartPTS value
// into the concrete PTS target that applyPerStreamShift uses for the
// current stream's first packet:
//
//   - PTSEpoch sentinel → "now since shared epoch" expressed in the
//     stream's own timebase (the monotonic-clock-aligned baseline).
//   - any other value → returned unchanged (legacy literal target).
//
// PTSKeep is not handled here: callers gate on it before calling this
// function (wantPTSShift is false).
func resolvePTSShiftTarget(target int64, timeBase astiav.Rational) int64 {
	if target != globaltypes.PTSEpoch {
		return target
	}
	return ptsSinceEpochInTimeBase(nowMonotonicNanos(), PTSEpochNanos(), timeBase)
}

// ptsSinceEpochInTimeBase converts the monotonic-clock delta (nowNanos
// - epochNanos) into the supplied stream timebase, returning a PTS
// value in that timebase. A zero or invalid timebase returns 0 to
// keep callers safe — they then fall through to their existing pts=0
// path and the worst case is the legacy desync rather than a panic
// or wrap.
func ptsSinceEpochInTimeBase(
	nowNanos int64,
	epochNanos int64,
	timeBase astiav.Rational,
) int64 {
	num := int64(timeBase.Num())
	den := int64(timeBase.Den())
	if num <= 0 || den <= 0 {
		return 0
	}
	delta := nowNanos - epochNanos
	if delta < 0 {
		delta = 0
	}
	// pts_in_tb = (delta_seconds) / (num/den) = delta_nanos * den / num / 1e9
	// Compute den / num first (in int64 ratio) to avoid overflow on the
	// common case num=1: delta * den / 1e9 fits comfortably for any
	// realistic uptime + sample rate.
	return delta * den / num / int64(time.Second)
}
