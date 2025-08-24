package codec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

const (
	encoderDebug = false
)

const (
	NameCopy = Name("copy")
	NameRaw  = Name("raw")
)

type Encoder interface {
	fmt.Stringer
	Closer
	Codec() *astiav.Codec
	CodecContext() *astiav.CodecContext
	MediaType() astiav.MediaType
	ToCodecParameters(cp *astiav.CodecParameters) error
	HardwareDeviceContext() *astiav.HardwareDeviceContext
	HardwarePixelFormat() astiav.PixelFormat
	TimeBase() astiav.Rational
	SendFrame(context.Context, *astiav.Frame) error
	ReceivePacket(context.Context, *astiav.Packet) error
	GetQuality(ctx context.Context) Quality
	SetQuality(context.Context, Quality, condition.Condition) error
	GetResolution(ctx context.Context) *Resolution
	SetResolution(context.Context, Resolution, condition.Condition) error
	Reset(ctx context.Context) error
	GetPCMAudioFormat(ctx context.Context) *PCMAudioFormat
}

type EncoderFullBackend = Codec
type EncoderFull struct {
	*EncoderFullBackend
	ReusableResources *Resources
	Quality           Quality
	InitTS            time.Time
	Next              typing.Optional[SwitchEncoderParams]
}

type SwitchEncoderParams struct {
	When       condition.Condition
	Quality    quality.Quality
	Resolution *Resolution
}

type Resolution = types.Resolution

type PCMAudioFormat struct {
	SampleFormat  astiav.SampleFormat
	SampleRate    int
	ChannelLayout astiav.ChannelLayout
	ChunkSize     int
}

func (pcmFmt PCMAudioFormat) Equal(other PCMAudioFormat) bool {
	channelLayoutEqual, err := pcmFmt.ChannelLayout.Compare(other.ChannelLayout)
	if err != nil {
		logger.Errorf(context.TODO(), "unable to compare channel layouts: %v", err)
		return false
	}
	return pcmFmt.SampleFormat == other.SampleFormat &&
		pcmFmt.SampleRate == other.SampleRate &&
		channelLayoutEqual &&
		pcmFmt.ChunkSize == other.ChunkSize
}

var _ Encoder = (*EncoderFull)(nil)

type ErrNotDummy struct{}

func (ErrNotDummy) Error() string {
	return "not a dummy encoder"
}

func NewEncoder(
	ctx context.Context,
	params CodecParams,
	opts ...EncoderFactoryOption,
) (_ret Encoder, _err error) {
	logger.Tracef(ctx, "NewEncoder(%#+v)", params)
	defer func() { logger.Tracef(ctx, "/NewEncoder(%#+v): %T %v", params, _ret, _err) }()
	switch params.CodecName {
	case NameCopy:
		return EncoderCopy{}, nil
	case NameRaw:
		return EncoderRaw{}, nil
	}
	if v, ok := EncoderFactoryOptionLatest[EncoderFactoryOptionOnlyDummy](opts); ok {
		if v.OnlyDummy {
			return nil, ErrNotDummy{}
		}
	}
	e, err := newEncoder(ctx, params, nil, opts...)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func newEncoder(
	ctx context.Context,
	params CodecParams,
	overrideQuality Quality,
	opts ...EncoderFactoryOption,
) (_ret *EncoderFull, _err error) {
	res := Resolution{
		Width:  uint32(params.CodecParameters.Width()),
		Height: uint32(params.CodecParameters.Height()),
	}
	logger.Tracef(ctx, "newEncoder(ctx, %#+v, %#+v, %v)", params, overrideQuality, res)
	defer func() {
		logger.Tracef(ctx, "/newEncoder(ctx, %#+v, %#+v, %v): %p %v", params, overrideQuality, res, _ret, _err)
	}()
	params = params.Clone(ctx)
	if overrideQuality != nil {
		if params.CodecParameters == nil {
			params.CodecParameters = astiav.AllocCodecParameters()
			setFinalizerFree(ctx, params.CodecParameters)
		}
		err := overrideQuality.Apply(params.CodecParameters)
		if err != nil {
			return nil, fmt.Errorf("unable to apply quality override %#+v: %w", overrideQuality, err)
		}
	}
	var reusableResources *Resources
	if v, ok := EncoderFactoryOptionLatest[EncoderFactoryOptionReusableResources](opts); ok {
		reusableResources = v.ReusableResources
	}
	c, err := newCodec(
		ctx,
		true,
		params,
		reusableResources,
	)
	if err != nil {
		return nil, err
	}

	e := &EncoderFull{
		EncoderFullBackend: c,
		ReusableResources:  reusableResources,
		InitTS:             time.Now(),
	}
	return e, nil
}

func (e *EncoderFull) String() string {
	ctx := context.TODO()
	if !e.locker.ManualTryRLock(ctx) {
		return "Encoder(<locked>)"
	}
	defer e.locker.ManualRUnlock(ctx)
	return fmt.Sprintf("Encoder(%s)", e.codec.Name())
}

func (e *EncoderFull) GetInitTS() time.Time {
	return e.InitTS
}

func (e *EncoderFull) SendFrame(
	ctx context.Context,
	f *astiav.Frame,
) (_err error) {
	logger.Tracef(ctx, "SendFrame")
	defer func() { logger.Tracef(ctx, "/SendFrame: %v", _err) }()
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &e.locker, func() error {
		return e.sendFrameLocked(ctx, f)
	})
}

func (e *EncoderFull) sendFrameLocked(
	ctx context.Context,
	f *astiav.Frame,
) error {
	if encoderDebug {
		if e.codecContext.Framerate().Float64() == 0 && f.Duration() == 0 {
			logger.Errorf(ctx, "it is impossible to calculate the framerate, since it is not set on the encoder and the frame has no duration")
		}
	}
	if strings.HasSuffix(e.codec.Name(), "_nvenc") {
		// NVENC has a bug that they ignore timestamps on frames,
		// thus if we have variadic framerates, which somehow leads
		// to abysmally small bitrate (at least in my case).
		//
		// See also https://video.stackexchange.com/questions/38096/vfr-input-h264-nvenc-output-bitrate-is-based-on-initial-frame-rate-when-i-wa
		e.setFrameRateFromDuration(ctx, f)
	}
	return e.codecContext.SendFrame(f)
}

func (e *EncoderFull) setFrameRateFromDuration(
	ctx context.Context,
	f *astiav.Frame,
) {
	dur := f.Duration()
	if dur <= 0 {
		logger.Debugf(ctx, "cannot set framerate from frame duration: frame has no duration")
		return
	}
	timeBase := e.codecContext.TimeBase()
	fps := timeBase.Invert()
	fps.SetNum(fps.Num() / int(dur))
	oldFPS := e.InitParams.CodecParameters.FrameRate()
	if oldFPS == fps {
		if encoderDebug {
			logger.Tracef(ctx, "FPS have not changed: %v", fps)
		}
		return
	}

	logger.Debugf(ctx, "setting FPS to %v (codec: '%s')", fps, e.codec.Name())
	e.InitParams.CodecParameters.SetFrameRate(fps)
	switch {
	case strings.HasSuffix(e.codec.Name(), "_nvenc"):
		// NVENC seems to ignore codec context framerate
		// so we just need to reinit the encoder
		err := e.reinitEncoder(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to reinit the encoder after framerate (%v -> %v) change: %v", oldFPS, fps, err)
		}
	default:
		e.codecContext.SetFramerate(fps)
	}
}

func (e *EncoderFull) ReceivePacket(
	ctx context.Context,
	p *astiav.Packet,
) (_err error) {
	logger.Tracef(ctx, "ReceivePacket")
	defer func() { logger.Tracef(ctx, "/ReceivePacket: %v", _err) }()
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &e.locker, func() error {
		return e.receivePacketLocked(ctx, p)
	})
}

func (e *EncoderFull) receivePacketLocked(
	ctx context.Context,
	p *astiav.Packet,
) (err error) {
	err = e.codecContext.ReceivePacket(p)
	if !e.Next.IsSet() {
		return
	}

	next := e.Next.Get()
	if next.When == nil || next.When.Match(ctx, packet.BuildInput(p, nil)) {
		e.Next.Unset()
		if q := next.Quality; q != nil {
			qErr := e.setQualityNow(ctx, q)
			if qErr != nil {
				logger.Errorf(ctx, "unable to set quality to %v: %v", q, qErr)
			}
		}
		if r := next.Resolution; r != nil {
			rErr := e.setResolutionNow(ctx, *r)
			if rErr != nil {
				logger.Errorf(ctx, "unable to set resolution to %dx%d: %v", r.Width, r.Height, rErr)
			}
		}
	}

	return err
}

func (e *EncoderFull) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Close")
	defer func() { logger.Tracef(ctx, "/Close: %v", _err) }()
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &e.locker, e.closeLocked, ctx)
}

func (e *EncoderFull) closeLocked(ctx context.Context) error {
	var result []error
	if err := e.EncoderFullBackend.closeLocked(ctx); err != nil {
		result = append(result, fmt.Errorf("unable to close the old encoder: %w", err))
	}
	e.InitParams.Options = nil
	e.InitParams.CodecParameters = nil
	return errors.Join(result...)
}

func IsDummyEncoder(encoder Encoder) bool {
	return IsEncoderCopy(encoder) || IsEncoderRaw(encoder)
}

func (e *EncoderFull) reinitEncoder(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "reinitEncoder")
	defer func() { logger.Tracef(ctx, "/reinitEncoder: %v", _err) }()

	oldInternals := e.codecInternals
	// We'd prefer to close the old encoder after the new one is created,
	// but there is a bug in NVENC that sometimes leads to segfaults:
	//#0  runtime.raise () at /usr/lib/go-1.24/src/runtime/sys_linux_amd64.s:154
	//#1  0x0000000000460017 in runtime.raisebadsignal (sig=11, c=0x7da2d17e5510) at /usr/lib/go-1.24/src/runtime/signal_unix.go:1038
	//#2  0x0000000000460285 in runtime.badsignal (sig=11, c=0x7da2d17e5510) at /usr/lib/go-1.24/src/runtime/signal_unix.go:1147
	//#3  0x000000000045ef2b in runtime.sigtrampgo (sig=11, info=0x7da2d17e56b0, ctx=0x7da2d17e5580) at /usr/lib/go-1.24/src/runtime/signal_unix.go:468
	//#4  0x0000000000487f46 in runtime.sigtramp () at /usr/lib/go-1.24/src/runtime/sys_linux_amd64.s:352
	//#5  0x00007da478445810 in <signal handler called> () at /lib/x86_64-linux-gnu/libc.so.6
	//#6  0x00007da3062199d1 in ??? () at /lib/x86_64-linux-gnu/libnvcuvid.so.1
	//#7  0x00007da306219b0a in ??? () at /lib/x86_64-linux-gnu/libnvcuvid.so.1
	//#8  0x00007da3062979a6 in ??? () at /lib/x86_64-linux-gnu/libnvcuvid.so.1
	//#9  0x00007da30629810d in ??? () at /lib/x86_64-linux-gnu/libnvcuvid.so.1
	//#10 0x00007da4784a2ef1 in start_thread (arg=<optimized out>) at ./nptl/pthread_create.c:448
	//#11 0x00007da47853445c in __GI___clone3 () at ../sysdeps/unix/sysv/linux/x86_64/clone3.S:78
	if err := oldInternals.closeLocked(ctx); err != nil {
		logger.Errorf(ctx, "unable to close the old encoder: %v", err)
	}

	var opts EncoderFactoryOptions
	if e.ReusableResources != nil {
		opts = append(opts, EncoderFactoryOptionReusableResources{ReusableResources: e.ReusableResources})
	}
	newEncoder, err := newEncoder(ctx, e.InitParams, e.Quality, opts...)
	if err != nil {
		return fmt.Errorf("unable to initialize new encoder: %w", err)
	}

	logger.Tracef(ctx, "replaced the encoder with a new one (%p); the old one (%p) was is closed", newEncoder.EncoderFullBackend, e.EncoderFullBackend)

	// keeping the locker from the old codec to make sure everybody who is already locked
	// will get relevant stuff when it will get unlocked
	oldCodec := e.EncoderFullBackend
	oldCodec.codecInternals = newEncoder.codecInternals
	newEncoder.EncoderFullBackend = oldCodec
	*e = *newEncoder
	return nil
}

func (e *EncoderFull) SanityCheck(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "SanityCheck")
	defer func() { logger.Tracef(ctx, "/SanityCheck: %v", _err) }()
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &e.locker, e.sanityCheckLocked, ctx)
}

func (e *EncoderFull) sanityCheckLocked(
	ctx context.Context,
) error {
	logger.Tracef(ctx, "sanityCheck[%p]", e.EncoderFullBackend)
	if e.codec == nil {
		return errors.New("codec == nil")
	}
	if e.codecContext == nil {
		return errors.New("codecContext == nil")
	}
	return nil
}
