package avpipeline

import (
	"github.com/asticode/go-astiav"
)

type EncoderCopy struct{}

var _ Encoder = (*EncoderCopy)(nil)

func (EncoderCopy) String() string {
	return "Encoder(copy)"
}
func (EncoderCopy) Close() error {
	return nil
}
func (EncoderCopy) Codec() *astiav.Codec {
	return nil
}
func (EncoderCopy) CodecContext() *astiav.CodecContext {
	return nil
}
func (EncoderCopy) HardwareDeviceContext() *astiav.HardwareDeviceContext {
	return nil
}
func (EncoderCopy) HardwarePixelFormat() astiav.PixelFormat {
	return 0
}
