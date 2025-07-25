package codec

import (
	"context"
	"fmt"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
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
		false,
		CodecParams{
			CodecName:          codecName,
			CodecParameters:    _codecParameters,
			HardwareDeviceType: hardwareDeviceType,
			HardwareDeviceName: hardwareDeviceName,
			TimeBase:           astiav.NewRational(0, 0),
			Options:            options,
			Flags:              flags,
		},
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
		return d.codecContext.SendPacket(p)
	})
}

func (d *Decoder) ReceiveFrame(
	ctx context.Context,
	f *astiav.Frame,
) error {
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &d.locker, func() error {
		return d.codecContext.ReceiveFrame(f)
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

func (d *Decoder) SetLowLatency(
	ctx context.Context,
	v bool,
) (_err error) {
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &d.locker, func() (_err error) {
		codecName := d.codec.Name()
		logger.Debugf(ctx, "SetLowLatency(ctx): %s:%v: %v", codecName, d.InitParams.HardwareDeviceType, v)
		defer func() {
			logger.Debugf(ctx, "/SetLowLatency(ctx): %s:%v: %v: %v", codecName, d.InitParams.HardwareDeviceType, v, _err)
		}()
		defer func() {
			if _err != nil {
				_err = fmt.Errorf("%s: %w", codecName, _err)
			}
		}()

		codecWords := strings.Split(codecName, "_")
		if len(codecWords) != 2 {
			return d.setLowLatencyGeneric(ctx, v)
		}
		codecModifier := codecWords[1]
		switch strings.ToLower(codecModifier) {
		case "mediacodec":
			return d.setLowLatencyMediacodec(ctx, v)
		}
		return d.setLowLatencyGeneric(ctx, v)
	})
}

func (d *Decoder) setLowLatencyGeneric(
	ctx context.Context,
	v bool,
) error {
	logger.Infof(ctx, "SetLowLatency (Generic): %v", v)
	return fmt.Errorf("not implemented, yet")
}
