package codec

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type DecoderFactory interface {
	fmt.Stringer

	NewDecoder(ctx context.Context, stream *astiav.Stream) (*Decoder, error)
}

type NaiveDecoderFactory struct {
	NaiveDecoderFactoryParams
}

var _ DecoderFactory = (*NaiveDecoderFactory)(nil)

type NaiveDecoderFactoryParams struct {
	HardwareDeviceType astiav.HardwareDeviceType
	HardwareDeviceName HardwareDeviceName
	VideoOptions       *astiav.Dictionary
	AudioOptions       *astiav.Dictionary
	PostInitFunc       func(context.Context, *Decoder)
}

func DefaultNaiveDecoderFactory() *NaiveDecoderFactoryParams {
	return &NaiveDecoderFactoryParams{}
}

func NewNaiveDecoderFactory(
	ctx context.Context,
	params *NaiveDecoderFactoryParams,
) *NaiveDecoderFactory {
	if params == nil {
		params = DefaultNaiveDecoderFactory()
	}
	return &NaiveDecoderFactory{
		NaiveDecoderFactoryParams: *params,
	}
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
