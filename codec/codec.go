package codec

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/unsafetools"
	"github.com/xaionaro-go/xsync"
)

const (
	doFullCopyOfParameters     = false
	manuallySetHardwareContext = false
)

type Codec struct {
	InitParams            CodecParams
	IsEncoder             bool
	codec                 *astiav.Codec
	codecContext          *astiav.CodecContext
	hardwareDeviceContext *astiav.HardwareDeviceContext
	hardwarePixelFormat   astiav.PixelFormat
	closer                *astikit.Closer
	locker                xsync.RWMutex
}

func (c *Codec) Codec() *astiav.Codec {
	return xsync.DoR1(context.TODO(), &c.locker, func() *astiav.Codec {
		return c.codec
	})
}

func (c *Codec) CodecContext() *astiav.CodecContext {
	return xsync.DoR1(context.TODO(), &c.locker, func() *astiav.CodecContext {
		return c.codecContext
	})
}

func (c *Codec) MediaType() astiav.MediaType {
	return xsync.DoR1(context.TODO(), &c.locker, func() astiav.MediaType {
		if c.codecContext == nil {
			return astiav.MediaTypeUnknown
		}
		return c.codecContext.MediaType()
	})
}

func (c *Codec) TimeBase() astiav.Rational {
	return xsync.DoR1(context.TODO(), &c.locker, func() astiav.Rational {
		if c.codecContext == nil {
			return astiav.Rational{}
		}
		return c.codecContext.TimeBase()
	})
}

func (c *Codec) HardwareDeviceContext() *astiav.HardwareDeviceContext {
	return xsync.DoR1(context.TODO(), &c.locker, func() *astiav.HardwareDeviceContext {
		return c.hardwareDeviceContext
	})
}

func (c *Codec) HardwarePixelFormat() astiav.PixelFormat {
	return xsync.DoR1(context.TODO(), &c.locker, func() astiav.PixelFormat {
		return c.hardwarePixelFormat
	})
}

func (c *Codec) Close(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &c.locker, c.closeLocked, ctx)
}

func (c *Codec) closeLocked(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "closeLocked")
	defer func() { logger.Debugf(ctx, "/closeLocked: %v", _err) }()
	if err := c.reset(ctx); err != nil {
		logger.Errorf(ctx, "unable to reset the codec: %w", err)
	}
	if c.closer == nil {
		return nil
	}
	return c.closer.Close()
}

func (c *Codec) ToCodecParameters(cp *astiav.CodecParameters) error {
	return xsync.DoR1(context.TODO(), &c.locker, func() error {
		if c.codecContext == nil {
			return fmt.Errorf("c.codecContext == nil")
		}
		return c.codecContext.ToCodecParameters(cp)
	})
}

func (c *Codec) Reset(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Reset")
	defer func() { logger.Debugf(ctx, "/Reset: %v", _err) }()
	return xsync.DoA1R1(ctx, &c.locker, c.reset, ctx)
}

func (c *Codec) reset(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "reset")
	defer func() { logger.Tracef(ctx, "/reset: %v", _err) }()
	c.codecContext.FlushBuffers()
	return nil
}

func findEncoderCodec(
	codecID astiav.CodecID,
	codecName string,
) *astiav.Codec {
	if codecName != "" {
		r := astiav.FindEncoderByName(string(codecName))
		if r != nil {
			return r
		}
	}
	return astiav.FindEncoder(codecID)
}

func findDecoderCodec(
	codecID astiav.CodecID,
	codecName string,
) *astiav.Codec {
	if codecName != "" {
		r := astiav.FindDecoderByName(string(codecName))
		if r != nil {
			return r
		}
	}
	return astiav.FindDecoder(codecID)
}

func findCodec(
	ctx context.Context,
	isEncoder bool,
	codecID astiav.CodecID,
	codecName string,
) (_ret *astiav.Codec) {
	logger.Tracef(ctx, "findCodec(ctx, %t, %s, '%s')", isEncoder, codecID, codecName)
	defer func() {
		logger.Tracef(ctx, "/findCodec(ctx, %t, %s, '%s'): %v", isEncoder, codecID, codecName, _ret)
	}()
	if isEncoder {
		return findEncoderCodec(codecID, codecName)
	}
	return findDecoderCodec(codecID, codecName)
}

func findCodecByName(
	ctx context.Context,
	isEncoder bool,
	codecName string,
) (_ret *astiav.Codec) {
	logger.Tracef(ctx, "findCodecByName(ctx, %t, '%s')", isEncoder, codecName)
	defer func() {
		logger.Tracef(ctx, "/findCodecByName(ctx, %t, '%s'): %v", isEncoder, codecName, _ret)
	}()
	if isEncoder {
		return astiav.FindEncoderByName(string(codecName))
	}
	return astiav.FindDecoderByName(string(codecName))
}

func hwCodecName(
	ctx context.Context,
	isEncoder bool,
	codecName string,
	hwDeviceType HardwareDeviceType,
) (_ret string) {
	logger.Tracef(ctx, "hwCodecName(ctx, %t, '%s', %v)", isEncoder, codecName, hwDeviceType)
	defer func() {
		logger.Tracef(ctx, "/hwCodecName(ctx, %t, '%s', %v): %v", isEncoder, codecName, hwDeviceType, _ret)
	}()
	switch hwDeviceType {
	case astiav.HardwareDeviceTypeCUDA:
		if isEncoder {
			return codecName + "_nvenc"
		} else {
			return codecName + "_cuvid"
		}
	default:
		return codecName + "_" + hwDeviceType.String()
	}
}

func newCodec(
	ctx context.Context,
	isEncoder bool, // otherwise: decoder
	params CodecParams,
) (_ret *Codec, _err error) {
	params = params.Clone(ctx)
	codecName := params.CodecName
	codecParameters := params.CodecParameters
	hardwareDeviceType := params.HardwareDeviceType
	hardwareDeviceName := params.HardwareDeviceName
	timeBase := params.TimeBase
	options := params.Options
	flags := params.Flags

	logger.Tracef(ctx, "newCodec(ctx, '%s', %#+v, %t, %s, '%s', %s, %#+v, %X)", codecName, codecParameters, isEncoder, hardwareDeviceType, hardwareDeviceName, timeBase, options, flags)
	defer func() {
		logger.Tracef(ctx, "/newCodec(ctx, '%s', %#+v, %t, %s, '%s', %s, %#+v, %X): %p %v", codecName, codecParameters, isEncoder, hardwareDeviceType, hardwareDeviceName, timeBase, options, flags, _ret, _err)
	}()
	ctx = belt.WithField(ctx, "is_encoder", isEncoder)
	ctx = belt.WithField(ctx, "codec_name", codecName)
	ctx = belt.WithField(ctx, "hw_dev_type", hardwareDeviceType)
	c := &Codec{
		InitParams: params,
		IsEncoder:  isEncoder,
		closer:     astikit.NewCloser(),
	}
	defer func() {
		if _err != nil {
			_ = c.Close(ctx)
		}
	}()

	lazyInitOptions := func() {
		if options != nil {
			return
		}
		options = astiav.NewDictionary()
		setFinalizerFree(ctx, options)
	}

	logIfError := func(err error) {
		if err == nil {
			return
		}
		logger.Errorf(ctx, "got an error: %v", err)
	}

	isHW := false
	c.codec = nil
	if codecName != "" && hardwareDeviceType != astiav.HardwareDeviceTypeNone {
		hwCodec := findCodecByName(
			ctx,
			isEncoder,
			hwCodecName(ctx, isEncoder, codecName, hardwareDeviceType),
		)
		if c.codec != nil {
			isHW = true
			c.codec = hwCodec
		}
	}
	if c.codec == nil {
		c.codec = findCodec(
			ctx,
			isEncoder,
			codecParameters.CodecID(),
			codecName,
		)
	}
	if c.codec == nil {
		if codecParameters.CodecID() == astiav.CodecIDNone {
			return nil, fmt.Errorf("unable to find a codec using name '%s'", codecName)
		}
		return nil, fmt.Errorf("unable to find a codec using name '%s' or codec ID %v", codecName, codecParameters.CodecID())
	}
	if !isHW && hardwareDeviceType != astiav.HardwareDeviceTypeNone {
		hwCodec := findCodecByName(
			ctx,
			isEncoder,
			hwCodecName(ctx, isEncoder, c.codec.Name(), hardwareDeviceType),
		)
		if hwCodec != nil {
			isHW = true
			c.codec = hwCodec
		}
	}
	codecParameters.SetCodecID(c.codec.ID())
	logger.Tracef(ctx, "codec name: '%s'", c.codec.Name())

	c.codecContext = astiav.AllocCodecContext(c.codec)
	if c.codecContext == nil {
		return nil, fmt.Errorf("unable to allocate codec context")
	}
	c.closer.Add(c.codecContext.Free)
	c.closer.Add(func() {
		logger.Tracef(ctx, "CodecContext.Free()")
	})

	if doFullCopyOfParameters {
		err := codecParameters.ToCodecContext(c.codecContext)
		if err != nil {
			return nil, fmt.Errorf("codecParameters.ToCodecContext(...) returned error: %w", err)
		}
	}

	var forcePixelFormat astiav.PixelFormat
	switch codecParameters.MediaType() {
	case astiav.MediaTypeVideo:
		lazyInitOptions()
		c.codecContext.SetPixelFormat(astiav.PixelFormatNone)
		if isEncoder {
			if options.Get("g", nil, 0) == nil {
				fps := codecParameters.FrameRate().Float64()
				if fps < 1 {
					logger.Warnf(ctx, "unable to detect the FPS, assuming 30")
					fps = 30
				}
				v := int(0.999 + fps)
				logger.Warnf(ctx, "gop_size is not set, defaulting to the FPS value (%d <- %f)", v, fps)
				logIfError(options.Set("g", fmt.Sprintf("%d", v), 0))
			}
			if options.Get("bf", nil, 0) == nil {
				logger.Debugf(ctx, "bf is not set, defaulting to zero")
				logIfError(options.Set("bf", "0", 0))
			}
			if strings.HasSuffix(c.codec.Name(), "_mediacodec") {
				if options.Get("pix_fmt", nil, 0) == nil {
					logger.Warnf(ctx, "is MediaCodec, but pixel format is not set; forcing NV12 pixel format")
					logIfError(options.Set("pix_fmt", "nv12", 0))
					forcePixelFormat = astiav.PixelFormatNv12
				}
			}
		} else {
			if strings.HasSuffix(c.codec.Name(), "_mediacodec") {
				if options.Get("pixel_format", nil, 0) == nil {
					logger.Warnf(ctx, "is MediaCodec, but pixel format is not set; forcing NV12 pixel format")
					logIfError(options.Set("pixel_format", "nv12", 0))
					forcePixelFormat = astiav.PixelFormatNv12
				}
			}
		}
	}

	if manuallySetHardwareContext && hardwareDeviceType != astiav.HardwareDeviceTypeNone {
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

		var err error
		c.hardwareDeviceContext, err = astiav.CreateHardwareDeviceContext(
			hardwareDeviceType,
			string(hardwareDeviceName),
			options,
			flags,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to create hardware (%s:%s) device context: %w", hardwareDeviceType, hardwareDeviceName, err)
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

	switch codecParameters.MediaType() {
	case astiav.MediaTypeVideo:
		if v := codecParameters.FrameRate(); v.Float64() > 0 {
			c.codecContext.SetFramerate(v)
		}
		c.codecContext.SetWidth(codecParameters.Width())
		c.codecContext.SetHeight(codecParameters.Height())
		if forcePixelFormat != astiav.PixelFormatNone {
			c.codecContext.SetPixelFormat(forcePixelFormat)
		}
		if c.codecContext.PixelFormat() == astiav.PixelFormatNone {
			c.codecContext.SetPixelFormat(codecParameters.PixelFormat())
		}
		if c.codecContext.PixelFormat() == astiav.PixelFormatNone {
			if pixFmts := c.codec.PixelFormats(); len(pixFmts) > 0 {
				c.codecContext.SetPixelFormat(pixFmts[0])
			}
		}
		c.codecContext.SetSampleAspectRatio(codecParameters.SampleAspectRatio())
		logger.Tracef(ctx,
			"pixel_format: %s; frame_rate: %s",
			c.codecContext.PixelFormat(), c.codecContext.Framerate(),
		)
	case astiav.MediaTypeAudio:
		c.codecContext.SetChannelLayout(codecParameters.ChannelLayout())
		c.codecContext.SetSampleRate(codecParameters.SampleRate())
		c.codecContext.SetSampleFormat(codecParameters.SampleFormat())
		logger.Tracef(ctx, "sample_rate: %d", c.codecContext.SampleRate())
	}

	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(ctx, "codec_parameters: %s", spew.Sdump(unsafetools.FieldByNameInValue(reflect.ValueOf(codecParameters), "c").Elem().Elem().Interface()))
	}

	logger.Debugf(ctx, "time_base == %v", timeBase)
	c.codecContext.SetTimeBase(timeBase)

	if isEncoder {
		if timeBase.Num() == 0 {
			return nil, fmt.Errorf("TimeBase must be set")
		}
	} else {
		c.codecContext.SetExtraData(codecParameters.ExtraData())
	}

	if err := c.codecContext.Open(c.codec, options); err != nil {
		return nil, fmt.Errorf("unable to open codec context: %w", err)
	}

	setFinalizer(ctx, c, func(c *Codec) { c.Close(ctx) })
	return c, nil
}
