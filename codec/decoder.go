package codec

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/xsync"
)

type Decoder struct {
	*Codec
}

func NewDecoder(
	ctx context.Context,
	codecName string,
	codecParameters *astiav.CodecParameters,
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	options *astiav.Dictionary,
	flags int,
) (_ret *Decoder, _err error) {
	_codecParameters := astiav.AllocCodecParameters()
	defer _codecParameters.Free()
	codecParameters.Copy(_codecParameters)
	c, err := newCodec(
		ctx,
		codecName,
		_codecParameters,
		false,
		hardwareDeviceType,
		hardwareDeviceName,
		astiav.NewRational(0, 0),
		options,
		flags,
	)
	if err != nil {
		return nil, err
	}
	return &Decoder{c}, nil
}

func (d *Decoder) String() string {
	return fmt.Sprintf("Decoder(%s)", d.codec.Name())
}

func (d *Decoder) SendPacket(
	ctx context.Context,
	p *astiav.Packet,
) error {
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &d.locker, func() error {
		return d.CodecContext().SendPacket(p)
	})
}

func (d *Decoder) ReceiveFrame(
	ctx context.Context,
	f *astiav.Frame,
) error {
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &d.locker, func() error {
		return d.CodecContext().ReceiveFrame(f)
	})
}

func (d *Decoder) GetQuality(
	ctx context.Context,
) Quality {
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &d.locker, func() Quality {
		bitRate := d.codecContext.BitRate()
		if bitRate != 0 {
			return quality.ConstantBitrate(d.codecContext.BitRate())
		}
		return nil
	})
}
