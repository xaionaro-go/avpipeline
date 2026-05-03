//go:build android && cgo
// +build android,cgo

// microphone.go implements Android microphone capture via AAudio.

package android

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"unsafe"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/internal"
	"github.com/xaionaro-go/avpipeline/kernel"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/AndroidGoLab/ndk/audio"
	aaudiocapi "github.com/AndroidGoLab/ndk/capi/aaudio"
	"github.com/xaionaro-go/xsync"
)

// AAudio natively supports S16 (and Float). We hardcode S16 as the
// capture and output format.
const (
	microphoneSampleFormat   = astiav.SampleFormatS16
	microphoneBytesPerSample = 2
)

type Microphone struct {
	*closuresignaler.ClosureSignaler
	Config MicrophoneConfig
	Locker xsync.Mutex

	streamInfo    *frame.StreamInfo
	codecParams   *astiav.CodecParameters
	stream        *audio.Stream
	bufferSamples int
	formatContext *astiav.FormatContext
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
	if cfg.InputPreset == 0 {
		cfg.InputPreset = microphoneDefaultInputPreset
	}

	if cfg.DeviceID == nil && cfg.DeviceNamePattern != "" {
		devID, err := resolveDeviceByName(ctx, cfg.DeviceNamePattern)
		if err != nil {
			return nil, fmt.Errorf("unable to resolve device name: %w", err)
		}
		cfg.DeviceID = &devID
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
	codecParams.SetCodecID(astiav.CodecIDPcmS16Le)
	codecParams.SetSampleFormat(microphoneSampleFormat)
	codecParams.SetSampleRate(cfg.SampleRate)
	codecParams.SetChannelLayout(channelLayout)

	k := &Microphone{
		ClosureSignaler: closuresignaler.New(),
		Config:          cfg,
		codecParams:     codecParams,
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
	frameBytes := frameSamples * microphoneBytesPerSample * k.Config.Channels
	readSamples := k.bufferSamples
	bufferBytes := readSamples * microphoneBytesPerSample * k.Config.Channels
	buffer := make([]byte, bufferBytes)
	pending := make([]byte, 0, bufferBytes)

	// Seed first PTS from the process-wide shared monotonic epoch
	// (kernel.PTSEpochNanos), expressed in 1/sampleRate timebase. The
	// camera Input kernel (android_camera / pulse paths) anchors its
	// per-stream first packet to the same epoch via the PTSEpoch
	// sentinel in input.go, so audio + video first frames land on a
	// common wall-clock origin reflecting their actual cold-start
	// offset instead of every kernel restarting at PTS=0. See
	// /tmp/av_sync_camera_mic_addinput.md option (b) and
	// kernel/pts_epoch.go for the shared-epoch helper.
	pts := initialMicrophonePTS(k.Config.SampleRate)
	logger.Infof(ctx, "initial audio PTS: %d (sample_rate=%d, epoch_nanos=%d)",
		pts, k.Config.SampleRate, kernel.PTSEpochNanos())

	readTimeoutNanos := k.Config.PollInterval.Nanoseconds()
	if readTimeoutNanos <= 0 {
		readTimeoutNanos = 100_000_000 // 100ms default
	}

	logger.Debugf(ctx, "starting capture")
	if err := k.stream.Start(); err != nil {
		return fmt.Errorf("unable to start audio stream: %w", err)
	}
	defer func() {
		if k.stream != nil {
			k.stream.Stop()
		}
	}()
	logger.Infof(ctx,
		"AAudio capture started: state=%s xruns=%d",
		k.stream.State(), k.stream.XRunCount(),
	)

	sd := newSilenceDetector(k.Config)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-k.CloseChan():
			return io.EOF
		default:
		}

		framesRead, err := k.readFromStream(ctx, buffer, readSamples, readTimeoutNanos)
		if err != nil {
			return err
		}
		if framesRead == 0 {
			continue
		}

		bytesRead := framesRead * microphoneBytesPerSample * k.Config.Channels
		sd.update(ctx, buffer[:bytesRead], int64(framesRead))

		pending = append(pending, buffer[:bytesRead]...)

		for len(pending) >= frameBytes {
			outFrame, err := k.buildFrameFromPCM(ctx, pending[:frameBytes], frameSamples, pts)
			if err != nil {
				return err
			}

			err = k.sendFrame(ctx, outputCh, outFrame)
			if err != nil {
				return err
			}

			pts += int64(frameSamples)
			copy(pending, pending[frameBytes:])
			pending = pending[:len(pending)-frameBytes]
		}
	}
}

// readFromStream reads audio data from the AAudio stream. On disconnect,
// it reopens the capture device transparently. Returns the number of
// frames read (0 means no data available, retry).
func (k *Microphone) readFromStream(
	ctx context.Context,
	buffer []byte,
	readSamples int,
	readTimeoutNanos int64,
) (int, error) {
	framesRead := aaudiocapi.AAudioStream_read(
		(*aaudiocapi.AAudioStream)(k.stream.Pointer()),
		unsafe.Pointer(&buffer[0]),
		int32(readSamples),
		readTimeoutNanos,
	)
	if framesRead >= 0 {
		return int(framesRead), nil
	}

	readErr := audio.Error(framesRead)
	if !errors.Is(readErr, audio.ErrDisconnected) {
		return 0, fmt.Errorf("audio read error: %w", readErr)
	}

	// AAudio stream disconnected (e.g. sensor privacy toggled,
	// audio routing changed). Reopen and restart.
	logger.Warnf(ctx, "AAudio stream disconnected, reopening capture device")
	_ = k.stream.Stop()
	_ = k.stream.Close()
	k.stream = nil

	if err := k.openCaptureDevice(ctx); err != nil {
		return 0, fmt.Errorf("unable to reopen capture device after disconnect: %w", err)
	}
	if err := k.stream.Start(); err != nil {
		return 0, fmt.Errorf("unable to restart capture after disconnect: %w", err)
	}
	logger.Infof(ctx, "AAudio capture reconnected: state=%s", k.stream.State())
	return 0, nil
}

// sendFrame sends a frame to the output channel, respecting context
// cancellation and close signals.
func (k *Microphone) sendFrame(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
	outFrame frame.Output,
) error {
	select {
	case outputCh <- packetorframe.OutputUnion{Frame: &outFrame}:
		return nil
	case <-ctx.Done():
		frame.Pool.Put(outFrame.Frame)
		return ctx.Err()
	case <-k.CloseChan():
		frame.Pool.Put(outFrame.Frame)
		return io.EOF
	}
}

func (k *Microphone) openCaptureDevice(ctx context.Context) error {
	if k.Config.DisableSensorPrivacyOnStart {
		disableSensorPrivacy(ctx)
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
		SetFormat(audio.PcmI16).
		SetPerformanceMode(audio.LowLatency).
		SetSharingMode(audio.Shared).
		SetBufferCapacityInFrames(int32(k.Config.BufferSamples))

	// Set input preset (defaults to UNPROCESSED for raw capture without
	// Android audio processing). Override via MicrophoneConfig.InputPreset.
	aaudiocapi.AAudioStreamBuilder_setInputPreset(
		(*aaudiocapi.AAudioStreamBuilder)(builder.Pointer()),
		k.Config.InputPreset,
	)

	if k.Config.DeviceID != nil {
		builder.SetDeviceID(*k.Config.DeviceID)
	}

	stream, err := builder.Open()
	if err != nil {
		return fmt.Errorf("unable to open AAudio capture stream: %w", err)
	}

	actualDeviceID := aaudiocapi.AAudioStream_getDeviceId(
		(*aaudiocapi.AAudioStream)(stream.Pointer()),
	)
	var requestedDeviceStr string
	switch {
	case k.Config.DeviceID == nil:
		requestedDeviceStr = "<default>"
	default:
		requestedDeviceStr = fmt.Sprintf("%d", *k.Config.DeviceID)
	}
	if k.Config.DeviceID != nil && actualDeviceID != *k.Config.DeviceID {
		logger.Warnf(ctx,
			"AAudio ignored requested device_id=%d, opened device_id=%d instead (device may not exist or does not support capture); to list available devices run: dumpsys media.audio_policy | sed -n '/Available input devices/,/^$/p'",
			*k.Config.DeviceID, actualDeviceID,
		)
	}
	logger.Infof(ctx,
		"AAudio capture stream opened: requested_device_id=%s actual_device_id=%d actual_sample_rate=%d actual_channels=%d state=%s frames_per_burst=%d input_preset=%d",
		requestedDeviceStr, actualDeviceID, stream.SampleRate(), stream.ChannelCount(), stream.State(), stream.FramesPerBurst(), k.Config.InputPreset,
	)

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
	f.SetSampleFormat(microphoneSampleFormat)
	f.SetSampleRate(k.Config.SampleRate)
	f.SetChannelLayout(k.codecParams.ChannelLayout())
	f.SetNbSamples(frameSamples)
	if err := f.AllocBuffer(0); err != nil {
		frame.Pool.Put(f)
		return frame.Output{}, fmt.Errorf("unable to allocate frame buffer: %w", err)
	}

	// SetBytes copies the PCM data into the C frame buffer.
	// Bytes(0) returns a copy, so writing to it would not modify
	// the actual frame data — SetBytes is required.
	if err := f.Data().SetBytes(pcm, 0); err != nil {
		frame.Pool.Put(f)
		return frame.Output{}, fmt.Errorf("unable to set frame data: %w", err)
	}

	f.SetPts(pts)
	f.SetDuration(int64(frameSamples))
	out := frame.BuildOutput(f, k.streamInfo)
	return out, nil
}

// disableSensorPrivacy runs `cmd sensor_privacy disable 0 microphone`
// to unblock the microphone at the Android system level. Requires root.
// sensorPrivacyCmdPath is the full path to the Android `cmd` binary
// which is not in PATH inside the Termux chroot.
const sensorPrivacyCmdPath = "/system/bin/cmd"

func disableSensorPrivacy(ctx context.Context) {
	out, err := exec.CommandContext(
		ctx, sensorPrivacyCmdPath, "sensor_privacy", "disable", "0", "microphone",
	).CombinedOutput()
	if err != nil {
		logger.Errorf(ctx, "unable to disable microphone sensor privacy: %v: %s", err, out)
		return
	}
	logger.Infof(ctx, "disabled microphone sensor privacy")
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
