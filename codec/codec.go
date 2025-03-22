package codec

import (
	"context"
	"fmt"
	"reflect"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/unsafetools"
	"github.com/xaionaro-go/xsync"
)

type Codec struct {
	codec                 *astiav.Codec
	codecContext          *astiav.CodecContext
	hardwareDeviceContext *astiav.HardwareDeviceContext
	hardwarePixelFormat   astiav.PixelFormat
	closer                *astikit.Closer
	locker                xsync.RWMutex
}

func (c *Codec) Codec() *astiav.Codec {
	return c.codec
}

func (c *Codec) CodecContext() *astiav.CodecContext {
	return c.codecContext
}

func (c *Codec) HardwareDeviceContext() *astiav.HardwareDeviceContext {
	return c.hardwareDeviceContext
}

func (c *Codec) HardwarePixelFormat() astiav.PixelFormat {
	return c.hardwarePixelFormat
}

func (c *Codec) Close(ctx context.Context) error {
	return c.closer.Close()
}

func (c *Codec) ToCodecParameters(cp *astiav.CodecParameters) error {
	return c.codecContext.ToCodecParameters(cp)
}

func newCodec(
	ctx context.Context,
	codecName string,
	codecParameters *astiav.CodecParameters,
	isEncoder bool, // otherwise: decoder
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	timeBase astiav.Rational,
	options *astiav.Dictionary,
	flags int,
) (_ret *Codec, _err error) {
	c := &Codec{
		closer: astikit.NewCloser(),
	}
	defer func() {
		if _err != nil {
			_ = c.Close(ctx)
		}
	}()

	if isEncoder {
		if codecName != "" {
			c.codec = astiav.FindEncoderByName(string(codecName))
			if c.codec != nil {
				codecParameters.SetCodecID(c.codec.ID())
			}
		} else {
			c.codec = astiav.FindEncoder(codecParameters.CodecID())
		}
	} else {
		if codecName != "" {
			c.codec = astiav.FindDecoderByName(string(codecName))
			if c.codec != nil {
				codecParameters.SetCodecID(c.codec.ID())
			}
		} else {
			c.codec = astiav.FindDecoder(codecParameters.CodecID())
		}
	}
	if c.codec == nil {
		if codecParameters == nil {
			return nil, fmt.Errorf("unable to find a codec using name '%s'", codecName)
		}
		return nil, fmt.Errorf("unable to find a codec using name '%s' or codec ID %v", codecName, codecParameters.CodecID())
	}

	c.codecContext = astiav.AllocCodecContext(c.codec)
	if c.codecContext == nil {
		return nil, fmt.Errorf("unable to allocate codec context")
	}
	c.closer.Add(c.codecContext.Free)

	if hardwareDeviceType != astiav.HardwareDeviceTypeNone {
		if codecParameters.MediaType() != astiav.MediaTypeVideo {
			return nil, fmt.Errorf("currently hardware encoding/decoding is supported only for video streams")
		}

		for _, p := range c.codec.HardwareConfigs() {
			if p.MethodFlags().Has(astiav.CodecHardwareConfigMethodFlagHwDeviceCtx) && p.HardwareDeviceType() == hardwareDeviceType {
				c.hardwarePixelFormat = p.PixelFormat()
				break
			}
		}

		if c.hardwarePixelFormat == astiav.PixelFormatNone {
			return nil, fmt.Errorf("hardware device type '%v' is not supported", hardwareDeviceType)
		}
	}

	err := codecParameters.ToCodecContext(c.codecContext)
	if err != nil {
		return nil, fmt.Errorf("codecParameters.ToCodecContext(...) returned error: %w", err)
	}

	switch codecParameters.MediaType() {
	case astiav.MediaTypeVideo:
		if frameRate := codecParameters.FrameRate(); frameRate.Num() != 0 {
			c.codecContext.SetFramerate(frameRate)
		}
	case astiav.MediaTypeAudio:
		if sampleRate := codecParameters.SampleRate(); sampleRate != 0 {
			c.codecContext.SetSampleRate(sampleRate)
		}
	}

	if hardwareDeviceType != astiav.HardwareDeviceTypeNone {
		c.hardwareDeviceContext, err = astiav.CreateHardwareDeviceContext(
			hardwareDeviceType,
			string(hardwareDeviceName),
			options,
			flags,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to create hardware device context: %w", err)
		}
		c.closer.Add(c.hardwareDeviceContext.Free)

		c.codecContext.SetHardwareDeviceContext(c.hardwareDeviceContext)
		c.codecContext.SetPixelFormatCallback(func(pfs []astiav.PixelFormat) astiav.PixelFormat {
			for _, pf := range pfs {
				if pf == c.hardwarePixelFormat {
					return pf
				}
			}

			logger.Errorf(ctx, "unable to find appropriate pixel format")
			return astiav.PixelFormatNone
		})
	}

	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(ctx, "codec_parameters: %s", spew.Sdump(unsafetools.FieldByNameInValue(reflect.ValueOf(codecParameters), "c").Elem().Elem().Interface()))
	}

	logger.Debugf(ctx, "time_base == %v", timeBase)
	c.codecContext.SetTimeBase(timeBase)

	if err := c.codecContext.Open(c.codec, options); err != nil {
		return nil, fmt.Errorf("unable to open codec context: %w", err)
	}

	return c, nil
}
