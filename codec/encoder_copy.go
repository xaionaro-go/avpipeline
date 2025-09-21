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

type ErrCopyEncoder struct{}

func (ErrCopyEncoder) Error() string {
	return "'copy' encoder"
}

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
	return ErrCopyEncoder{}
}

func (EncoderCopy) ReceivePacket(context.Context, *astiav.Packet) error {
	return ErrCopyEncoder{}
}

func (EncoderCopy) GetQuality(
	ctx context.Context,
) Quality {
	return nil
}

func (EncoderCopy) SetQuality(context.Context, Quality, condition.Condition) error {
	return ErrCopyEncoder{}
}

func (EncoderCopy) GetResolution(ctx context.Context) *Resolution {
	return nil
}
func (EncoderCopy) SetResolution(context.Context, Resolution, condition.Condition) error {
	return ErrCopyEncoder{}
}

func (EncoderCopy) Reset(context.Context) error {
	return nil
}

func (EncoderCopy) GetPCMAudioFormat(ctx context.Context) *PCMAudioFormat {
	return nil
}

func (EncoderCopy) SetForceNextKeyFrame(ctx context.Context, v bool) error {
	return ErrCopyEncoder{}
}

func (EncoderCopy) Flush(ctx context.Context, callback CallbackPacketReceiver) error {
	return nil
}

func (EncoderCopy) Drain(ctx context.Context, callback CallbackPacketReceiver) error {
	return nil
}

func (EncoderCopy) IsDirty() bool {
	return false
}

func (EncoderCopy) LockDo(ctx context.Context, fn func(context.Context, Encoder) error) error {
	return fn(ctx, EncoderCopy{})
}

func IsEncoderCopy(encoder Encoder) bool {
	_, ok := encoder.(EncoderCopy)
	return ok
}
