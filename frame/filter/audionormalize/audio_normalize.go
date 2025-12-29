package audionormalize

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unsafe"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

type AudioNormalize struct {
	TargetPeak float64
	MaxGain    float64
	MaxSeen    float64
	Locker     xsync.Mutex
	Scratch    []byte
}

var _ condition.Condition = (*AudioNormalize)(nil)

func New(
	targetPeak float64,
	maxGain float64,
) (*AudioNormalize, error) {
	if targetPeak <= 0 || targetPeak > 1 {
		return nil, fmt.Errorf("targetPeak must be in (0,1], got %v", targetPeak)
	}
	return &AudioNormalize{
		TargetPeak: targetPeak,
		MaxGain:    maxGain,
	}, nil
}

func (k *AudioNormalize) String() string {
	return fmt.Sprintf("AudioNormalize(TargetPeak=%v, MaxGain=%v; MaxSeen=%v)", k.TargetPeak, k.MaxGain, k.MaxSeen)
}

func (k *AudioNormalize) Match(
	ctx context.Context,
	input frame.Input,
) (_ret bool) {
	logger.Tracef(ctx, "Match")
	defer func() { logger.Tracef(ctx, "/Match: %v", _ret) }()
	switch input.GetMediaType() {
	case astiav.MediaTypeAudio:
		err := xsync.DoA2R1(ctx, &k.Locker, k.normalizeAudioFrame, ctx, input)
		if err != nil {
			logger.Errorf(ctx, "AudioNormalize: unable to normalize audio frame: %v", err)
		}
	}
	return true
}

func (k *AudioNormalize) Reset(
	ctx context.Context,
) (_ret error) {
	logger.Tracef(ctx, "Reset")
	defer func() { logger.Tracef(ctx, "/Reset: %v", _ret) }()
	return xsync.DoR1(ctx, &k.Locker, func() error {
		k.MaxSeen = 0
		return nil
	})
}

func (k *AudioNormalize) normalizeAudioFrame(
	ctx context.Context,
	input frame.Input,
) (_err error) {
	logger.Tracef(ctx, "normalizeAudioFrame")
	defer func() { logger.Tracef(ctx, "/normalizeAudioFrame: %v", _err) }()
	f := input.Frame

	if f == nil {
		return fmt.Errorf("nil frame")
	}

	if k.TargetPeak <= 0 || k.TargetPeak > 1 {
		return fmt.Errorf("TargetPeak must be in (0,1], got %v", k.TargetPeak)
	}

	sf := f.SampleFormat()
	ch := f.ChannelLayout().Channels()
	ns := f.NbSamples()
	if ch <= 0 || ns <= 0 {
		return fmt.Errorf("invalid frame parameters: channels=%d nbSamples=%d", ch, ns)
	}

	// Use a stable alignment; we also derive planar stride from the returned buffer size.
	const align = 1

	bufSize, err := f.SamplesBufferSize(align)
	if err != nil {
		return fmt.Errorf("unable to get sample buffer size: %w", err)
	}
	if bufSize <= 0 {
		return nil
	}

	if cap(k.Scratch) < bufSize {
		k.Scratch = make([]byte, bufSize)
	}
	buf := k.Scratch[:bufSize]

	if _, err := f.SamplesCopyToBuffer(buf, align); err != nil {
		return fmt.Errorf("unable to copy samples to buffer: %w", err)
	}

	peak, err := framePeakFromBuffer(buf, sf, ch, ns)
	if err != nil {
		return err
	}
	if peak > k.MaxSeen {
		k.MaxSeen = peak
	}

	gain := 1.0
	if k.MaxSeen > 0 {
		gain = k.TargetPeak / k.MaxSeen
		if k.MaxGain > 0 && gain > k.MaxGain {
			gain = k.MaxGain
		}
	}

	if err := applyGainToBuffer(buf, sf, ch, ns, gain); err != nil {
		return err
	}

	// Make frame writable before overwriting its data. (astiav requires caller to manage writability)
	if err := f.MakeWritable(); err != nil {
		return fmt.Errorf("unable to make frame writable: %w", err)
	}
	if err := f.Data().SetBytes(buf, align); err != nil {
		return fmt.Errorf("unable to set frame data from buffer: %w", err)
	}

	return nil
}

func framePeakFromBuffer(buf []byte, sf astiav.SampleFormat, channels, nbSamples int) (float64, error) {
	name := sf.Name()
	planar := sf.IsPlanar()
	bps := sf.BytesPerSample()

	base := name
	if planar {
		base = strings.TrimSuffix(name, "p")
	}

	// Derive per-plane stride from buf size. We only process the active portion (nbSamples*bps).
	var planeStride int
	if planar {
		if channels <= 0 || len(buf)%channels != 0 {
			return 0, fmt.Errorf("unexpected planar buffer size: len=%d channels=%d", len(buf), channels)
		}
		planeStride = len(buf) / channels
		if planeStride < nbSamples*bps {
			return 0, fmt.Errorf("planar stride too small: stride=%d need=%d", planeStride, nbSamples*bps)
		}
	}

	switch base {
	case "u8":
		return peakU8(buf, planar, planeStride, channels, nbSamples), nil
	case "s16":
		return peakS16(buf, planar, planeStride, channels, nbSamples), nil
	case "s32":
		return peakS32(buf, planar, planeStride, channels, nbSamples), nil
	case "flt":
		return peakF32(buf, planar, planeStride, channels, nbSamples), nil
	case "dbl":
		return peakF64(buf, planar, planeStride, channels, nbSamples), nil
	default:
		return 0, fmt.Errorf("unsupported sample format for manual peak normalization: %q", name)
	}
}

func applyGainToBuffer(buf []byte, sf astiav.SampleFormat, channels, nbSamples int, gain float64) error {
	name := sf.Name()
	planar := sf.IsPlanar()
	bps := sf.BytesPerSample()

	base := name
	if planar {
		base = strings.TrimSuffix(name, "p")
	}

	var planeStride int
	if planar {
		if channels <= 0 || len(buf)%channels != 0 {
			return fmt.Errorf("unexpected planar buffer size: len=%d channels=%d", len(buf), channels)
		}
		planeStride = len(buf) / channels
		if planeStride < nbSamples*bps {
			return fmt.Errorf("planar stride too small: stride=%d need=%d", planeStride, nbSamples*bps)
		}
	}

	switch base {
	case "u8":
		applyU8(buf, planar, planeStride, channels, nbSamples, gain)
		return nil
	case "s16":
		applyS16(buf, planar, planeStride, channels, nbSamples, gain)
		return nil
	case "s32":
		applyS32(buf, planar, planeStride, channels, nbSamples, gain)
		return nil
	case "flt":
		applyF32(buf, planar, planeStride, channels, nbSamples, gain)
		return nil
	case "dbl":
		applyF64(buf, planar, planeStride, channels, nbSamples, gain)
		return nil
	default:
		return fmt.Errorf("unsupported sample format for manual peak normalization: %q", name)
	}
}

func peakU8(buf []byte, planar bool, planeStride, ch, ns int) float64 {
	maxAbs := 0
	if planar {
		active := ns
		for c := range ch {
			p := buf[c*planeStride : c*planeStride+active]
			for _, u := range p {
				s := int(u) - 128
				if s < 0 {
					s = -s
				}
				if s > maxAbs {
					maxAbs = s
				}
			}
		}
	} else {
		for _, u := range buf[:ns*ch] {
			s := int(u) - 128
			if s < 0 {
				s = -s
			}
			if s > maxAbs {
				maxAbs = s
			}
		}
	}
	return float64(maxAbs) / 128.0
}

func peakS16(buf []byte, planar bool, planeStride, ch, ns int) float64 {
	const full = 32768.0
	maxAbs := int32(0)
	if planar {
		activeBytes := ns * 2
		for c := range ch {
			b := buf[c*planeStride : c*planeStride+activeBytes]
			s := unsafe.Slice((*int16)(unsafe.Pointer(&b[0])), ns)
			for _, v16 := range s {
				v := int32(v16)
				if v < 0 {
					v = -v
				}
				if v > maxAbs {
					maxAbs = v
				}
			}
		}
	} else {
		total := ns * ch
		s := unsafe.Slice((*int16)(unsafe.Pointer(&buf[0])), total)
		for _, v16 := range s {
			v := int32(v16)
			if v < 0 {
				v = -v
			}
			if v > maxAbs {
				maxAbs = v
			}
		}
	}
	return float64(maxAbs) / full
}

func peakS32(buf []byte, planar bool, planeStride, ch, ns int) float64 {
	const full = 2147483648.0
	maxAbs := int64(0)
	if planar {
		activeBytes := ns * 4
		for c := range ch {
			b := buf[c*planeStride : c*planeStride+activeBytes]
			s := unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), ns)
			for _, v32 := range s {
				v := int64(v32)
				if v < 0 {
					v = -v
				}
				if v > maxAbs {
					maxAbs = v
				}
			}
		}
	} else {
		total := ns * ch
		s := unsafe.Slice((*int32)(unsafe.Pointer(&buf[0])), total)
		for _, v32 := range s {
			v := int64(v32)
			if v < 0 {
				v = -v
			}
			if v > maxAbs {
				maxAbs = v
			}
		}
	}
	return float64(maxAbs) / full
}

func peakF32(buf []byte, planar bool, planeStride, ch, ns int) float64 {
	maxAbs := 0.0
	if planar {
		activeBytes := ns * 4
		for c := range ch {
			b := buf[c*planeStride : c*planeStride+activeBytes]
			s := unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), ns)
			for _, v := range s {
				a := math.Abs(float64(v))
				if a > maxAbs {
					maxAbs = a
				}
			}
		}
	} else {
		total := ns * ch
		s := unsafe.Slice((*float32)(unsafe.Pointer(&buf[0])), total)
		for _, v := range s {
			a := math.Abs(float64(v))
			if a > maxAbs {
				maxAbs = a
			}
		}
	}
	if maxAbs > 1 {
		return 1
	}
	return maxAbs
}

func peakF64(buf []byte, planar bool, planeStride, ch, ns int) float64 {
	maxAbs := 0.0
	if planar {
		activeBytes := ns * 8
		for c := range ch {
			b := buf[c*planeStride : c*planeStride+activeBytes]
			s := unsafe.Slice((*float64)(unsafe.Pointer(&b[0])), ns)
			for _, v := range s {
				a := math.Abs(v)
				if a > maxAbs {
					maxAbs = a
				}
			}
		}
	} else {
		total := ns * ch
		s := unsafe.Slice((*float64)(unsafe.Pointer(&buf[0])), total)
		for _, v := range s {
			a := math.Abs(v)
			if a > maxAbs {
				maxAbs = a
			}
		}
	}
	if maxAbs > 1 {
		return 1
	}
	return maxAbs
}

func clampF64(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func applyU8(buf []byte, planar bool, planeStride, ch, ns int, gain float64) {
	if planar {
		for c := range ch {
			p := buf[c*planeStride : c*planeStride+ns]
			for i, u := range p {
				s := float64(int(u)-128) * gain
				s = clampF64(s, -128, 127)
				p[i] = uint8(int(math.Round(s)) + 128)
			}
		}
		return
	}
	total := ns * ch
	p := buf[:total]
	for i, u := range p {
		s := float64(int(u)-128) * gain
		s = clampF64(s, -128, 127)
		p[i] = uint8(int(math.Round(s)) + 128)
	}
}

func applyS16(buf []byte, planar bool, planeStride, ch, ns int, gain float64) {
	const lo, hi = -32768.0, 32767.0
	if planar {
		activeBytes := ns * 2
		for c := range ch {
			b := buf[c*planeStride : c*planeStride+activeBytes]
			s := unsafe.Slice((*int16)(unsafe.Pointer(&b[0])), ns)
			for i, v := range s {
				out := clampF64(float64(v)*gain, lo, hi)
				s[i] = int16(int32(math.Round(out)))
			}
		}
		return
	}
	total := ns * ch
	s := unsafe.Slice((*int16)(unsafe.Pointer(&buf[0])), total)
	for i, v := range s {
		out := clampF64(float64(v)*gain, lo, hi)
		s[i] = int16(int32(math.Round(out)))
	}
}

func applyS32(buf []byte, planar bool, planeStride, ch, ns int, gain float64) {
	const lo, hi = -2147483648.0, 2147483647.0
	if planar {
		activeBytes := ns * 4
		for c := range ch {
			b := buf[c*planeStride : c*planeStride+activeBytes]
			s := unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), ns)
			for i, v := range s {
				out := clampF64(float64(v)*gain, lo, hi)
				s[i] = int32(int64(math.Round(out)))
			}
		}
		return
	}
	total := ns * ch
	s := unsafe.Slice((*int32)(unsafe.Pointer(&buf[0])), total)
	for i, v := range s {
		out := clampF64(float64(v)*gain, lo, hi)
		s[i] = int32(int64(math.Round(out)))
	}
}

func applyF32(buf []byte, planar bool, planeStride, ch, ns int, gain float64) {
	if planar {
		activeBytes := ns * 4
		for c := range ch {
			b := buf[c*planeStride : c*planeStride+activeBytes]
			s := unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), ns)
			for i, v := range s {
				out := clampF64(float64(v)*gain, -1, 1)
				s[i] = float32(out)
			}
		}
		return
	}
	total := ns * ch
	s := unsafe.Slice((*float32)(unsafe.Pointer(&buf[0])), total)
	for i, v := range s {
		out := clampF64(float64(v)*gain, -1, 1)
		s[i] = float32(out)
	}
}

func applyF64(buf []byte, planar bool, planeStride, ch, ns int, gain float64) {
	if planar {
		activeBytes := ns * 8
		for c := range ch {
			b := buf[c*planeStride : c*planeStride+activeBytes]
			s := unsafe.Slice((*float64)(unsafe.Pointer(&b[0])), ns)
			for i, v := range s {
				s[i] = clampF64(v*gain, -1, 1)
			}
		}
		return
	}
	total := ns * ch
	s := unsafe.Slice((*float64)(unsafe.Pointer(&buf[0])), total)
	for i, v := range s {
		s[i] = clampF64(v*gain, -1, 1)
	}
}
