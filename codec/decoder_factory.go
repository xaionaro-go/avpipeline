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
	VideoOptions       *astiav.Dictionary
	AudioOptions       *astiav.Dictionary
	PostInitFunc       func(context.Context, *Decoder)
}

var _ DecoderFactory = (*NaiveDecoderFactory)(nil)

func NewNaiveDecoderFactory(
	ctx context.Context,
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	videoCustomOptions types.DictionaryItems,
	audioCustomOptions types.DictionaryItems,
	postInit func(context.Context, *Decoder),
) *NaiveDecoderFactory {
	f := &NaiveDecoderFactory{
		HardwareDeviceType: hardwareDeviceType,
		HardwareDeviceName: hardwareDeviceName,
		PostInitFunc:       postInit,
	}
	if len(videoCustomOptions) > 0 {
		f.VideoOptions = astiav.NewDictionary()
		setFinalizerFree(ctx, f.VideoOptions)

		for _, opt := range videoCustomOptions {
			logger.Debugf(ctx, "decoderFactory.VideoOptions['%s'] = '%s'", opt.Key, opt.Value)
			f.VideoOptions.Set(opt.Key, opt.Value, 0)
		}
	}
	if len(audioCustomOptions) > 0 {
		f.AudioOptions = astiav.NewDictionary()
		setFinalizerFree(ctx, f.AudioOptions)

		for _, opt := range audioCustomOptions {
			logger.Debugf(ctx, "decoderFactory.AudioOptions['%s'] = '%s'", opt.Key, opt.Value)
			f.AudioOptions.Set(opt.Key, opt.Value, 0)
		}
	}
	return f
}

func (f *NaiveDecoderFactory) NewDecoder(
	ctx context.Context,
	stream *astiav.Stream,
) (_ret *Decoder, _err error) {
	if fn := f.PostInitFunc; fn != nil {
		defer func() {
			if _err != nil {
				return
			}
			f.PostInitFunc(ctx, _ret)
		}()
	}
	codecParameters := stream.CodecParameters()
	if codecParameters.MediaType() != astiav.MediaTypeVideo {
		return NewDecoder(
			ctx,
			"",
			codecParameters,
			0,
			"",
			f.VideoOptions,
			0,
		)
	}
	return NewDecoder(
		ctx,
		"",
		codecParameters,
		f.HardwareDeviceType,
		f.HardwareDeviceName,
		f.VideoOptions,
		0,
	)
}

func (f *NaiveDecoderFactory) String() string {
	return "NaiveDecoderFactory"
}
