package codec

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
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
	GetResolution(ctx context.Context) (uint32, uint32)
	SetResolution(context.Context, uint32, uint32, condition.Condition) error
	Reset(ctx context.Context) error
}

type EncoderFullBackend = Codec
type EncoderFull struct {
	*EncoderFullBackend
	Quality Quality
	InitTS  time.Time
	Next    typing.Optional[SwitchEncoderParams]
}

type Resolution struct {
	Width  uint32
	Height uint32
}

type SwitchEncoderParams struct {
	When       condition.Condition
	Quality    quality.Quality
	Resolution *Resolution
}

var _ Encoder = (*EncoderFull)(nil)

func NewEncoder(
	ctx context.Context,
	params CodecParams,
) (_ret Encoder, _err error) {
	logger.Tracef(ctx, "NewEncoder(%#+v)", params)
	defer func() { logger.Tracef(ctx, "/NewEncoder(%#+v): %T %v", params, _ret, _err) }()
	switch params.CodecName {
	case NameCopy:
		return EncoderCopy{}, nil
	case NameRaw:
		return EncoderRaw{}, nil
	}
	e, err := newEncoder(ctx, params, nil)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func newEncoder(
	ctx context.Context,
	params CodecParams,
	overrideQuality Quality,
) (_ret *EncoderFull, _err error) {
	logger.Tracef(ctx, "newEncoder(ctx, %#+v, %#+v)", params, overrideQuality)
	defer func() { logger.Tracef(ctx, "/newEncoder(ctx, %#+v, %#+v): %p %v", params, overrideQuality, _ret, _err) }()
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
	c, err := newCodec(
		ctx,
		true,
		params,
	)
	if err != nil {
		return nil, err
	}
	return &EncoderFull{
		EncoderFullBackend: c,
		InitTS:             time.Now(),
	}, nil
}

func (e *EncoderFull) String() string {
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
	_ context.Context,
	f *astiav.Frame,
) error {
	return e.codecContext.SendFrame(f)
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
			rErr := e.setResolutionNow(ctx, r.Width, r.Height)
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

func IsFakeEncoder(encoder Encoder) bool {
	return IsEncoderCopy(encoder) || IsEncoderRaw(encoder)
}

func (e *EncoderFull) reinitEncoder(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "reinitEncoder")
	defer func() { logger.Tracef(ctx, "/reinitEncoder: %v", _err) }()

	newEncoder, err := newEncoder(ctx, e.InitParams, e.Quality)
	if err != nil {
		return fmt.Errorf("unable to initialize new encoder: %w", err)
	}
	if err := e.closeLocked(ctx); err != nil {
		logger.Errorf(ctx, "unable to close the old encoder: %v", err)
	}

	logger.Tracef(ctx, "replaced the encoder with a new one (%p); the old one (%p) was is closed", newEncoder.EncoderFullBackend, e.EncoderFullBackend)

	// keeping the locker from the old codec to make sure everybody who is already locked
	// will get relevant stuff when it will get unlocked
	oldCodec := e.EncoderFullBackend
	oldCodec.codecInternals = newEncoder.codecInternals
	*e = *newEncoder
	e.EncoderFullBackend = oldCodec
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
