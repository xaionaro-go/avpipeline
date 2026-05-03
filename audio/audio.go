package audio

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/asticode/go-astiav"
)

// ExtractSamples extracts samples from a specific channel of an audio frame.
func ExtractSamples(f *astiav.Frame, channel int) ([]float64, error) {
	nbSamples := f.NbSamples()
	format := f.SampleFormat()
	channels := f.ChannelLayout().Channels()
	data := f.Data()

	res := make([]float64, nbSamples)
	buf, err := data.Bytes(0)
	if err != nil {
		return nil, err
	}

	switch {
	case format.IsPlanar():
		// Bytes(0) returns all planes concatenated; pick out the
		// requested channel's plane.
		if len(buf)%channels != 0 {
			return nil, fmt.Errorf("planar buffer size %d not divisible by channel count %d", len(buf), channels)
		}
		planeSize := len(buf) / channels
		plane := buf[channel*planeSize : (channel+1)*planeSize]
		switch format {
		case astiav.SampleFormatFltp:
			ptr := unsafe.Pointer(&plane[0])
			samples := unsafe.Slice((*float32)(ptr), nbSamples)
			for i := range nbSamples {
				res[i] = float64(samples[i])
			}
		case astiav.SampleFormatDblp:
			ptr := unsafe.Pointer(&plane[0])
			samples := unsafe.Slice((*float64)(ptr), nbSamples)
			copy(res, samples)
		case astiav.SampleFormatS16P:
			ptr := unsafe.Pointer(&plane[0])
			samples := unsafe.Slice((*int16)(ptr), nbSamples)
			for i := range nbSamples {
				res[i] = float64(samples[i]) / 32767.0
			}
		default:
			return nil, fmt.Errorf("unsupported sample format: %v", format)
		}
	default:
		// Packed: interleaved samples in plane 0; stride is len(channels).
		ptr := unsafe.Pointer(&buf[0])
		switch format {
		case astiav.SampleFormatFlt:
			samples := unsafe.Slice((*float32)(ptr), nbSamples*channels)
			for i := range nbSamples {
				res[i] = float64(samples[i*channels+channel])
			}
		case astiav.SampleFormatDbl:
			samples := unsafe.Slice((*float64)(ptr), nbSamples*channels)
			for i := range nbSamples {
				res[i] = samples[i*channels+channel]
			}
		case astiav.SampleFormatS16:
			samples := unsafe.Slice((*int16)(ptr), nbSamples*channels)
			for i := range nbSamples {
				res[i] = float64(samples[i*channels+channel]) / 32767.0
			}
		default:
			return nil, fmt.Errorf("unsupported sample format: %v", format)
		}
	}
	return res, nil
}

// s16Encode converts a float64 sample in [-1.0, 1.0] to int16 with
// nearest-integer rounding. Symmetric with the /32767.0 decode path
// in ExtractSamples; clamps to int16 range to avoid overflow on
// out-of-range inputs (e.g. exactly 1.0 rounds to 32767).
func s16Encode(sample float64) int16 {
	v := math.Round(sample * 32767.0)
	switch {
	case v > math.MaxInt16:
		return math.MaxInt16
	case v < math.MinInt16:
		return math.MinInt16
	default:
		return int16(v)
	}
}

// FillSamples fills a specific channel of an audio frame with samples.
//
// Implementation note: astiav's FrameData.Bytes returns a fresh Go-owned
// copy of the frame buffer (it goes through SamplesCopyToBuffer), not an
// aliased view of f.c.data. Writes through that copy do not persist.
// The supported write path is FrameData.SetBytes, which copies the full
// multi-plane buffer back into f.c.data via av_samples_copy and requires
// the frame to be writable. This function therefore performs a
// read-modify-write round trip per call.
func FillSamples(f *astiav.Frame, channel int, samples []float64) error {
	if len(samples) == 0 {
		return nil
	}

	format := f.SampleFormat()
	channels := f.ChannelLayout().Channels()
	data := f.Data()
	nbSamples := f.NbSamples()

	buf, err := data.Bytes(0)
	if err != nil {
		return err
	}
	// MakeWritable may COPY the underlying buffer, so it MUST run before
	// SetBytes. Order Bytes -> MakeWritable -> mutate -> SetBytes is safe:
	// `data` is a wrapper around *Frame (survives the writable-copy),
	// `buf` is a Go-owned snapshot (unaffected), and SetBytes writes into
	// the now-writable c.data.
	if err := f.MakeWritable(); err != nil {
		return fmt.Errorf("making frame writable failed: %w", err)
	}

	switch {
	case format.IsPlanar():
		// Bytes(0) returns all planes concatenated; pick out the
		// requested channel's plane and mutate it in place.
		if len(buf)%channels != 0 {
			return fmt.Errorf("planar buffer size %d not divisible by channel count %d", len(buf), channels)
		}
		planeSize := len(buf) / channels
		plane := buf[channel*planeSize : (channel+1)*planeSize]
		ptr := unsafe.Pointer(&plane[0])
		switch format {
		case astiav.SampleFormatFltp:
			out := unsafe.Slice((*float32)(ptr), nbSamples)
			for i, sample := range samples {
				out[i] = float32(sample)
			}
		case astiav.SampleFormatDblp:
			out := unsafe.Slice((*float64)(ptr), nbSamples)
			copy(out, samples)
		case astiav.SampleFormatS16P:
			out := unsafe.Slice((*int16)(ptr), nbSamples)
			for i, sample := range samples {
				out[i] = s16Encode(sample)
			}
		default:
			return fmt.Errorf("unsupported sample format: %v", format)
		}
	default:
		// Packed: interleaved samples in plane 0; stride is len(channels).
		ptr := unsafe.Pointer(&buf[0])
		switch format {
		case astiav.SampleFormatFlt:
			out := unsafe.Slice((*float32)(ptr), nbSamples*channels)
			for i, sample := range samples {
				out[i*channels+channel] = float32(sample)
			}
		case astiav.SampleFormatDbl:
			out := unsafe.Slice((*float64)(ptr), nbSamples*channels)
			for i, sample := range samples {
				out[i*channels+channel] = sample
			}
		case astiav.SampleFormatS16:
			out := unsafe.Slice((*int16)(ptr), nbSamples*channels)
			for i, sample := range samples {
				out[i*channels+channel] = s16Encode(sample)
			}
		default:
			return fmt.Errorf("unsupported sample format: %v", format)
		}
	}

	if err := data.SetBytes(buf, 0); err != nil {
		return fmt.Errorf("persisting samples to frame failed: %w", err)
	}
	return nil
}
