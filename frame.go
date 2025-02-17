package avpipeline

import (
	"fmt"
	"time"

	"github.com/asticode/go-astiav"
)

type Frame struct {
	*astiav.Frame
	InputStream        *astiav.Stream
	InputFormatContext *astiav.FormatContext
	Decoder            *Decoder
	Packet             *astiav.Packet
	RAMFrame           *astiav.Frame
}

func (f *Frame) MaxPosition() time.Duration {
	return toDuration(f.InputFormatContext.Duration(), 1/float64(astiav.TimeBase))
}

func (f *Frame) Position() time.Duration {
	return toDuration(f.Pts(), f.InputStream.TimeBase().Float64())
}

func (f *Frame) PositionInBytes() int64 {
	return f.Packet.Pos()
}

func (f *Frame) FrameDuration() time.Duration {
	return toDuration(f.Packet.Duration(), f.InputStream.TimeBase().Float64())
}

func (f *Frame) TransferFromHardwareToRAM() error {
	if f.Decoder.HardwareDeviceContext() == nil {
		return fmt.Errorf("is not a hardware-backed frame")
	}

	if f.Frame.PixelFormat() != f.Decoder.HardwarePixelFormat() {
		return fmt.Errorf("unexpected pixel format: %v != %v", f.Frame.PixelFormat(), f.Decoder.HardwarePixelFormat())
	}

	if err := f.Frame.TransferHardwareData(f.RAMFrame); err != nil {
		return fmt.Errorf("failed to transfer frame from hardware decoder to RAM: %w", err)
	}

	f.RAMFrame.SetPts(f.Frame.Pts())
	f.Frame.Unref()
	f.Frame = f.RAMFrame
	return nil
}

func toDuration(ts int64, timeBase float64) time.Duration {
	seconds := float64(ts) * float64(timeBase)
	return time.Duration(float64(time.Second) * seconds)
}
