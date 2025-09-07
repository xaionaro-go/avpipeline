package codec

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

const (
	extraDefensiveChecks = true
)

var _ Encoder = (*EncoderFull)(nil)

type EncoderFullLocked struct {
	*EncoderFullBackend
	ReusableResources *Resources
	Quality           Quality
	InitTS            time.Time
	Next              typing.Optional[SwitchEncoderParams]
	IsDirtyValue      atomic.Bool
	CallCount         atomic.Int64
}

func newEncoderFullUnlocked(
	ctx context.Context,
	params CodecParams,
	overrideQuality Quality,
	opts ...EncoderFactoryOption,
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
	var reusableResources *Resources
	if v, ok := EncoderFactoryOptionLatest[EncoderFactoryOptionReusableResources](opts); ok {
		reusableResources = v.Resources
	}
	input := Input{
		IsEncoder:         true,
		Params:            params,
		ReusableResources: reusableResources,
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
		ReusableResources:  reusableResources,
		InitTS:             time.Now(),
	}
	return e, nil
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
	logger.Tracef(ctx, "SendFrame")
	defer func() { logger.Tracef(ctx, "/SendFrame: %v", _err) }()
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
	e.IsDirtyValue.Store(true)
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
		result = append(result, fmt.Errorf("unable to close the old encoder: %w", err))
	}
	e.InitParams.Options = nil
	e.InitParams.CodecParameters = nil
	return errors.Join(result...)
}

func (e *EncoderFullLocked) reinitEncoder(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "reinitEncoder")
	defer func() { logger.Debugf(ctx, "/reinitEncoder: %v", _err) }()

	var opts EncoderFactoryOptions
	if e.ReusableResources != nil {
		opts = append(opts, EncoderFactoryOptionReusableResources{Resources: e.ReusableResources})
	}
	newEncoder, err := newEncoderFullUnlocked(ctx, e.InitParams, e.Quality, opts...)
	if err != nil {
		return fmt.Errorf("unable to initialize new encoder: %w", err)
	}

	logger.Tracef(ctx, "replaced the encoder with a new one (%p); the old one (%p) is going to be closed", newEncoder.EncoderFullBackend, e.EncoderFullBackend)

	oldInternals := e.codecInternals
	e.codecInternals = newEncoder.codecInternals
	if err := oldInternals.closeLocked(ctx); err != nil {
		logger.Errorf(ctx, "unable to close the old encoder: %v", err)
	}
	e.IsDirtyValue.Store(false)
	e.InitTS = newEncoder.InitTS
	e.Quality = newEncoder.Quality
	e.ReusableResources = newEncoder.ReusableResources
	e.Next = newEncoder.Next

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

func (e *EncoderFullLocked) Flush(
	ctx context.Context,
	callback CallbackPacketReceiver,
) (_err error) {
	defer e.checkCallCount(ctx)()
	logger.Tracef(ctx, "Flush")
	defer func() { logger.Tracef(ctx, "/Flush: %v", _err) }()

	defer func() {
		if _err == nil {
			if e.IsDirtyValue.Load() {
				logger.Errorf(ctx, "%v is still dirty after flush; forcing IsDirty:false", e)
				e.IsDirtyValue.Store(false)
			}
		}
	}()

	caps := e.codec.Capabilities()
	logger.Tracef(ctx, "Capabilities: %08x", caps)

	if caps&astiav.CodecCapabilityDelay == 0 {
		logger.Tracef(ctx, "the encoder has no delay, nothing to flush")
		return nil
	}

	logger.Tracef(ctx, "sending the FLUSH REQUEST pseudo-frame")
	err := e.codecContext.SendFrame(nil)
	switch {
	case err == nil:
		// flushing had just been initiated
	case errors.Is(err, astiav.ErrEof):
		return nil // the encoder is already flushed
	default:
		return fmt.Errorf("unable to send the FLUSH REQUEST pseudo-frame: %w", err)
	}

	err = e.Drain(ctx, callback)
	if err != nil {
		return fmt.Errorf("unable to drain: %w", err)
	}

	if caps&astiav.CodecCapabilityEncoderFlush != 0 {
		logger.Tracef(ctx, "flushing buffers")
		e.codecContext.FlushBuffers()
	} else {
		logger.Warnf(ctx, "the encoder has no flush capability, reinitializing the encoder")
		err := e.reinitEncoder(ctx)
		if err != nil {
			return fmt.Errorf("unable to reinit the encoder after draining: %w", err)
		}
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
				e.IsDirtyValue.Store(false)
				return nil
			}
			if isEAgain {
				if caps&astiav.CodecCapabilityDelay == 0 {
					e.IsDirtyValue.Store(false)
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
	return e.IsDirtyValue.Load()
}

func (e *EncoderFullLocked) LockDo(ctx context.Context, fn func(context.Context, Encoder) error) error {
	return fn(ctx, e)
}

func (e *EncoderFullLocked) checkCallCount(ctx context.Context) context.CancelFunc {
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
