// error.go defines custom error types used within the kernel package.

package kernel

import (
	"errors"
	"fmt"
)

// errEncoderStalled is the internal sentinel returned by the bounded
// SendFrame retry loop after the configured number of EAGAIN-then-drain
// cycles produced no progress. The wrapping caller in sendFrame
// observes the error, increments a stall counter on the streamEncoder,
// and decides whether to bail (drop the frame) or escalate to a full
// encoder Reinit. The error is intentionally not exported: it is a
// hot-path internal flow-control signal, not part of the public API.
//
// See encoder.go: sendFrameWithDrainRetry + sendFrame for the full
// stall-detection / Reinit watchdog (av1_mediacodec stall).
var errEncoderStalled = errors.New("encoder stalled — bailing for reinit")

type ErrNotImplemented struct {
	Err error
}

func (e ErrNotImplemented) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("not implemented: %v", e.Err)
	}
	return "not implemented"
}

func (e ErrNotImplemented) Unwrap() error {
	return e.Err
}

type ErrUnableToSetSendBufferSize struct {
	Size uint
	Err  error
}

func (e ErrUnableToSetSendBufferSize) Error() string {
	return fmt.Sprintf("unable to set send buffer size to %d: %v", e.Size, e.Err)
}

func (e ErrUnableToSetSendBufferSize) Unwrap() error {
	return e.Err
}

// ErrLateStreamAddition signals that a new stream was requested AFTER
// the output muxer's header was already written. Adding a stream at
// that point cannot work safely: the muxer is committed to the stream
// table from WriteHeader, and writing packets for an unknown index
// triggers division-by-zero (SIGFPE) inside av_interleaved_write_frame
// (e.g. mpegts divides by sample_rate=0 when the stream entry was
// created by NewStream(nil) but never had codec parameters populated).
//
// Callers should treat this as a request to recreate the Output kernel
// (close + reopen) rather than silently dropping the packet. When that
// is not feasible, returning the error to the upstream forwarder at
// least surfaces the condition instead of producing a SIGFPE crash.
type ErrLateStreamAddition struct {
	StreamIndex int
}

func (e ErrLateStreamAddition) Error() string {
	return fmt.Sprintf("a new stream (index %d) appeared after the muxer header was already written; the output kernel needs to be recreated", e.StreamIndex)
}
