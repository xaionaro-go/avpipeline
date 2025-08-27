package codec

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet/condition"
)

type EncoderRaw struct{}

var _ Encoder = EncoderRaw{}

func (EncoderRaw) String() string {
	return "Encoder(raw)"
}

func (EncoderRaw) Close(ctx context.Context) error {
	return nil
}

func (EncoderRaw) Codec() *astiav.Codec {
	return nil
}

func (EncoderRaw) CodecContext() *astiav.CodecContext {
	return nil
}

func (EncoderRaw) MediaType() astiav.MediaType {
	panic(fmt.Errorf("'raw' needs to be processed manually"))
}

func (EncoderRaw) ToCodecParameters(cp *astiav.CodecParameters) error {
	return nil
}

func (EncoderRaw) HardwareDeviceContext() *astiav.HardwareDeviceContext {
	return nil
}

func (EncoderRaw) HardwarePixelFormat() astiav.PixelFormat {
	return 0
}

func (EncoderRaw) TimeBase() astiav.Rational {
	panic(fmt.Errorf("'raw' needs to be processed manually"))
}

func (EncoderRaw) SendFrame(context.Context, *astiav.Frame) error {
	return fmt.Errorf("'raw' needs to be processed manually")
}

func (EncoderRaw) ReceivePacket(context.Context, *astiav.Packet) error {
	return fmt.Errorf("'raw' needs to be processed manually")
}

func (EncoderRaw) GetQuality(
	ctx context.Context,
) Quality {
	return nil
}

func (EncoderRaw) SetQuality(context.Context, Quality, condition.Condition) error {
	return fmt.Errorf("'raw' implies the quality cannot be manipulated")
}

func (EncoderRaw) GetResolution(ctx context.Context) *Resolution {
	return nil
}

func (EncoderRaw) SetResolution(context.Context, Resolution, condition.Condition) error {
	return fmt.Errorf("'raw' implies the resolution cannot be manipulated")
}

func (EncoderRaw) Reset(context.Context) error {
	return nil
}

func (EncoderRaw) GetPCMAudioFormat(ctx context.Context) *PCMAudioFormat {
	return nil
}

func (EncoderRaw) Drain(context.Context, CallbackPacketReceiver) error {
	return nil
}

func (EncoderRaw) Flush(context.Context, CallbackPacketReceiver) error {
	return nil
}

func (EncoderRaw) IsDirty() bool {
	return false
}

func IsEncoderRaw(encoder Encoder) bool {
	_, ok := encoder.(EncoderRaw)
	return ok
}
