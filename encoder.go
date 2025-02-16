package avpipeline

import (
	"context"

	"github.com/asticode/go-astiav"
)

type Encoder struct {
	*Codec
}

func NewEncoder(
	ctx context.Context,
	codecName string,
	codecParameters *astiav.CodecParameters,
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	options *astiav.Dictionary,
	flags int,
) (_ret *Encoder, _err error) {
	c, err := newCodec(
		ctx,
		codecName,
		codecParameters,
		true,
		hardwareDeviceType,
		hardwareDeviceName,
		options,
		flags,
	)
	if err != nil {
		return nil, err
	}
	return &Encoder{Codec: c}, nil
}

type EncoderFactory interface {
	NewEncoder(ctx context.Context, pkt *astiav.Packet) (*Encoder, error)
}
