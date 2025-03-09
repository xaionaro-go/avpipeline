package avpipeline

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type DecoderFactory interface {
	fmt.Stringer

	NewDecoder(ctx context.Context, pkt InputPacket) (*Decoder, error)
}

type NaiveDecoderFactory struct {
	HardwareDeviceType astiav.HardwareDeviceType
	HardwareDeviceName HardwareDeviceName
}

var _ DecoderFactory = (*NaiveDecoderFactory)(nil)

func NewNaiveDecoderFactory(
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
) *NaiveDecoderFactory {
	return &NaiveDecoderFactory{
		HardwareDeviceType: hardwareDeviceType,
		HardwareDeviceName: hardwareDeviceName,
	}
}

func (f *NaiveDecoderFactory) NewDecoder(
	ctx context.Context,
	pkt InputPacket,
) (*Decoder, error) {
	codecParameters := pkt.CodecParameters()
	if codecParameters.MediaType() != astiav.MediaTypeVideo {
		return NewDecoder(
			ctx,
			"",
			codecParameters,
			0,
			"",
			nil,
			0,
		)
	}
	return NewDecoder(
		ctx,
		"",
		codecParameters,
		f.HardwareDeviceType,
		f.HardwareDeviceName,
		nil,
		0,
	)
}

func (f *NaiveDecoderFactory) String() string {
	return "NaiveDecoderFactory"
}
