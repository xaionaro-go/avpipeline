package avpipeline

import (
	"context"
	"fmt"
	"io"

	"github.com/asticode/go-astiav"
)

const (
	CodecNameCopy = "copy"
)

type Encoder interface {
	fmt.Stringer
	io.Closer
	Codec() *astiav.Codec
	CodecContext() *astiav.CodecContext
	HardwareDeviceContext() *astiav.HardwareDeviceContext
	HardwarePixelFormat() astiav.PixelFormat
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
	return "Encoder"
}
