package audio

import (
	"fmt"
	"unsafe"

	"github.com/asticode/go-astiav"
)

// ExtractSamples extracts samples from a specific channel of an audio frame.
func ExtractSamples(f *astiav.Frame, channel int) ([]float64, error) {
	nbSamples := f.NbSamples()
	format := f.SampleFormat()
	data := f.Data()

	res := make([]float64, nbSamples)
	if format.IsPlanar() {
		plane, err := data.Bytes(channel)
		if err != nil {
			return nil, err
		}
		if len(plane) == 0 {
			return res, nil
		}
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
				res[i] = float64(samples[i]) / 32768.0
			}
		default:
			return nil, fmt.Errorf("unsupported sample format: %v", format)
		}
		return res, nil
	}

	// Packed
	plane, err := data.Bytes(0)
	if err != nil {
		return nil, err
	}
	if len(plane) == 0 {
		return res, nil
	}
	channels := f.ChannelLayout().Channels()
	ptr := unsafe.Pointer(&plane[0])
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
			res[i] = float64(samples[i*channels+channel]) / 32768.0
		}
	default:
		return nil, fmt.Errorf("unsupported sample format: %v", format)
	}
	return res, nil
}

// FillSamples fills a specific channel of an audio frame with samples.
func FillSamples(f *astiav.Frame, channel int, samples []float64) error {
	format := f.SampleFormat()
	data := f.Data()
	nbSamples := f.NbSamples()

	if format.IsPlanar() {
		plane, err := data.Bytes(channel)
		if err != nil {
			return err
		}
		if len(plane) == 0 {
			return nil
		}
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
				out[i] = int16(sample * 32767.0)
			}
		default:
			return fmt.Errorf("unsupported sample format: %v", format)
		}
		return nil
	}

	// Packed
	plane, err := data.Bytes(0)
	if err != nil {
		return err
	}
	if len(plane) == 0 {
		return nil
	}
	channels := f.ChannelLayout().Channels()
	ptr := unsafe.Pointer(&plane[0])
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
			out[i*channels+channel] = int16(sample * 32767.0)
		}
	default:
		return fmt.Errorf("unsupported sample format: %v", format)
	}
	return nil
}
