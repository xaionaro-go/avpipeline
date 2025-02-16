package avpipeline

import (
	"context"

	"github.com/asticode/go-astiav"
)

type Decoder struct {
	*Codec
}

func NewDecoder(
	ctx context.Context,
	codecName string,
	codecParameters *astiav.CodecParameters,
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	options *astiav.Dictionary,
	flags int,
) (_ret *Decoder, _err error) {
	c, err := newCodec(
		ctx,
		codecName,
		codecParameters,
		false,
		hardwareDeviceType,
		hardwareDeviceName,
		options,
		flags,
	)
	if err != nil {
		return nil, err
	}
	return &Decoder{c}, nil
}

type DecoderFactory interface {
	NewDecoder(ctx context.Context, pkt *astiav.Packet) (*Decoder, error)
}
