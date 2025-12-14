package codec

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/resource"
	"github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/quality"
)

const (
	encoderDebug = true
)

type CallbackPacketReceiver func(
	context.Context,
	*EncoderFullLocked,
	astiav.CodecCapabilities,
	*astiav.Packet,
) error

type Encoder interface {
	fmt.Stringer
	Closer
	Codec() *astiav.Codec
	CodecContext() *astiav.CodecContext
	MediaType() astiav.MediaType
	ToCodecParameters(cp *astiav.CodecParameters) error
	HardwareDeviceContext() *astiav.HardwareDeviceContext
	HardwarePixelFormat() astiav.PixelFormat
	TimeBase() astiav.Rational
	SendFrame(context.Context, *astiav.Frame) error
	ReceivePacket(context.Context, *astiav.Packet) error
	GetQuality(ctx context.Context) Quality
	SetQuality(context.Context, Quality, condition.Condition) error
	GetResolution(ctx context.Context) *Resolution
	SetResolution(context.Context, Resolution, condition.Condition) error
	SetForceNextKeyFrame(ctx context.Context, v bool) error
	Flush(ctx context.Context, callback CallbackPacketReceiver) error
	Drain(ctx context.Context, callback CallbackPacketReceiver) error
	IsDirty() bool
	GetPCMAudioFormat(ctx context.Context) *PCMAudioFormat
	LockDo(ctx context.Context, fn func(context.Context, Encoder) error) error
}
type EncoderInput = resource.GetterInput

type SwitchEncoderParams struct {
	When       condition.Condition
	Quality    quality.Quality
	Resolution *Resolution
}

type Resolution = types.Resolution

type PCMAudioFormat struct {
	SampleFormat  astiav.SampleFormat
	SampleRate    int
	ChannelLayout astiav.ChannelLayout
	ChunkSize     int
}

func (pcmFmt PCMAudioFormat) String() string {
	return fmt.Sprintf("(%s, %d Hz, %s, chunk %d)", pcmFmt.SampleFormat, pcmFmt.SampleRate, pcmFmt.ChannelLayout, pcmFmt.ChunkSize)
}

func (pcmFmt PCMAudioFormat) Equal(other PCMAudioFormat) bool {
	channelLayoutEqual, err := pcmFmt.ChannelLayout.Compare(other.ChannelLayout)
	if err != nil {
		logger.Errorf(context.TODO(), "unable to compare channel layouts: %v", err)
		return false
	}
	return pcmFmt.SampleFormat == other.SampleFormat &&
		pcmFmt.SampleRate == other.SampleRate &&
		channelLayoutEqual &&
		(pcmFmt.ChunkSize == other.ChunkSize && pcmFmt.ChunkSize == 0 && other.ChunkSize == 0)
}

type ErrNotDummy struct{}

func (ErrNotDummy) Error() string {
	return "not a dummy encoder"
}

type Resources = resource.Resources

func IsDummyEncoder(encoder Encoder) bool {
	return IsEncoderCopy(encoder) || IsEncoderRaw(encoder)
}
