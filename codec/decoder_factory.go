package codec

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/types"
)

type DecoderFactory interface {
	fmt.Stringer

	NewDecoder(ctx context.Context, stream *astiav.Stream) (*Decoder, error)
}

type NaiveDecoderFactory struct {
	HardwareDeviceType astiav.HardwareDeviceType
	HardwareDeviceName HardwareDeviceName
	Options            *astiav.Dictionary
}

var _ DecoderFactory = (*NaiveDecoderFactory)(nil)

func NewNaiveDecoderFactory(
	ctx context.Context,
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	customOptions types.DictionaryItems,
) *NaiveDecoderFactory {
	f := &NaiveDecoderFactory{
		HardwareDeviceType: hardwareDeviceType,
		HardwareDeviceName: hardwareDeviceName,
	}
	if len(customOptions) > 0 {
		f.Options = astiav.NewDictionary()
		setFinalizerFree(ctx, f.Options)

		for _, opt := range customOptions {
			logger.Debugf(ctx, "decoderFactory.Dictionary['%s'] = '%s'", opt.Key, opt.Value)
			f.Options.Set(opt.Key, opt.Value, 0)
		}
	}
	return f
}

func (f *NaiveDecoderFactory) NewDecoder(
	ctx context.Context,
	stream *astiav.Stream,
) (*Decoder, error) {
	codecParameters := stream.CodecParameters()
	if codecParameters.MediaType() != astiav.MediaTypeVideo {
		return NewDecoder(
			ctx,
			"",
			codecParameters,
			0,
			"",
			f.Options,
			0,
		)
	}
	return NewDecoder(
		ctx,
		"",
		codecParameters,
		f.HardwareDeviceType,
		f.HardwareDeviceName,
		f.Options,
		0,
	)
}

func (f *NaiveDecoderFactory) String() string {
	return "NaiveDecoderFactory"
}
