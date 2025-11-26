package codec

import (
	"context"
	"slices"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/resource"
)

type CodecParams struct {
	CodecName             Name
	CodecParameters       *astiav.CodecParameters
	HardwareDeviceType    HardwareDeviceType
	HardwareDeviceName    HardwareDeviceName
	ErrorRecognitionFlags astiav.ErrorRecognitionFlags
	TimeBase              astiav.Rational
	CustomOptions         *astiav.Dictionary
	HWDevFlags            int
	ResourceManager       ResourceManager
	Options               []Option
}

type ResourceManager = resource.ResourceManager

func (p CodecParams) Clone(ctx context.Context) CodecParams {
	if p.CodecParameters != nil {
		cp := astiav.AllocCodecParameters()
		setFinalizerFree(ctx, cp)
		p.CodecParameters.Copy(cp)
		p.CodecParameters = cp
	}
	if p.CustomOptions != nil {
		if v := p.CustomOptions.Pack(); len(v) > 0 {
			opts := astiav.NewDictionary()
			setFinalizerFree(ctx, opts)
			opts.Unpack(p.CustomOptions.Pack())
			p.CustomOptions = opts
		}
	}
	p.Options = slices.Clone(p.Options)
	return p
}
