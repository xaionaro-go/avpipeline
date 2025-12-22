package codec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/indicator"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

const (
	extraDefensiveChecks = true
)

var _ Encoder = (*EncoderFull)(nil)

type EncoderFullLocked struct {
	*EncoderFullBackend
	Quality           Quality
	InitTS            time.Time
	Next              typing.Optional[SwitchEncoderParams]
	CallCount         atomic.Int64
	ForceNextKeyFrame bool
	AverageFPS        *indicator.MAMA[float64]
}

func newEncoderFullLocked(
	ctx context.Context,
	params CodecParams,
	overrideQuality Quality,
) (_ret *EncoderFullLocked, _err error) {
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
	input := Input{
		IsEncoder: true,
		Params:    params,
	}
	c, err := newCodec(
		ctx,
		input,
	)
	if err != nil {
		return nil, err
	}

	e := &EncoderFullLocked{
		EncoderFullBackend: c,
		InitTS:             time.Now(),
		AverageFPS:         indicator.NewMAMA[float64](60, 0.1, 0.01),
	}
	switch e.MediaType() {
	case astiav.MediaTypeVideo:
		e.ForceNextKeyFrame = true
	}
	return e, nil
}

func (e *EncoderFullLocked) MediaType() astiav.MediaType {
	return e.mediaTypeLocked()
}

func (e *EncoderFullLocked) AsLocked() *EncoderFull {
	return (*EncoderFull)(e)
}

func (e *EncoderFullLocked) UnlockDo(
	ctx context.Context,
	fn func(ctx context.Context),
) {
	e.locker.UDo(xsync.WithNoLogging(ctx, true), func() {
		fn(ctx)
	})
}

func (e *EncoderFullLocked) String() string {
	ctx := context.TODO()
	if !e.locker.ManualTryRLock(ctx) {
		return "Encoder(<locked>)"
	}
	defer e.locker.ManualRUnlock(ctx)
	return fmt.Sprintf("Encoder(%s)", e.codec.Name())
}

func (e *EncoderFullLocked) GetInitTS() time.Time {
	return e.InitTS
}

func (e *EncoderFullLocked) SendFrame(
	ctx context.Context,
	f *astiav.Frame,
) (_err error) {
	defer e.checkCallCount(ctx)()
	logger.Tracef(ctx, "SendFrame: pts:%d, pixel_format:%s", f.Pts(), f.PixelFormat())
	defer func() { logger.Tracef(ctx, "/SendFrame: %v", _err) }()

	switch e.MediaType() {
	case astiav.MediaTypeVideo:
		if e.ForceNextKeyFrame {
			e.ForceNextKeyFrame = false
			if !f.Flags().Has(astiav.FrameFlagKey) {
				logger.Debugf(ctx, "forcing the frame to be a keyframe")
				f.SetFlags(f.Flags().Add(astiav.FrameFlagKey))
				f.SetPictureType(astiav.PictureTypeI)
				if f.Pts() == astiav.NoPtsValue {
					return fmt.Errorf("cannot force the frame to be a keyframe: frame has no PTS")
				}
			}
		}

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
	}

	e.isDirty = true
	return e.codecContext.SendFrame(f)
}

func (e *EncoderFullLocked) setFrameRateFromDuration(
	ctx context.Context,
	f *astiav.Frame,
) {
	dur := f.Duration()
	if dur <= 0 {
		logger.Debugf(ctx, "cannot set framerate from frame duration: frame has no duration")
		return
	}
	timeBase := e.codecContext.TimeBase()
	curFPS := timeBase.Invert()
	curFPS.SetNum(curFPS.Num() / int(dur))
	avgFPS := e.AverageFPS.Update(curFPS.Float64())
	_fps := globaltypes.RationalFromApproxFloat64(avgFPS)
	fps := astiav.NewRational(_fps.Num, _fps.Den)
	logger.Tracef(ctx, "calculated FPS from frame duration: dur:%d time_base:%f fps:%v avg_fps:%f (~= %v = %v = %f)", dur, timeBase.Float64(), curFPS, avgFPS, _fps, fps, fps.Float64())
	oldFPS := e.InitParams.CodecParameters.FrameRate()
	if oldFPS == fps {
		if encoderDebug {
			logger.Tracef(ctx, "FPS have not changed: %v", fps)
		}
		return
	}
	if !e.AverageFPS.Valid() {
		logger.Debugf(ctx, "waiting for more samples to stabilize framerate: curFPS:%v avgFPS:%f", curFPS, avgFPS)
		return
	}

	if encoderDebug {
		logger.Tracef(ctx, "setting FPS to %v->%v (codec: '%s')", oldFPS, avgFPS, e.codec.Name())
	}
	e.InitParams.CodecParameters.SetFrameRate(fps)
	switch {
	case strings.HasSuffix(e.codec.Name(), "_nvenc"):
		fpsChangeFactor := math.Abs((fps.Float64() - oldFPS.Float64()) / math.Max(fps.Float64(), oldFPS.Float64()))
		if fpsChangeFactor < 0.2 {
			if encoderDebug {
				logger.Tracef(ctx, "the framerate change is negligible (%v -> %v: %f), not reinitializing the encoder", oldFPS, fps, fpsChangeFactor)
			}
			return
		}
		// NVENC seems to ignore codec context framerate
		// so we just need to reinit the encoder
		logger.Warnf(ctx, "reinitializing encoder to apply new framerate %f (%v); fps_change_factor:%f, dur:%v, time_base:%f", fps.Float64(), fps, fpsChangeFactor, dur, timeBase.Float64())
		err := e.reinitEncoder(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to reinit the encoder after framerate (%v -> %v) change: %v", oldFPS, fps, err)
		}
	default:
		e.codecContext.SetFramerate(fps)
	}
}

func (e *EncoderFullLocked) ReceivePacket(
	ctx context.Context,
	p *astiav.Packet,
) (err error) {
	logger.Tracef(ctx, "ReceivePacket")
	defer func() { logger.Tracef(ctx, "/ReceivePacket: %v", err) }()
	err = e.codecContext.ReceivePacket(p)
	if !e.Next.IsSet() {
		return
	}

	// TODO: delete this, apparently this is not needed
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

func (e *EncoderFullLocked) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Close")
	defer func() { logger.Tracef(ctx, "/Close: %v", _err) }()
	var result []error
	if err := e.EncoderFullBackend.closeLocked(ctx); err != nil {
		result = append(result, fmt.Errorf("unable to close the encoder: %w", err))
	}
	e.InitParams.CustomOptions = nil
	e.InitParams.CodecParameters = nil
	return errors.Join(result...)
}

func (e *EncoderFullLocked) reinitEncoder(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "reinitEncoder")
	defer func() { logger.Debugf(ctx, "/reinitEncoder: %v", _err) }()

	if err := e.codecInternals.closeLocked(ctx); err != nil {
		logger.Errorf(ctx, "unable to close the old encoder: %v", err)
	}

	newEncoder, err := newEncoderFullLocked(ctx, e.InitParams, e.Quality)
	if err != nil {
		return fmt.Errorf("unable to initialize new encoder: %w", err)
	}

	logger.Tracef(ctx, "replaced the encoder with a new one (%p); the old one (%p) is going to be closed", newEncoder.EncoderFullBackend, e.EncoderFullBackend)

	e.codecInternals = newEncoder.codecInternals
	e.InitTS = newEncoder.InitTS
	e.Quality = newEncoder.Quality
	e.Next = newEncoder.Next

	switch e.MediaType() {
	case astiav.MediaTypeVideo:
		e.ForceNextKeyFrame = true
	}

	return nil
}

func (e *EncoderFullLocked) SanityCheck(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "SanityCheck")
	defer func() { logger.Tracef(ctx, "/SanityCheck: %v", _err) }()
	logger.Tracef(ctx, "sanityCheck[%p]", e.EncoderFullBackend)
	if e.codec == nil {
		return errors.New("codec == nil")
	}
	if e.codecContext == nil {
		return errors.New("codecContext == nil")
	}
	return nil
}

func (e *EncoderFullLocked) SetForceNextKeyFrame(
	ctx context.Context,
	v bool,
) error {
	e.ForceNextKeyFrame = v
	return nil
}

func (e *EncoderFullLocked) Flush(
	ctx context.Context,
	callback CallbackPacketReceiver,
) (_err error) {
	defer e.checkCallCount(ctx)()
	logger.Debugf(ctx, "Flush")
	defer func() { logger.Debugf(ctx, "/Flush: %v", _err) }()

	defer func() {
		if _err != nil {
			return
		}
		switch e.MediaType() {
		case astiav.MediaTypeVideo:
			e.ForceNextKeyFrame = true
		}
		if e.isDirty {
			logger.Errorf(ctx, "%v is still dirty after flush; forcing isDirty:false", e)
			e.isDirty = false
		}
	}()

	caps := e.codec.Capabilities()
	logger.Tracef(ctx, "Capabilities: %08x", caps)

	if caps&astiav.CodecCapabilityDelay == 0 {
		logger.Tracef(ctx, "the encoder has no delay, nothing to flush")
		return nil
	}

	err := e.Drain(ctx, callback)
	if err != nil {
		return fmt.Errorf("unable to pre-drain: %w", err)
	}

	logger.Tracef(ctx, "sending the FLUSH REQUEST pseudo-frame")
	err = e.codecContext.SendFrame(nil)
	switch {
	case err == nil:
		// flushing had just been initiated
		logger.Tracef(ctx, "waiting for the encoder to be flushed")
		err = e.Drain(ctx, callback)
		if err != io.EOF {
			return fmt.Errorf("unable to drain: %w", err)
		}
	case errors.Is(err, astiav.ErrEof):
		// the encoder is already flushed
	default:
		return fmt.Errorf("unable to send the FLUSH REQUEST pseudo-frame: %w", err)
	}

	if caps&astiav.CodecCapabilityEncoderFlush != 0 && !e.quirks.HasAny(QuirkBuggyFlushBuffers) {
		logger.Tracef(ctx, "flushing buffers")
		e.codecContext.FlushBuffers()
		return
	}

	logger.Warnf(ctx, "the encoder has no flush capability, reinitializing the encoder")
	err = e.reinitEncoder(ctx)
	if err != nil {
		return fmt.Errorf("unable to reinit the encoder after draining: %w", err)
	}

	return nil
}

func (e *EncoderFullLocked) Drain(
	ctx context.Context,
	callback CallbackPacketReceiver,
) (_err error) {
	logger.Tracef(ctx, "Drain")
	defer func() { logger.Tracef(ctx, "/Drain: %v", _err) }()

	caps := e.codec.Capabilities()
	logger.Tracef(ctx, "Capabilities: %08x", caps)
	for {
		pkt := packet.Pool.Get()
		err := e.ReceivePacket(ctx, pkt)
		if err != nil {
			isEOF := errors.Is(err, astiav.ErrEof)
			isEAgain := errors.Is(err, astiav.ErrEagain)
			// isEOF means that the decoder has been fully flushed
			// isEAgain means that there are no more frames to receive right now
			packet.Pool.Pool.Put(pkt)
			logger.Tracef(ctx, "encoder.ReceivePacket(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
			if isEOF {
				e.isDirty = false
				return io.EOF
			}
			if isEAgain {
				if caps&astiav.CodecCapabilityDelay == 0 {
					e.isDirty = false
				}
				return nil
			}
			return fmt.Errorf("unable receive the packet from the encoder: %w", err)
		}
		if callback == nil {
			packet.Pool.Pool.Put(pkt)
			continue
		}
		err = callback(ctx, e, caps, pkt)
		if err != nil {
			packet.Pool.Pool.Put(pkt)
			return fmt.Errorf("unable to process the packet: %w", err)
		}
	}
}

func (e *EncoderFullLocked) IsDirty() bool {
	return e.isDirty
}

func (e *EncoderFullLocked) LockDo(ctx context.Context, fn func(context.Context, Encoder) error) error {
	return fn(ctx, e)
}

func (e *EncoderFullLocked) checkCallCount(context.Context) context.CancelFunc {
	if !extraDefensiveChecks {
		return func() {}
	}
	if e.CallCount.Add(1) > 1 {
		s := make([]byte, 10<<20)
		n := runtime.Stack(s, true)
		s = s[:n]
		panic(fmt.Sprintf("concurrent call detected to EncoderFullLocked methods, this is a bug:\n%s", s))
	}
	return func() {
		e.CallCount.Add(-1)
	}
}

func (e *EncoderFullLocked) Codec() *astiav.Codec {
	return e.codec
}
