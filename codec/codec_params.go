package codec

import (
	"context"

	"github.com/asticode/go-astiav"
)

type CodecParams struct {
	CodecName          string
	CodecParameters    *astiav.CodecParameters
	HardwareDeviceType astiav.HardwareDeviceType
	HardwareDeviceName HardwareDeviceName
	TimeBase           astiav.Rational
	Options            *astiav.Dictionary
	Flags              int
}

func (p CodecParams) Clone(ctx context.Context) CodecParams {
	if p.CodecParameters != nil {
		cp := astiav.AllocCodecParameters()
		setFinalizerFree(ctx, cp)
		p.CodecParameters.Copy(cp)
		p.CodecParameters = cp
	}
	if p.Options != nil {
		opts := astiav.NewDictionary()
		setFinalizerFree(ctx, opts)
		opts.Unpack(p.Options.Pack())
		p.Options = opts
	}
	return p
}
