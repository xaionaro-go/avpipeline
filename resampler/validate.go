// validate.go validates PCMAudioFormat values used to construct a Resampler.

package resampler

import (
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
)

// validateOutputFormat returns a descriptive error if the given output PCM
// audio format would cause libav to reject downstream allocations. The
// individual checks correspond to the EINVAL conditions in
// av_audio_fifo_alloc, av_samples_get_buffer_size, and av_frame_get_buffer.
// Returning a typed error here lets the daemon log identify exactly which
// field is unset, instead of bubbling an opaque "Invalid argument" from C.
func validateOutputFormat(out codec.PCMAudioFormat) error {
	var errs []error
	if out.SampleFormat == astiav.SampleFormatNone {
		errs = append(errs, fmt.Errorf("SampleFormat is unset (none)"))
	}
	if out.SampleRate <= 0 {
		errs = append(errs, fmt.Errorf("SampleRate is %d (must be > 0)", out.SampleRate))
	}
	if out.ChannelLayout.Channels() <= 0 {
		errs = append(errs, fmt.Errorf("ChannelLayout has %d channels (must be > 0; layout=%s, order=%v)",
			out.ChannelLayout.Channels(), out.ChannelLayout, out.ChannelLayout.Order()))
	}
	if out.ChunkSize <= 0 {
		errs = append(errs, fmt.Errorf("ChunkSize is %d (must be > 0; for AAC the encoder context's frame_size is populated only after avcodec_open2 succeeds)", out.ChunkSize))
	}
	return errors.Join(errs...)
}
