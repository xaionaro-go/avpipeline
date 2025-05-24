package codec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

const (
	CodecNameCopy = "copy"
)

type Encoder interface {
	fmt.Stringer
	Closer
	Codec() *astiav.Codec
	CodecContext() *astiav.CodecContext
	ToCodecParameters(cp *astiav.CodecParameters) error
	HardwareDeviceContext() *astiav.HardwareDeviceContext
	HardwarePixelFormat() astiav.PixelFormat
	TimeBase() astiav.Rational
	SendFrame(context.Context, *astiav.Frame) error
	ReceivePacket(context.Context, *astiav.Packet) error
	GetQuality(ctx context.Context) Quality
	SetQuality(context.Context, Quality, condition.Condition) error
	Reset(ctx context.Context) error
}

type EncoderFullBackend = Codec
type EncoderFull struct {
	*EncoderFullBackend
	Quality Quality
	InitTS  time.Time
	Next    typing.Optional[SwitchEncoderParams]
}

type SwitchEncoderParams struct {
	When    condition.Condition
	Quality quality.Quality
}

var _ Encoder = (*EncoderFull)(nil)

func NewEncoder(
	ctx context.Context,
	params CodecParams,
) (_ret Encoder, _err error) {
	logger.Tracef(ctx, "NewEncoder")
	defer func() { logger.Tracef(ctx, "/NewEncoder: %T %v", _ret, _err) }()
	if params.CodecName == CodecNameCopy {
		return EncoderCopy{}, nil
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
	logger.Tracef(ctx, "newEncoder")
	defer func() { logger.Tracef(ctx, "/newEncoder: %p %v", _ret, _err) }()
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
) error {
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
) error {
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
	if next.When == nil || next.When.Match(ctx, packet.BuildInput(p, nil, nil)) {
		e.Next.Unset()
		q := next.Quality
		qErr := e.setQualityNow(ctx, q)
		if qErr != nil {
			logger.Errorf(ctx, "unable to set quality to %v: %v", q, qErr)
		}
	}

	return err
}

func (e *EncoderFull) GetQuality(
	ctx context.Context,
) Quality {
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &e.locker, e.getQualityLocked, ctx)
}

func (e *EncoderFull) getQualityLocked(
	ctx context.Context,
) Quality {
	if e.codecContext == nil {
		panic(fmt.Errorf("e.codecContext == nil"))
	}
	bitRate := e.codecContext.BitRate()
	if bitRate != 0 {
		return quality.ConstantBitrate(e.codecContext.BitRate())
	}
	return nil
}

func (e *EncoderFull) SetQuality(
	ctx context.Context,
	q Quality,
	when condition.Condition,
) (_err error) {
	logger.Debugf(ctx, "SetQuality(ctx, %#+v)", q)
	defer func() { logger.Tracef(ctx, "/SetQuality(ctx, %#+v): %v", q, _err) }()
	return xsync.DoA3R1(xsync.WithNoLogging(ctx, true), &e.locker, e.setQualityLocked, ctx, q, when)
}

func (e *EncoderFull) setQualityLocked(
	ctx context.Context,
	q Quality,
	when condition.Condition,
) (_err error) {
	if when == nil {
		return e.setQualityNow(ctx, q)
	}
	logger.Tracef(ctx, "setQualityLocked(): will set the new quality when condition '%s' is satisfied", when)
	e.Next.Set(SwitchEncoderParams{
		When:    when,
		Quality: q,
	})
	return nil
}

func (e *EncoderFull) setQualityNow(
	ctx context.Context,
	q Quality,
) (_err error) {
	codecName := e.codec.Name()
	logger.Debugf(ctx, "setQualityNow(ctx, %T(%v)): %s", q, q, codecName)
	defer func() { logger.Debugf(ctx, "/setQualityNow(ctx, %T(%v)): %s: %v", q, q, codecName, _err) }()
	defer func() {
		if _err != nil {
			_err = fmt.Errorf("%s: %w", codecName, _err)
		}
	}()
	codecWords := strings.Split(codecName, "_")
	if len(codecWords) != 2 {
		return e.setQualityGeneric(ctx, q)
	}
	codecModifier := codecWords[1]
	switch strings.ToLower(codecModifier) {
	case "mediacodec":
		return e.setQualityMediacodec(ctx, q)
	default:
		return e.setQualityGeneric(ctx, q)
	}
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

func (e *EncoderFull) setQualityGeneric(
	ctx context.Context,
	q Quality,
) (_err error) {
	if q == e.Quality {
		logger.Debugf(ctx, "the quality is already %T(%v)", q, q)
		return nil
	}
	logger.Infof(ctx, "SetQuality (generic): %T(%v)", q, q)
	e.InitParams.CodecParameters.SetBitRate(0)
	// TODO: consider user Reset() instead of reimplementing the same logic
	newEncoder, err := newEncoder(ctx, e.InitParams, q)
	if err != nil {
		return fmt.Errorf("unable to initialize new encoder for quality %#+v: %w", q, err)
	}
	if err := e.closeLocked(ctx); err != nil {
		logger.Errorf(ctx, "unable to close the old encoder: %v", err)
	}
	*e = *newEncoder
	e.Quality = q
	return nil
}
