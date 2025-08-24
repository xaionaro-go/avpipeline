package codec

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
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
	doFullCopyOfParameters   = false
	setRoteControlParameters = false
	setSetPktTimeBase        = false
)

type codecInternals struct {
	InitParams            CodecParams
	IsEncoder             bool
	codec                 *astiav.Codec
	codecContext          *astiav.CodecContext
	hardwareDeviceContext *astiav.HardwareDeviceContext
	hardwarePixelFormat   astiav.PixelFormat
	closer                *astikit.Closer
}

type Codec struct {
	*codecInternals
	locker xsync.RWMutex
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
			logger.Errorf(context.TODO(), "codecContext == nil")
			return astiav.MediaTypeUnknown
		}
		return c.codecContext.MediaType()
	})
}

func (c *Codec) TimeBase() astiav.Rational {
	return xsync.DoR1(context.TODO(), &c.locker, func() astiav.Rational {
		if c.codecContext == nil {
			logger.Errorf(context.TODO(), "codecContext == nil")
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

func (c *codecInternals) closeLocked(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "closeLocked")
	defer func() { logger.Debugf(ctx, "/closeLocked: %v", _err) }()
	logger.Tracef(ctx, "closing the codec, due to: %s", debug.Stack())
	defer func() {
		c.codec = nil
		c.codecContext = nil
	}()
	if c.closer == nil {
		return nil
	}
	if err := c.reset(ctx); err != nil {
		logger.Errorf(ctx, "unable to reset the codec: %v", err)
	}
	err := c.closer.Close()
	c.closer = nil
	return err
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

func (c *codecInternals) reset(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "reset")
	defer func() { logger.Tracef(ctx, "/reset: %v", _err) }()
	if c.codecContext == nil {
		return fmt.Errorf("codec is closed")
	}
	c.codecContext.FlushBuffers()
	return nil
}

func findEncoderCodec(
	codecID astiav.CodecID,
	codecName Name,
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
	codecName Name,
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
	codecName Name,
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

type Resources struct {
	HWDeviceContext *astiav.HardwareDeviceContext
}

func newCodec(
	ctx context.Context,
	isEncoder bool, // otherwise: decoder
	params CodecParams,
	reusableResources *Resources,
) (_ret *Codec, _err error) {
	params = params.Clone(ctx)
	codecName := params.CodecName
	codecParameters := params.CodecParameters
	hardwareDeviceType := params.HardwareDeviceType
	hardwareDeviceName := params.HardwareDeviceName
	timeBase := params.TimeBase
	options := params.Options
	flags := params.Flags

	logger.Tracef(ctx, "newCodec(ctx, '%s', %s, %#+v, %t, %s, '%s', %s, %#+v, %X)", codecName, codecParameters.CodecID(), codecParameters, isEncoder, hardwareDeviceType, hardwareDeviceName, timeBase, options, flags)
	defer func() {
		logger.Tracef(ctx, "/newCodec(ctx, '%s', %s, %#+v, %t, %s, '%s', %s, %#+v, %X): %p %v", codecName, codecParameters.CodecID(), codecParameters, isEncoder, hardwareDeviceType, hardwareDeviceName, timeBase, options, flags, _ret, _err)
	}()
	ctx = belt.WithField(ctx, "is_encoder", isEncoder)
	ctx = belt.WithField(ctx, "codec_id", codecParameters.CodecID())
	ctx = belt.WithField(ctx, "codec_name", codecName)
	ctx = belt.WithField(ctx, "hw_dev_type", hardwareDeviceType)
	c := &Codec{
		codecInternals: &codecInternals{
			InitParams: params,
			IsEncoder:  isEncoder,
			closer:     astikit.NewCloser(),
		},
	}
	defer func() {
		if _err != nil {
			logger.Debugf(ctx, "got an error, closing the codec: %v", _err)
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
		hwCodec := codecName.hwName(ctx, isEncoder, hardwareDeviceType).Codec(ctx, isEncoder)
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
		hwCodec := Name(c.codec.Name()).hwName(ctx, isEncoder, hardwareDeviceType).Codec(ctx, isEncoder)
		if hwCodec != nil {
			isHW = true
			c.codec = hwCodec
		}
	}

	ctx = belt.WithField(ctx, "codec_id", c.codec.ID())
	codecParameters.SetCodecID(c.codec.ID())
	logger.Tracef(ctx, "codec name: '%s' (%s)", c.codec.Name(), c.codec.ID())

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
			options.Set("gpu", "0", 0)
			if options.Get("g", nil, 0) == nil {
				fps := codecParameters.FrameRate().Float64()
				if fps < 1 {
					logger.Warnf(ctx, "unable to detect the FPS, assuming 30")
					fps = 30
				}
				v := int(0.999+fps) * 2
				logger.Warnf(ctx, "gop_size is not set, defaulting to the FPS*2 value (%d <- %f)", v, fps)
				logIfError(options.Set("g", fmt.Sprintf("%d", v), 0))
			}
			if options.Get("bf", nil, 0) == nil {
				logger.Debugf(ctx, "bf is not set, defaulting to zero")
				logIfError(options.Set("bf", "0", 0))
			}
			if codecParameters.BitRate() > 0 {
				options.Set("b", fmt.Sprintf("%d", codecParameters.BitRate()), 0) // TODO: figure out: do we need this?
				options.Set("rc", "cbr", 0)
				options.Set("bitrate_mode", "cbr", 0) // TODO: do we need to deduplicate this with the line above?
			}
			if strings.HasSuffix(c.codec.Name(), "_mediacodec") {
				if options.Get("pix_fmt", nil, 0) == nil {
					logger.Warnf(ctx, "is MediaCodec, but pixel format is not set; forcing MediaCodec pixel format")
					logIfError(options.Set("pix_fmt", "mediacodec", 0))
					forcePixelFormat = astiav.PixelFormatMediacodec
				}
			}
		} else {
			if strings.HasSuffix(c.codec.Name(), "_mediacodec") {
				if options.Get("pixel_format", nil, 0) == nil {
					logger.Warnf(ctx, "is MediaCodec, but pixel format is not set; forcing MediaCodec pixel format")
					logIfError(options.Set("pixel_format", "mediacodec", 0))
					forcePixelFormat = astiav.PixelFormatMediacodec
				}

				height := codecParameters.Height()
				alignedHeight := (height + 15) &^ 15
				logger.Tracef(ctx, "MediaCodec aligned height: %d (current: %d)", alignedHeight, height)
				if alignedHeight != height && options.Get("create_window", nil, 0) == nil {
					logger.Warnf(ctx, "in MediaCodec H264/HEVC heights are aligned with 16, while AV1 is not, so there could be is a green strip at the bottom during recoding H264->AV1 (due to %dp != %dp); to handle this we are forcing create_window=1", codecParameters.Height(), (codecParameters.Height()+15)&^15)
					logIfError(options.Set("create_window", "1", 0))
				}
			}
		}
	}

	if hardwareDeviceType != astiav.HardwareDeviceTypeNone {
		if codecParameters.MediaType() != astiav.MediaTypeVideo {
			return nil, fmt.Errorf("currently hardware encoding/decoding is supported only for video streams")
		}
		err := c.initHardware(
			ctx,
			hardwareDeviceType,
			hardwareDeviceName,
			options,
			flags,
			reusableResources,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to init hardware device context: %w", err)
		}
	}

	switch codecParameters.MediaType() {
	case astiav.MediaTypeVideo:
		if bitRate := codecParameters.BitRate(); bitRate > 0 {
			logger.Tracef(ctx, "bitrate: %d", bitRate)
			c.codecContext.SetBitRate(bitRate)
			if setRoteControlParameters {
				c.codecContext.SetRateControlMinRate(bitRate)
				c.codecContext.SetRateControlMaxRate(bitRate)
				c.codecContext.SetRateControlBufferSize(int(bitRate * 2))
			}
			c.codecContext.SetFlags(c.codecContext.Flags() & ^astiav.CodecContextFlags(astiav.CodecContextFlagQscale))
		}
		if v := codecParameters.FrameRate(); v.Float64() > 0 {
			logger.Tracef(ctx, "setting frame rate to %s", v)
			c.codecContext.SetFramerate(v)
		}
		logger.Tracef(ctx, "resolution: %dx%d", codecParameters.Width(), codecParameters.Height())
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
	if setSetPktTimeBase {
		c.codecContext.SetPktTimeBase(timeBase)
	}

	if isEncoder {
		if timeBase.Num() == 0 {
			return nil, fmt.Errorf("TimeBase must be set")
		}
	} else {
		c.codecContext.SetExtraData(codecParameters.ExtraData())
	}

	c.logHints(ctx)
	logger.Tracef(ctx, "c.codecContext.Open(%#+v, %#+v)", c.codec, options)
	if err := c.codecContext.Open(c.codec, options); err != nil {
		return nil, fmt.Errorf("unable to open codec context: %w", err)
	}

	setFinalizer(ctx, c.codecInternals, func(c *codecInternals) { c.closeLocked(ctx) })
	return c, nil
}

func (c *Codec) logHints(ctx context.Context) {
	if strings.HasSuffix(c.codec.Name(), "_mediacodec") {
		height := c.codecContext.Height()
		suggestedHeight := (height + 15) &^ 15
		if suggestedHeight != height {
			logger.Debugf(ctx, "in MediaCodec H264/HEVC heights are aligned with 16, while AV1 is not, so there could be is a green strip at the bottom during recoding H264->AV1 (due to %dp != %dp); to handle this copy-crop frames from H264/HEVC using swscale to be %dp", height, suggestedHeight, height)
		}
	}
}

func (c *Codec) initHardware(
	ctx context.Context,
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	options *astiav.Dictionary,
	flags int,
	reusableResources *Resources,
) (_err error) {
	logger.Tracef(ctx, "initHardware(%s, '%s', %#+v, %X)", hardwareDeviceType, hardwareDeviceName, options, flags)
	defer func() {
		logger.Tracef(ctx, "/initHardware(%s, '%s', %#+v, %X): %v", hardwareDeviceType, hardwareDeviceName, options, flags, _err)
	}()
	err := c.initHardwareDeviceContext(
		ctx,
		hardwareDeviceType,
		hardwareDeviceName,
		options,
		flags,
		reusableResources,
	)
	if err != nil {
		return fmt.Errorf("unable to get or create hardware device context: %w", err)
	}

	err = c.initHardwarePixelFormat(ctx, hardwareDeviceType)
	if err != nil {
		return fmt.Errorf("unable to init hardware pixel format: %w", err)
	}
	c.platformSpecificHWSanityChecks(ctx)
	return nil
}

func (c *Codec) initHardwarePixelFormat(
	ctx context.Context,
	hardwareDeviceType astiav.HardwareDeviceType,
) (_err error) {
	logger.Tracef(ctx, "initHardwarePixelFormat")
	defer func() { logger.Tracef(ctx, "/initHardwarePixelFormat: %v", _err) }()
	for _, p := range c.codec.HardwareConfigs() {
		if p.MethodFlags().Has(astiav.CodecHardwareConfigMethodFlagHwDeviceCtx) && p.HardwareDeviceType() == hardwareDeviceType {
			c.hardwarePixelFormat = p.PixelFormat()
			break
		}
	}

	if c.hardwarePixelFormat == astiav.PixelFormatNone {
		return fmt.Errorf("hardware device type '%v' is not supported", hardwareDeviceType)
	}
	c.codecContext.SetPixelFormatCallback(func(pfs []astiav.PixelFormat) astiav.PixelFormat {
		for _, pf := range pfs {
			if pf == c.hardwarePixelFormat {
				return pf
			}
		}

		logger.Errorf(ctx, "unable to find appropriate pixel format")
		return astiav.PixelFormatNone
	})

	return nil
}

func (c *Codec) initHardwareDeviceContext(
	ctx context.Context,
	hardwareDeviceType astiav.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	options *astiav.Dictionary,
	flags int,
	reusableResources *Resources,
) error {
	if reusableResources != nil {
		// TODO: add a check if we can reuse the hardware device context; for example, we
		//       might've been asked to use another device at all
		if oldHWCtx := reusableResources.HWDeviceContext; oldHWCtx != nil {
			logger.Debugf(ctx, "reusing the old hardware device context: %p", oldHWCtx)
			c.hardwareDeviceContext = oldHWCtx
			c.closer.Add(func() {
				logger.Tracef(ctx, "not closing the reused hardware device context: %p", oldHWCtx)
			})
			c.codecContext.SetHardwareDeviceContext(c.hardwareDeviceContext)
			return nil
		}
	}

	var err error
	c.hardwareDeviceContext, err = astiav.CreateHardwareDeviceContext(
		hardwareDeviceType,
		string(hardwareDeviceName),
		options,
		flags,
	)
	if err != nil {
		return fmt.Errorf("unable to create hardware (%s:%s) device context: %w", hardwareDeviceType, hardwareDeviceName, err)
	}
	c.closer.Add(c.hardwareDeviceContext.Free)
	c.codecContext.SetHardwareDeviceContext(c.hardwareDeviceContext)
	logger.Tracef(ctx, "HardwareDeviceContext: %p", c.hardwareDeviceContext)
	return nil
}
