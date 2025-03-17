package avpipeline

import (
	"context"
	"fmt"
	"io"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/xsync"
)

const (
	CodecNameCopy = "copy"
)

type Encoder interface {
	fmt.Stringer
	io.Closer
	Codec() *astiav.Codec
	CodecContext() *astiav.CodecContext
	ToCodecParameters(cp *astiav.CodecParameters) error
	HardwareDeviceContext() *astiav.HardwareDeviceContext
	HardwarePixelFormat() astiav.PixelFormat
	SendFrame(context.Context, *astiav.Frame) error
	SetQuality(context.Context, Quality) error
}

type EncoderFullBackend = Codec
type EncoderFull struct {
	*EncoderFullBackend
}

var _ Encoder = (*EncoderFull)(nil)

func NewEncoder(
	ctx context.Context,
	codecName string,
	codecParameters *astiav.CodecParameters,
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	timeBase astiav.Rational,
	options *astiav.Dictionary,
	flags int,
) (_ret Encoder, _err error) {
	if codecName == CodecNameCopy {
		return EncoderCopy{}, nil
	}
	c, err := newCodec(
		ctx,
		codecName,
		codecParameters,
		true,
		hardwareDeviceType,
		hardwareDeviceName,
		timeBase,
		options,
		flags,
	)
	if err != nil {
		return nil, err
	}
	return &EncoderFull{EncoderFullBackend: c}, nil
}

func (e *EncoderFull) String() string {
	return fmt.Sprintf("Encoder(%s)", e.codec.Name())
}

func (e *EncoderFull) SetQuality(
	ctx context.Context,
	q Quality,
) (_err error) {
	logger.Tracef(ctx, "SetQuality(ctx, %#+v)", q)
	return xsync.DoA2R1(ctx, &e.locker, e.setQualityNoLock, ctx, q)
}

func (e *EncoderFull) setQualityNoLock(
	ctx context.Context,
	q Quality,
) (_err error) {
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
		return fmt.Errorf("dynamically changing the quality is not implemented, yet", e.codec.Name())
	}
}

func (e *EncoderFull) SendFrame(
	ctx context.Context,
	f *astiav.Frame,
) error {
	return xsync.DoR1(ctx, &e.locker, func() error {
		return e.CodecContext().SendFrame(f)
	})
}
