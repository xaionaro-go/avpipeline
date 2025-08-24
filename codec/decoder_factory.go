package codec

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

type DecoderFactory interface {
	fmt.Stringer

	NewDecoder(ctx context.Context, stream *astiav.Stream) (*Decoder, error)
}

type NaiveDecoderFactory struct {
	NaiveDecoderFactoryParams
	Locker        xsync.Mutex
	VideoDecoders []*Decoder
	AudioDecoders []*Decoder
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
	return xsync.DoA2R2(ctx, &f.Locker, f.newDecoder, ctx, stream)
}

func (f *NaiveDecoderFactory) newDecoder(
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

	defer func() {
		if _err != nil {
			return
		}
		switch codecParameters.MediaType() {
		case astiav.MediaTypeAudio:
			f.AudioDecoders = append(f.AudioDecoders, _ret)
		case astiav.MediaTypeVideo:
			f.VideoDecoders = append(f.VideoDecoders, _ret)
		}
	}()

	switch codecParameters.MediaType() {
	case astiav.MediaTypeAudio:
		return NewDecoder(
			ctx,
			"",
			codecParameters,
			0,
			"",
			f.AudioOptions,
			0,
		)
	case astiav.MediaTypeVideo:
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

	return nil, fmt.Errorf("only audio and video tracks are supported by NaiveDecoderFactory, yet")
}

func (f *NaiveDecoderFactory) String() string {
	return "NaiveDecoderFactory"
}

var _ ResourcesGetter = (*NaiveDecoderFactory)(nil)

func (f *NaiveDecoderFactory) GetResources(
	ctx context.Context,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...EncoderFactoryOption,
) (_ret *Resources) {
	logger.Tracef(ctx, "GetResources: %#+v, %s, %#+v", params, timeBase, opts)
	defer func() { logger.Tracef(ctx, "/GetResources: %#+v, %s, %#+v: %#+v", params, timeBase, opts, _ret) }()
	return xsync.DoA4R1(ctx, &f.Locker, f.getResources, ctx, params, timeBase, opts)
}

func (f *NaiveDecoderFactory) getResources(
	ctx context.Context,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts []EncoderFactoryOption,
) (_ret *Resources) {
	if v, ok := EncoderFactoryOptionLatest[EncoderFactoryOptionFrameSource](opts); ok {
		frameSource := v.FrameSource
		if frameSource != nil {
			d := frameSource.GetDecoder()
			logger.Debugf(ctx, "got the decoder from frame source: %v", d)
			for _, decoder := range f.VideoDecoders {
				if decoder == d {
					return &Resources{
						HWDeviceContext: decoder.HardwareDeviceContext(),
					}
				}
			}
			return nil
		}
	}

	logger.Warnf(ctx, "guessing the decoder; this is not reliable; consider using EncoderFactoryOptionFrameSource")
	switch params.MediaType() {
	case astiav.MediaTypeVideo:
		if len(f.VideoDecoders) == 0 {
			logger.Warnf(ctx, "no video decoders are present")
			return nil
		}
		if len(f.VideoDecoders) > 1 {
			logger.Warnf(ctx, "multiple video decoders are present")
			return nil
		}
		d := f.VideoDecoders[0]
		return &Resources{
			HWDeviceContext: d.HardwareDeviceContext(),
		}
	case astiav.MediaTypeAudio:
		return nil
	}
	return nil
}
