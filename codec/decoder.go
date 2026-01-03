// decoder.go provides the public Decoder API and initialization logic.

package codec

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

type Decoder DecoderLocked

func NewDecoder(
	ctx context.Context,
	decInput DecoderInput,
) (_ret *Decoder, _err error) {
	_codecParameters := astiav.AllocCodecParameters()
	defer _codecParameters.Free()
	decInput.CodecParameters.Copy(_codecParameters)
	input := Input{
		IsEncoder: false,
		Params: CodecParams{
			CodecName:             decInput.CodecName,
			CodecParameters:       _codecParameters,
			HardwareDeviceType:    decInput.HardwareDeviceType,
			HardwareDeviceName:    decInput.HardwareDeviceName,
			ErrorRecognitionFlags: decInput.ErrorRecognitionFlags,
			TimeBase:              astiav.NewRational(0, 0),
			CustomOptions:         decInput.CustomOptions,
			HWDevFlags:            decInput.Flags,
			ResourceManager:       decInput.ResourceManager,
			Options:               decInput.Options,
		},
	}
	c, err := newCodec(
		ctx,
		input,
	)
	if err != nil {
		return nil, err
	}
	return &Decoder{Codec: c}, nil
}

func (d *Decoder) locked() *DecoderLocked {
	return (*DecoderLocked)(d)
}

func (d *Decoder) String() string {
	return d.locked().String()
}

func (d *Decoder) SendPacket(
	ctx context.Context,
	p *astiav.Packet,
) error {
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &d.locker, d.locked().SendPacket, ctx, p)
}

func (d *Decoder) ReceiveFrame(
	ctx context.Context,
	f *astiav.Frame,
) error {
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &d.locker, d.locked().ReceiveFrame, ctx, f)
}

func (d *Decoder) GetQuality(
	ctx context.Context,
) Quality {
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &d.locker, d.locked().GetQuality, ctx)
}

func (d *Decoder) SetLowLatency(
	ctx context.Context,
	v bool,
) (_err error) {
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &d.locker, d.locked().SetLowLatency, ctx, v)
}

func (d *Decoder) Reset(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Reset")
	defer func() { logger.Debugf(ctx, "/Reset: %v", _err) }()
	return xsync.DoA1R1(ctx, &d.locker, d.locked().Reset, ctx)
}

func (d *Decoder) Flush(
	ctx context.Context,
	callback CallbackFrameReceiver,
) error {
	return xsync.DoA2R1(ctx, &d.locker, d.locked().Flush, ctx, callback)
}

func (d *Decoder) Drain(
	ctx context.Context,
	callback CallbackFrameReceiver,
) error {
	return xsync.DoA2R1(ctx, &d.locker, d.locked().Drain, ctx, callback)
}

func (d *Decoder) LockDo(
	ctx context.Context,
	callback func(context.Context, *DecoderLocked) error,
) error {
	return xsync.DoR1(ctx, &d.locker, func() error {
		return callback(ctx, d.locked())
	})
}

func (d *Decoder) IsDirty(
	ctx context.Context,
) bool {
	return d.isDirty
}
