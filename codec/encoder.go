package codec

import (
	"context"
	"errors"
	"fmt"
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
	CodecNameRaw  = "raw"
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
	logger.Tracef(ctx, "NewEncoder")
	defer func() { logger.Tracef(ctx, "/NewEncoder: %T %v", _ret, _err) }()
	switch params.CodecName {
	case CodecNameCopy:
		return EncoderCopy{}, nil
	case CodecNameRaw:
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
