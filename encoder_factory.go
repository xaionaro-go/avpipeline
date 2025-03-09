package avpipeline

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type EncoderFactory interface {
	fmt.Stringer
	NewEncoder(ctx context.Context, pkt InputPacket) (Encoder, error)
}

type NaiveEncoderFactory struct {
	VideoCodec         string
	AudioCodec         string
	HardwareDeviceType astiav.HardwareDeviceType
	HardwareDeviceName HardwareDeviceName
}

var _ EncoderFactory = (*NaiveEncoderFactory)(nil)

func NewNaiveEncoderFactory(
	videoCodec string,
	audioCodec string,
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
) *NaiveEncoderFactory {
	return &NaiveEncoderFactory{
		VideoCodec:         videoCodec,
		AudioCodec:         audioCodec,
		HardwareDeviceType: hardwareDeviceType,
		HardwareDeviceName: hardwareDeviceName,
	}
}

func (f *NaiveEncoderFactory) String() string {
	return fmt.Sprintf("NaiveEncoderFactory(%s/%s)", f.VideoCodec, f.AudioCodec)
}

func (f *NaiveEncoderFactory) NewEncoder(
	ctx context.Context,
	pkt InputPacket,
) (Encoder, error) {
	codecParametersOrig := pkt.CodecParameters()
	codecParameters := astiav.AllocCodecParameters()
	defer codecParameters.Free()
	codecParametersOrig.Copy(codecParameters)
	switch codecParameters.MediaType() {
	case astiav.MediaTypeVideo:
		return NewEncoder(ctx, f.VideoCodec, codecParameters, f.HardwareDeviceType, f.HardwareDeviceName, pkt.TimeBase(), nil, 0)
	case astiav.MediaTypeAudio:
		return NewEncoder(ctx, f.AudioCodec, codecParameters, 0, "", pkt.TimeBase(), nil, 0)
	default:
		return nil, fmt.Errorf("only audio and video tracks are supported by NaiveEncoderFactory, yet")
	}
}
