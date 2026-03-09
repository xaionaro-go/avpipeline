//go:build android && cgo
// +build android,cgo

// microphone.go implements Android microphone capture via AAudio.

package android

import (
	"context"
	"fmt"
	"io"
	"unsafe"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/internal"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/ndk/audio"
	aaudiocapi "github.com/xaionaro-go/ndk/capi/aaudio"
	"github.com/xaionaro-go/xsync"
)

type Microphone struct {
	*closuresignaler.ClosureSignaler
	Config MicrophoneConfig
	Locker xsync.Mutex

	streamInfo  *frame.StreamInfo
	codecParams *astiav.CodecParameters
	stream      *audio.Stream

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

	bps, err := bytesPerSample(cfg.SampleFormat)
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
		bytesPerSample:  bps,
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
	if k.stream != nil {
		_ = k.stream.Stop()
		_ = k.stream.Close()
		k.stream = nil
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

	if k.stream == nil {
		return fmt.Errorf("audio stream is not initialized")
	}

	frameSamples := k.Config.FrameSamples
	frameBytes := frameSamples * k.bytesPerSample * k.Config.Channels
	readSamples := k.bufferSamples
	bufferBytes := readSamples * k.bytesPerSample * k.Config.Channels
	buffer := make([]byte, bufferBytes)
	var pts int64
	pending := make([]byte, 0, bufferBytes)

	readTimeoutNanos := k.Config.PollInterval.Nanoseconds()
	if readTimeoutNanos <= 0 {
		readTimeoutNanos = 100_000_000 // 100ms default
	}

	logger.Debugf(ctx, "starting capture")
	if err := k.stream.Start(); err != nil {
		return fmt.Errorf("unable to start audio stream: %w", err)
	}
	defer k.stream.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-k.CloseChan():
			return io.EOF
		default:
		}

		framesRead := aaudiocapi.AAudioStream_read(
			(*aaudiocapi.AAudioStream)(k.stream.Pointer()),
			unsafe.Pointer(&buffer[0]),
			int32(readSamples),
			readTimeoutNanos,
		)
		if framesRead < 0 {
			return fmt.Errorf("audio read error: %w", audio.Error(framesRead))
		}
		if framesRead == 0 {
			continue
		}

		bytesRead := int(framesRead) * k.bytesPerSample * k.Config.Channels
		pending = append(pending, buffer[:bytesRead]...)

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
	format, err := aaudioFormat(k.Config.SampleFormat)
	if err != nil {
		return err
	}

	builder, err := audio.NewStreamBuilder()
	if err != nil {
		return fmt.Errorf("unable to create AAudio stream builder: %w", err)
	}
	defer builder.Close()

	builder.
		SetDirection(audio.Input).
		SetSampleRate(int32(k.Config.SampleRate)).
		SetChannelCount(int32(k.Config.Channels)).
		SetFormat(format).
		SetPerformanceMode(audio.LowLatency).
		SetSharingMode(audio.Shared).
		SetBufferCapacityInFrames(int32(k.Config.BufferSamples))

	if k.Config.DeviceID != 0 {
		builder.SetDeviceID(k.Config.DeviceID)
	}

	stream, err := builder.Open()
	if err != nil {
		return fmt.Errorf("unable to open AAudio capture stream: %w", err)
	}
	k.stream = stream
	return nil
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

func aaudioFormat(format astiav.SampleFormat) (audio.Format, error) {
	switch format {
	case astiav.SampleFormatS16:
		return audio.PcmI16, nil
	default:
		return audio.Invalid, fmt.Errorf("unsupported sample format for AAudio: %v", format)
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
