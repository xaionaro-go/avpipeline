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
	_codecParameters := astiav.AllocCodecParameters()
	defer _codecParameters.Free()
	codecParameters.Copy(_codecParameters)
	c, err := newCodec(
		ctx,
		codecName,
		_codecParameters,
		false,
		hardwareDeviceType,
		hardwareDeviceName,
		astiav.NewRational(0, 0),
		options,
		flags,
	)
	if err != nil {
		return nil, err
	}
	return &Decoder{c}, nil
}
