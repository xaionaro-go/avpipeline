// microphone_pts_seed.go derives the initial PTS for the AAudio
// microphone capture loop from the process-wide shared monotonic
// epoch (kernel.PTSEpochNanos). Lives outside the android+cgo build
// tag so it is testable on any platform without an AAudio stack.

package android

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/kernel"
)

// initialMicrophonePTS returns the audio-sample-units PTS to seed the
// microphone Generate loop with at the moment Generate is entered.
// The value equals (now - sharedEpoch) converted into 1/sampleRate
// timebase. When sampleRate is non-positive the value is 0 — the
// caller must validate sampleRate before reaching the capture loop,
// so this branch is purely defensive against a misconfigured kernel.
//
// Both this kernel and the Input kernel (camera path) anchor first
// PTS to the same epoch, so independently-opened audio + video
// kernels start at PTS values reflecting their actual cold-start
// offset rather than each restarting at PTS=0. This eliminates the
// 200-2000ms audio-leads-video desync caused by the previous
// "every kernel seeds at 0" behaviour.
func initialMicrophonePTS(sampleRate int) int64 {
	if sampleRate <= 0 {
		return 0
	}
	tb := astiav.NewRational(1, sampleRate)
	return kernel.PTSSinceEpochInTimeBase(tb)
}
