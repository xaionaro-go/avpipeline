//go:build android && cgo
// +build android,cgo

// microphone.go implements Android microphone capture.

package android

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/internal"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/ndk/audio/al"
	"github.com/xaionaro-go/ndk/audio/alc"
	"github.com/xaionaro-go/xsync"
)

type Microphone struct {
	*closuresignaler.ClosureSignaler
	Config MicrophoneConfig
	Locker xsync.Mutex

	streamInfo  *frame.StreamInfo
	codecParams *astiav.CodecParameters
	device      *alc.CaptureDevice

	bytesPerSample int
	bufferSamples  int
	formatContext  *astiav.FormatContext
}

var _ kerneltypes.Abstract = (*Microphone)(nil)

func NewMicrophone(ctx context.Context, cfg MicrophoneConfig) (*Microphone, error) {
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = microphoneDefaultSampleRate
	}
	if cfg.Channels <= 0 {
		cfg.Channels = microphoneDefaultChannels
	}
	if cfg.FrameSamples <= 0 {
		cfg.FrameSamples = microphoneDefaultFrameSamples
	}
	if cfg.BufferSamples <= 0 {
		cfg.BufferSamples = microphoneDefaultBufferSamples
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = microphoneDefaultPollInterval
	}
	if cfg.SampleFormat == astiav.SampleFormatNone {
		cfg.SampleFormat = astiav.SampleFormatS16
	}

	bytesPerSample, err := bytesPerSample(cfg.SampleFormat)
	if err != nil {
		return nil, err
	}
	if cfg.FrameSamples <= 0 {
		return nil, fmt.Errorf("frame samples must be positive")
	}
	if cfg.BufferSamples < cfg.FrameSamples {
		cfg.BufferSamples = cfg.FrameSamples
	}

	channelLayout, err := channelLayoutFromCount(cfg.Channels)
	if err != nil {
		return nil, err
	}

	codecParams := astiav.AllocCodecParameters()
	if codecParams == nil {
		return nil, fmt.Errorf("unable to allocate codec parameters")
	}
	internal.SetFinalizerFree(ctx, codecParams)
	codecParams.SetMediaType(astiav.MediaTypeAudio)
	codecParams.SetCodecID(codecIDFromSampleFormat(cfg.SampleFormat))
	codecParams.SetSampleFormat(cfg.SampleFormat)
	codecParams.SetSampleRate(cfg.SampleRate)
	codecParams.SetChannelLayout(channelLayout)

	k := &Microphone{
		ClosureSignaler: closuresignaler.New(),
		Config:          cfg,
		codecParams:     codecParams,
		bytesPerSample:  bytesPerSample,
		bufferSamples:   cfg.BufferSamples,
		formatContext:   astiav.AllocFormatContext(),
	}
	if k.formatContext == nil {
		return nil, fmt.Errorf("unable to allocate format context")
	}
	internal.SetFinalizerFree(ctx, k.formatContext)

	k.streamInfo = frame.BuildStreamInfo(
		k,
		codecParams,
		0,
		1,
		astiav.NewRational(1, cfg.SampleRate),
		0,
		nil,
	)

	stream := k.formatContext.NewStream(nil)
	if stream == nil {
		return nil, fmt.Errorf("unable to create format stream")
	}
	codecParams.Copy(stream.CodecParameters())
	stream.SetTimeBase(astiav.NewRational(1, cfg.SampleRate))
	stream.SetIndex(0)

	if err := k.openCaptureDevice(ctx); err != nil {
		return nil, err
	}

	return k, nil
}

func (k *Microphone) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *Microphone) String() string {
	if k == nil {
		return "Microphone(<nil>)"
	}
	return "Microphone"
}

func (k *Microphone) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	if k == nil {
		return
	}
	k.Locker.Do(ctx, func() {
		callback(k.formatContext)
	})
}

func (k *Microphone) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close()")
	defer func() { logger.Debugf(ctx, "/Close(): %v", _err) }()
	k.ClosureSignaler.Close(ctx)
	return xsync.DoA1R1(ctx, &k.Locker, k.closeLocked, ctx)
}

func (k *Microphone) closeLocked(ctx context.Context) error {
	if k.device != nil {
		k.device.Stop()
		_ = k.device.Close()
		k.device = nil
	}
	return nil
}

func (k *Microphone) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	_ = ctx
	_ = input
	_ = outputCh
	return kerneltypes.ErrUnexpectedInputType{}
}

func (k *Microphone) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Debugf(ctx, "Generate")
	defer func() { logger.Debugf(ctx, "/Generate: %v", _err) }()

	if k.device == nil {
		return fmt.Errorf("capture device is not initialized")
	}

	bufferBytes := k.bufferSamples * k.bytesPerSample * k.Config.Channels
	buffer := make([]byte, bufferBytes)
	frameSamples := k.Config.FrameSamples
	frameBytes := frameSamples * k.bytesPerSample * k.Config.Channels
	var pts int64
	pending := make([]byte, 0, bufferBytes)

	logger.Debugf(ctx, "starting capture")
	k.device.Start()
	defer k.device.Stop()

	ticker := time.NewTicker(k.Config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-k.CloseChan():
			return io.EOF
		case <-ticker.C:
		}

		available, err := k.captureSamplesAvailable()
		if err != nil {
			return err
		}
		if available <= 0 {
			continue
		}
		if available > k.bufferSamples {
			available = k.bufferSamples
		}

		k.device.Samples(buffer, int64(available))
		readBytes := available * k.bytesPerSample * k.Config.Channels
		pending = append(pending, buffer[:readBytes]...)

		for len(pending) >= frameBytes {
			frameBytesSlice := pending[:frameBytes]
			outFrame, err := k.buildFrameFromPCM(ctx, frameBytesSlice, frameSamples, pts)
			if err != nil {
				return err
			}

			select {
			case outputCh <- packetorframe.OutputUnion{Frame: &outFrame}:
			case <-ctx.Done():
				frame.Pool.Put(outFrame.Frame)
				return ctx.Err()
			case <-k.CloseChan():
				frame.Pool.Put(outFrame.Frame)
				return io.EOF
			}

			pts += int64(frameSamples)
			copy(pending, pending[frameBytes:])
			pending = pending[:len(pending)-frameBytes]
		}
	}
}

func (k *Microphone) openCaptureDevice(ctx context.Context) error {
	if k.Config.LibraryPath != "" {
		if err := al.InitPath(k.Config.LibraryPath); err != nil {
			return fmt.Errorf("unable to init OpenAL library: %w", err)
		}
	} else {
		if err := al.Init(); err != nil {
			return fmt.Errorf("unable to init OpenAL library: %w", err)
		}
	}

	format, err := captureFormat(k.Config.SampleFormat, k.Config.Channels)
	if err != nil {
		return err
	}

	device := alc.CaptureOpen(k.Config.DeviceName, uint(k.Config.SampleRate), format, int64(k.Config.BufferSamples))
	if device == nil {
		return fmt.Errorf("unable to open capture device")
	}
	if err := alc.Error(device.Error()); err != "" {
		_ = device.Close()
		return fmt.Errorf("capture device error: %s", err)
	}
	k.device = device
	return nil
}

func (k *Microphone) captureSamplesAvailable() (int, error) {
	if k.device == nil {
		return 0, fmt.Errorf("capture device is nil")
	}
	var sample int32
	k.device.GetIntegerv(alc.CaptureSamples, 4, &sample)
	if sample < 0 {
		return 0, fmt.Errorf("invalid capture sample count: %d", sample)
	}
	return int(sample), nil
}

func (k *Microphone) buildFrameFromPCM(
	ctx context.Context,
	pcm []byte,
	frameSamples int,
	pts int64,
) (frame.Output, error) {
	f := frame.Pool.Get()
	f.Unref()
	f.SetSampleFormat(k.Config.SampleFormat)
	f.SetSampleRate(k.Config.SampleRate)
	f.SetChannelLayout(k.codecParams.ChannelLayout())
	f.SetNbSamples(frameSamples)
	if err := f.AllocBuffer(0); err != nil {
		frame.Pool.Put(f)
		return frame.Output{}, fmt.Errorf("unable to allocate frame buffer: %w", err)
	}
	if err := fillFramePCM(f, k.Config.SampleFormat, k.Config.Channels, pcm); err != nil {
		frame.Pool.Put(f)
		return frame.Output{}, err
	}
	f.SetPts(pts)
	f.SetDuration(int64(frameSamples))
	out := frame.BuildOutput(f, k.streamInfo)
	return out, nil
}

func bytesPerSample(format astiav.SampleFormat) (int, error) {
	switch format {
	case astiav.SampleFormatS16:
		return 2, nil
	case astiav.SampleFormatU8:
		return 1, nil
	default:
		return 0, fmt.Errorf("unsupported sample format: %v", format)
	}
}

func codecIDFromSampleFormat(format astiav.SampleFormat) astiav.CodecID {
	switch format {
	case astiav.SampleFormatS16:
		return astiav.CodecIDPcmS16Le
	case astiav.SampleFormatU8:
		return astiav.CodecIDPcmU8
	default:
		return astiav.CodecIDNone
	}
}

func channelLayoutFromCount(channels int) (astiav.ChannelLayout, error) {
	switch channels {
	case 1:
		return astiav.ChannelLayoutMono, nil
	case 2:
		return astiav.ChannelLayoutStereo, nil
	default:
		return astiav.ChannelLayout{}, fmt.Errorf("unsupported channel count: %d", channels)
	}
}

func captureFormat(format astiav.SampleFormat, channels int) (int, error) {
	switch format {
	case astiav.SampleFormatS16:
		switch channels {
		case 1:
			return al.FormatMono16, nil
		case 2:
			return al.FormatStereo16, nil
		default:
			return 0, fmt.Errorf("unsupported channel count: %d", channels)
		}
	case astiav.SampleFormatU8:
		switch channels {
		case 1:
			return al.FormatMono8, nil
		case 2:
			return al.FormatStereo8, nil
		default:
			return 0, fmt.Errorf("unsupported channel count: %d", channels)
		}
	default:
		return 0, fmt.Errorf("unsupported sample format: %v", format)
	}
}

func fillFramePCM(f *astiav.Frame, format astiav.SampleFormat, channels int, pcm []byte) error {
	data := f.Data()
	if format.IsPlanar() {
		return fmt.Errorf("planar formats are not supported for capture")
	}
	buf, err := data.Bytes(0)
	if err != nil {
		return err
	}
	if len(buf) < len(pcm) {
		return fmt.Errorf("frame buffer too small: %d < %d", len(buf), len(pcm))
	}
	copy(buf[:len(pcm)], pcm)
	_ = channels
	return nil
}
