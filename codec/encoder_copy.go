package codec

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet/condition"
)

// TODO: delete me
type EncoderCopy struct{}

var _ Encoder = EncoderCopy{}

func (EncoderCopy) String() string {
	return "Encoder(copy)"
}

func (EncoderCopy) Close(ctx context.Context) error {
	return nil
}

func (EncoderCopy) Codec() *astiav.Codec {
	return nil
}

func (EncoderCopy) CodecContext() *astiav.CodecContext {
	return nil
}

func (EncoderCopy) MediaType() astiav.MediaType {
	panic(fmt.Errorf("'copy' needs to be processed manually"))
}

func (EncoderCopy) ToCodecParameters(cp *astiav.CodecParameters) error {
	return nil
}

func (EncoderCopy) HardwareDeviceContext() *astiav.HardwareDeviceContext {
	return nil
}

func (EncoderCopy) HardwarePixelFormat() astiav.PixelFormat {
	return 0
}

func (EncoderCopy) TimeBase() astiav.Rational {
	panic(fmt.Errorf("'copy' needs to be processed manually"))
}

func (EncoderCopy) SendFrame(context.Context, *astiav.Frame) error {
	return fmt.Errorf("'copy' needs to be processed manually")
}

func (EncoderCopy) ReceivePacket(context.Context, *astiav.Packet) error {
	return fmt.Errorf("'copy' needs to be processed manually")
}

func (EncoderCopy) GetQuality(
	ctx context.Context,
) Quality {
	return nil
}

func (EncoderCopy) SetQuality(context.Context, Quality, condition.Condition) error {
	return fmt.Errorf("'copy' implies the quality cannot be manipulated")
}

func (EncoderCopy) GetResolution(ctx context.Context) (uint32, uint32) {
	return 0, 0
}
func (EncoderCopy) SetResolution(context.Context, uint32, uint32, condition.Condition) error {
	return fmt.Errorf("'copy' implies the resolution cannot be manipulated")
}

func (EncoderCopy) Reset(context.Context) error {
	return nil
}

func IsEncoderCopy(encoder Encoder) bool {
	_, ok := encoder.(EncoderCopy)
	return ok
}
