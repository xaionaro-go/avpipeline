// encoder_raw.go implements a "raw" encoder.

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

func (EncoderRaw) Codec(context.Context) *astiav.Codec {
	return nil
}

func (EncoderRaw) CodecContext(context.Context) *astiav.CodecContext {
	return nil
}

func (EncoderRaw) MediaType(context.Context) astiav.MediaType {
	panic(fmt.Errorf("'raw' needs to be processed manually"))
}

func (EncoderRaw) ToCodecParameters(context.Context, *astiav.CodecParameters) error {
	return nil
}

func (EncoderRaw) HardwareDeviceContext(context.Context) *astiav.HardwareDeviceContext {
	return nil
}

func (EncoderRaw) HardwarePixelFormat(context.Context) astiav.PixelFormat {
	return 0
}

func (EncoderRaw) HardwareFramesContext(context.Context) *astiav.HardwareFramesContext {
	return nil
}

func (EncoderRaw) TimeBase(context.Context) astiav.Rational {
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

func (EncoderRaw) SetForceNextKeyFrame(ctx context.Context, v bool) error {
	return nil
}

func (EncoderRaw) Flush(context.Context, CallbackPacketReceiver) error {
	return nil
}

func (EncoderRaw) IsDirty() bool {
	return false
}

func (EncoderRaw) LockDo(ctx context.Context, fn func(context.Context, Encoder) error) error {
	return fn(ctx, EncoderRaw{})
}

func IsEncoderRaw(encoder Encoder) bool {
	_, ok := encoder.(EncoderRaw)
	return ok
}
