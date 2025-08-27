package codec

import (
	"context"

	"github.com/asticode/go-astiav"
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
			CodecName:          decInput.CodecName,
			CodecParameters:    _codecParameters,
			HardwareDeviceType: decInput.HardwareDeviceType,
			HardwareDeviceName: decInput.HardwareDeviceName,
			TimeBase:           astiav.NewRational(0, 0),
			Options:            decInput.Options,
			Flags:              decInput.Flags,
		},
		ReusableResources: nil,
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

func (d *Decoder) unlocked() *DecoderLocked {
	return (*DecoderLocked)(d)
}

func (d *Decoder) String() string {
	return d.unlocked().String()
}

func (d *Decoder) SendPacket(
	ctx context.Context,
	p *astiav.Packet,
) error {
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &d.locker, d.unlocked().SendPacket, ctx, p)
}

func (d *Decoder) ReceiveFrame(
	ctx context.Context,
	f *astiav.Frame,
) error {
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &d.locker, d.unlocked().ReceiveFrame, ctx, f)
}

func (d *Decoder) GetQuality(
	ctx context.Context,
) Quality {
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &d.locker, d.unlocked().GetQuality, ctx)
}

func (d *Decoder) SetLowLatency(
	ctx context.Context,
	v bool,
) (_err error) {
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &d.locker, d.unlocked().SetLowLatency, ctx, v)
}

func (d *Decoder) Flush(
	ctx context.Context,
	callback CallbackFrameReceiver,
) error {
	return xsync.DoA2R1(ctx, &d.locker, d.unlocked().Flush, ctx, callback)
}

func (d *Decoder) Drain(
	ctx context.Context,
	callback CallbackFrameReceiver,
) error {
	return xsync.DoA2R1(ctx, &d.locker, d.unlocked().Drain, ctx, callback)
}

func (d *Decoder) LockDo(
	ctx context.Context,
	callback func(context.Context, *DecoderLocked) error,
) error {
	return xsync.DoR1(ctx, &d.locker, func() error {
		return callback(ctx, d.unlocked())
	})
}

func (d *Decoder) IsDirty(
	ctx context.Context,
) bool {
	return d.IsDirtyValue.Load()
}
