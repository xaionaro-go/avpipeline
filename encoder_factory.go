package avpipeline

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/xsync"
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
	VideoEncoders      []Encoder
	AudioEncoders      []Encoder
	Locker             xsync.Mutex
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
) (_ret Encoder, _err error) {
	return xsync.DoA2R2(ctx, &f.Locker, f.newEncoderNoLock, ctx, pkt)
}

func (f *NaiveEncoderFactory) newEncoderNoLock(
	ctx context.Context,
	pkt InputPacket,
) (_ret Encoder, _err error) {
	codecParametersOrig := pkt.CodecParameters()
	codecParameters := astiav.AllocCodecParameters()
	defer codecParameters.Free()
	codecParametersOrig.Copy(codecParameters)

	defer func() {
		if _err != nil {
			return
		}
		switch codecParameters.MediaType() {
		case astiav.MediaTypeVideo:
			f.VideoEncoders = append(f.VideoEncoders, _ret)
		case astiav.MediaTypeAudio:
			f.AudioEncoders = append(f.AudioEncoders, _ret)
		}
	}()

	switch codecParameters.MediaType() {
	case astiav.MediaTypeVideo:
		return NewEncoder(ctx, f.VideoCodec, codecParameters, f.HardwareDeviceType, f.HardwareDeviceName, pkt.TimeBase(), nil, 0)
	case astiav.MediaTypeAudio:
		return NewEncoder(ctx, f.AudioCodec, codecParameters, 0, "", pkt.TimeBase(), nil, 0)
	default:
		return nil, fmt.Errorf("only audio and video tracks are supported by NaiveEncoderFactory, yet")
	}
}
