package codec

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/condition"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/avpipeline/types"
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
	SendFrame(context.Context, *astiav.Frame) error
	ReceivePacket(context.Context, *astiav.Packet) error
	SetQuality(context.Context, Quality, condition.Condition) error
}

type EncoderFullBackend = Codec
type EncoderFull struct {
	*EncoderFullBackend
	Params EncoderParams
	InitTS time.Time
	Next   typing.Optional[SwitchEncoderParams]
}

type SwitchEncoderParams struct {
	When    condition.Condition
	Quality quality.Quality
}

var _ Encoder = (*EncoderFull)(nil)

type EncoderParams struct {
	CodecName          string
	CodecParameters    *astiav.CodecParameters
	HardwareDeviceType astiav.HardwareDeviceType
	HardwareDeviceName HardwareDeviceName
	TimeBase           astiav.Rational
	Options            *astiav.Dictionary
	Flags              int
}

func NewEncoder(
	ctx context.Context,
	params EncoderParams,
) (_ret Encoder, _err error) {
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
	params EncoderParams,
	overrideQuality Quality,
) (_ret *EncoderFull, _err error) {
	if params.CodecParameters != nil {
		cp := astiav.AllocCodecParameters()
		params.CodecParameters.Copy(cp)
		params.CodecParameters = cp
		runtime.SetFinalizer(params.CodecParameters, func(cp *astiav.CodecParameters) {
			cp.Free()
		})
	}
	if params.Options != nil {
		opts := astiav.NewDictionary()
		opts.Unpack(params.Options.Pack())
		params.Options = opts
		runtime.SetFinalizer(params.Options, func(opts *astiav.Dictionary) {
			opts.Free()
		})
	}
	if overrideQuality != nil {
		if params.CodecParameters == nil {
			params.CodecParameters = astiav.AllocCodecParameters()
		}
		err := overrideQuality.Apply(params.CodecParameters)
		if err != nil {
			return nil, fmt.Errorf("unable to apply quality override %#+v: %w", overrideQuality, err)
		}
	}
	c, err := newCodec(
		ctx,
		params.CodecName,
		params.CodecParameters,
		true,
		params.HardwareDeviceType,
		params.HardwareDeviceName,
		params.TimeBase,
		params.Options,
		params.Flags,
	)
	if err != nil {
		return nil, err
	}
	return &EncoderFull{
		EncoderFullBackend: c,
		Params:             params,
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
		return e.sendFrameNoLock(ctx, f)
	})
}

func (e *EncoderFull) sendFrameNoLock(
	_ context.Context,
	f *astiav.Frame,
) error {
	return e.CodecContext().SendFrame(f)
}

func (e *EncoderFull) ReceivePacket(
	ctx context.Context,
	p *astiav.Packet,
) error {
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &e.locker, func() error {
		return e.receivePacketNoLock(ctx, p)
	})
}

func (e *EncoderFull) receivePacketNoLock(
	ctx context.Context,
	p *astiav.Packet,
) (err error) {
	err = e.CodecContext().ReceivePacket(p)
	if !e.Next.IsSet() {
		return
	}

	next := e.Next.Get()
	if next.When == nil || next.When.Match(ctx, types.BuildInputPacket(p, nil, nil)) {
		e.Next.Unset()
		q := next.Quality
		qErr := e.setQualityNow(ctx, q)
		if qErr != nil {
			logger.Errorf(ctx, "unable to set quality to %v: %v", q, qErr)
		}
	}

	return err
}

func (e *EncoderFull) SetQuality(
	ctx context.Context,
	q Quality,
	when condition.Condition,
) (_err error) {
	logger.Debugf(ctx, "SetQuality(ctx, %#+v)", q)
	defer func() { logger.Tracef(ctx, "/SetQuality(ctx, %#+v): %v", q, _err) }()
	return xsync.DoA3R1(xsync.WithNoLogging(ctx, true), &e.locker, e.setQualityNoLock, ctx, q, when)
}

func (e *EncoderFull) setQualityNoLock(
	ctx context.Context,
	q Quality,
	when condition.Condition,
) (_err error) {
	if when == nil {
		return e.setQualityNow(ctx, q)
	}
	logger.Tracef(ctx, "setQualityNoLock(): will set the new quality when condition '%s' is satisfied", when)
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
	logger.Debugf(ctx, "setQualityNow(ctx, %#+v)", q)
	defer func() { logger.Debugf(ctx, "/setQualityNow(ctx, %#+v): %v", q, _err) }()
	codecName := e.codec.Name()
	defer func() {
		if _err != nil {
			_err = fmt.Errorf("%s: %w", codecName, _err)
		}
	}()
	switch codecName {
	case "mediacodec":
		return e.setQualityMediacodec(ctx, q)
	default:
		return e.setQualityGeneric(ctx, q)
	}
}

func (e *EncoderFull) Close(ctx context.Context) error {
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &e.locker, e.closeNoLock, ctx)
}

func (e *EncoderFull) closeNoLock(ctx context.Context) error {
	var result []error
	if err := e.EncoderFullBackend.Close(ctx); err != nil {
		result = append(result, fmt.Errorf("unable to close the old encoder: %w", err))
	}
	e.Params.Options = nil
	e.Params.CodecParameters = nil
	return errors.Join(result...)
}

func (e *EncoderFull) setQualityGeneric(
	ctx context.Context,
	q Quality,
) (_err error) {
	newEncoder, err := newEncoder(ctx, e.Params, q)
	if err != nil {
		return fmt.Errorf("unable to initialize new encoder for quality %#+v: %w", q, err)
	}
	if err := e.closeNoLock(ctx); err != nil {
		logger.Errorf(ctx, "unable to close the old encoder: %v", err)
	}
	*e = *newEncoder
	return nil
}
