package codec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/codec/mediacodec"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/unsafetools"
	"github.com/xaionaro-go/xsync"
)

const (
	doFullCopyOfParameters   = false
	setRoteControlParameters = false
	setSetPktTimeBase        = false
)

type hardwareContextType int

const (
	undefinedHardwareContextType hardwareContextType = iota
	hardwareContextTypeDevice
	hardwareContextTypeFrames
	endOfHardwareContextType
)

type codecInternals struct {
	InitParams            CodecParams
	IsEncoder             bool
	codec                 *astiav.Codec
	codecContext          *astiav.CodecContext
	hardwareDeviceContext *astiav.HardwareDeviceContext
	hardwarePixelFormat   astiav.PixelFormat
	hardwareContextType   hardwareContextType
	closer                *astikit.Closer
	_                     Quirks // unused, yet
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
		c.closer = nil
	}()
	if c.closer == nil {
		return nil
	}
	logger.Debugf(ctx, "resetting")
	if err := c.reset(ctx); err != nil {
		logger.Errorf(ctx, "unable to reset the codec: %v", err)
		if err == io.EOF {
			return err
		}
	}
	logger.Debugf(ctx, "closing the codec internals")
	belt.Flush(ctx) // we want to flush the logs before a SEGFAULT-risky operation:
	err := c.closer.Close()
	return err
}

func (c *Codec) ToCodecParameters(cp *astiav.CodecParameters) error {
	return xsync.DoA1R1(context.TODO(), &c.locker, c.toCodecParametersLocked, cp)
}

func (c *Codec) toCodecParametersLocked(cp *astiav.CodecParameters) (_err error) {
	if c.codecContext == nil {
		return fmt.Errorf("c.codecContext == nil")
	}
	return c.codecContext.ToCodecParameters(cp)
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
	if !c.IsEncoder {
		c.codecContext.FlushBuffers()
		return
	}

	c.codecContext.SendFrame(nil)
	for {
		pkt := packet.Pool.Get()
		err := c.codecContext.ReceivePacket(pkt)
		packet.Pool.Put(pkt)
		switch err {
		case nil:
			logger.Warnf(ctx, "codec contained a packet")
			continue
		case astiav.ErrEof:
			logger.Debugf(ctx, "ReceivePacket draining loop successfully finished")
		case astiav.ErrEinval:
			return io.EOF
		default:
			return fmt.Errorf("unable to receive packet: %w", err)
		}
		break
	}
	caps := c.codec.Capabilities()
	logger.Tracef(ctx, "Capabilities: %08x", caps)
	if caps&astiav.CodecCapabilityEncoderFlush != 0 {
		logger.Tracef(ctx, "flushing buffers")
		c.codecContext.FlushBuffers()
	}

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

type Input struct {
	IsEncoder         bool // otherwise: decoder
	Params            CodecParams
	ReusableResources *Resources
}

func newCodec(
	ctx context.Context,
	input Input,
) (_ret *Codec, _err error) {
	isEncoder := input.IsEncoder
	params := input.Params.Clone(ctx)
	reusableResources := input.ReusableResources
	codecName := params.CodecName
	codecParameters := params.CodecParameters
	hardwareDeviceType := params.HardwareDeviceType
	hardwareDeviceName := params.HardwareDeviceName
	timeBase := params.TimeBase
	options := params.Options
	hwDevFlags := params.HWDevFlags
	ctx = belt.WithField(ctx, "is_encoder", isEncoder)
	if codecParameters.CodecID() != astiav.CodecIDNone {
		ctx = belt.WithField(ctx, "codec_id", codecParameters.CodecID())
	}
	ctx = belt.WithField(ctx, "codec_name", codecName)
	ctx = belt.WithField(ctx, "hw_dev_type", hardwareDeviceType)

	logger.Tracef(ctx, "newCodec(ctx, '%s', %s, %#+v, %t, %s, '%s', %s, %#+v, %X)", codecName, codecParameters.CodecID(), codecParameters, isEncoder, hardwareDeviceType, hardwareDeviceName, timeBase, options, hwDevFlags)
	defer func() {
		logger.Tracef(ctx, "/newCodec(ctx, '%s', %s, %#+v, %t, %s, '%s', %s, %#+v, %X): %p %v", codecName, codecParameters.CodecID(), codecParameters, isEncoder, hardwareDeviceType, hardwareDeviceName, timeBase, options, hwDevFlags, _ret, _err)
	}()
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

	if !isEncoder && hardwareDeviceType == globaltypes.HardwareDeviceTypeCUDA {
		logger.Warnf(ctx, "hardware decoding using CUDA is not supported, yet")
		hardwareDeviceType = globaltypes.HardwareDeviceTypeNone
	}

	isHW := false
	c.codec = nil
	if codecName != "" && hardwareDeviceType != globaltypes.HardwareDeviceTypeNone {
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
	if !isHW && hardwareDeviceType != globaltypes.HardwareDeviceTypeNone {
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

	var gopSize int64
	var bFrames int64
	var forcePixelFormat astiav.PixelFormat
	switch codecParameters.MediaType() {
	case astiav.MediaTypeVideo:
		lazyInitOptions()
		c.codecContext.SetPixelFormat(astiav.PixelFormatNone)

		defaultMediaCodecPixelFormat := astiav.PixelFormatNv12
		if strings.HasSuffix(c.codec.Name(), "_mediacodec") {
			if reusableResources != nil && reusableResources.HWDeviceContext != nil {
				defaultMediaCodecPixelFormat = astiav.PixelFormatMediacodec
			}
		}

		if isEncoder {
			options.Set("gpu", "0", 0)
			if v := options.Get("g", nil, 0); v == nil {
				fps := codecParameters.FrameRate().Float64()
				if fps < 1 {
					logger.Warnf(ctx, "unable to detect the FPS, assuming 30")
					fps = 30
				}
				gopSize = int64(0.999+fps) * 2
				logger.Warnf(ctx, "gop_size is not set, defaulting to the FPS*2 value (%d <- %f)", gopSize, fps)
				logIfError(options.Set("g", fmt.Sprintf("%d", gopSize), 0))
			} else {
				var err error
				gopSize, err = strconv.ParseInt(v.Value(), 10, 64)
				logIfError(err)
			}
			if v := options.Get("bf", nil, 0); v == nil {
				logger.Debugf(ctx, "bf is not set, defaulting to zero")
				logIfError(options.Set("bf", "0", 0))
				bFrames = 0
			} else {
				var err error
				bFrames, err = strconv.ParseInt(v.Value(), 10, 64)
				logIfError(err)
			}
			if v := options.Get("forced-idr", nil, 0); v == nil {
				logger.Debugf(ctx, "forced-idr is not set, defaulting to 1")
				logIfError(options.Set("forced-idr", "1", 0))
			}
			if codecParameters.BitRate() > 0 {
				options.Set("b", fmt.Sprintf("%d", codecParameters.BitRate()), 0) // TODO: figure out: do we need this?
				options.Set("rc", "cbr", 0)
				options.Set("bitrate_mode", "cbr", 0) // TODO: do we need to deduplicate this with the line above?
			}
			if strings.HasSuffix(c.codec.Name(), "_mediacodec") {
				if v := options.Get("priority", nil, 0); v != nil {
					priority, err := strconv.ParseInt(v.Value(), 10, 64)
					if err != nil {
						logger.Warnf(ctx, "unable to parse priority option value '%s' as int: %v", v.Value(), err)
					} else {
						logIfError(c.ffAMediaFormatSetInt32(ctx, mediacodec.KEY_PRIORITY, int32(priority)))
					}
				}
				if v := options.Get("forced-idr", nil, 0); v != nil && v.Value() != "0" {
					logIfError(c.ffAMediaFormatSetInt32(ctx, mediacodec.KEY_PREPEND_HEADER_TO_SYNC_FRAMES, 1))
					logIfError(c.ffAMediaFormatSetInt32(ctx, mediacodec.KEY_INTRA_REFRESH_PERIOD, 0))
				}
				if options.Get("pix_fmt", nil, 0) == nil {
					logger.Warnf(ctx, "is MediaCodec, but pixel format is not set; forcing %s pixel format", defaultMediaCodecPixelFormat)
					logIfError(options.Set("pix_fmt", defaultMediaCodecPixelFormat.String(), 0))
					forcePixelFormat = defaultMediaCodecPixelFormat
				}
			}
		} else {
			if strings.HasSuffix(c.codec.Name(), "_mediacodec") {
				if options.Get("pixel_format", nil, 0) == nil {
					logger.Warnf(ctx, "is MediaCodec, but pixel format is not set; forcing %s pixel format", defaultMediaCodecPixelFormat)
					logIfError(options.Set("pixel_format", defaultMediaCodecPixelFormat.String(), 0))
					forcePixelFormat = defaultMediaCodecPixelFormat
				}

				height := codecParameters.Height()
				alignedHeight := (height + 15) &^ 15
				logger.Tracef(ctx, "MediaCodec aligned height: %d (current: %d)", alignedHeight, height)
				if alignedHeight != height && options.Get("create_window", nil, 0) == nil {
					logger.Warnf(ctx, "in MediaCodec H264/HEVC heights are aligned with 16, while AV1 is not, so there could be is a green strip at the bottom during recoding H264->AV1 (due to %dp != %dp); to handle you may want to use create_window=1 (and please use pixel_format 'mediacodec')", codecParameters.Height(), (codecParameters.Height()+15)&^15)
				}
			}
		}
	}

	if hardwareDeviceType != globaltypes.HardwareDeviceTypeNone {
		if codecParameters.MediaType() != astiav.MediaTypeVideo {
			return nil, fmt.Errorf("currently hardware encoding/decoding is supported only for video streams")
		}
		err := c.initHardware(
			ctx,
			hardwareDeviceType,
			hardwareDeviceName,
			options,
			hwDevFlags,
			reusableResources,
		)
		switch {
		case err == nil:
		case errors.As(err, &ErrNotImplemented{}):
			logger.Warnf(ctx, "hardware initialization of this type is not implemented, yet: %v", err)
		default:
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
		if forcePixelFormat != 0 {
			logger.Tracef(ctx, "forcing pixel format to %s", forcePixelFormat)
			c.codecContext.SetPixelFormat(forcePixelFormat)
		}
		if c.codecContext.PixelFormat() == astiav.PixelFormatNone {
			logger.Tracef(ctx, "pixel format is not set")
			switch hardwareDeviceType {
			case globaltypes.HardwareDeviceTypeCUDA:
				logger.Debugf(ctx, "is CUDA, so applying NV12 pixel format")
				c.codecContext.SetPixelFormat(astiav.PixelFormatNv12)
			default:
				c.codecContext.SetPixelFormat(codecParameters.PixelFormat())
			}
		}
		if c.codecContext.PixelFormat() == astiav.PixelFormatNone {
			logger.Warnf(ctx, "pixel format is not set, so applying the first supported one")
			if pixFmts := c.codec.PixelFormats(); len(pixFmts) > 0 {
				c.codecContext.SetPixelFormat(pixFmts[0])
			}
		}
		c.codecContext.SetMaxBFrames(int(bFrames))
		c.codecContext.SetGopSize(int(gopSize))
		c.codecContext.SetSampleAspectRatio(codecParameters.SampleAspectRatio())
		logger.Tracef(ctx,
			"pixel_format: %s; frame_rate: %s; device_type: %s; hw_pixel_format: %s",
			c.codecContext.PixelFormat(), c.codecContext.Framerate(),
			hardwareDeviceType, c.hardwarePixelFormat,
		)
	case astiav.MediaTypeAudio:
		c.codecContext.SetChannelLayout(codecParameters.ChannelLayout())
		if options != nil {
			if v := options.Get("ac", nil, 0); v != nil {
				logger.Debugf(ctx, "ac option is set to '%s'", v.Value())
				channels, err := strconv.ParseInt(v.Value(), 10, 64)
				if err != nil {
					return nil, fmt.Errorf("unable to parse ac option value '%s' as int: %w", v.Value(), err)
				}
				switch channels {
				case 1:
					c.codecContext.SetChannelLayout(astiav.ChannelLayoutMono)
				case 2:
					c.codecContext.SetChannelLayout(astiav.ChannelLayoutStereo)
				default:
					return nil, fmt.Errorf("unsupported ac option value '%s'", v.Value())
				}
			}
		}
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
	flags := 0 |
		astiav.CodecContextFlags(astiav.CodecContextFlagLowDelay) | // this is a streaming focused library
		astiav.CodecContextFlags(astiav.CodecContextFlagClosedGop) // to make sure we can route dynamically without issues
	if c.codec.Capabilities()&astiav.CodecCapabilityDelay != 0 {
		if isEncoder && c.codec.Capabilities()&astiav.CodecCapabilityEncoderReorderedOpaque == 0 {
			logger.Warnf(ctx, "codec '%s' has 'delay' capability, but doesn't have 'encoder_reordered_opaque' capability, so it is not supported by avpipeline", c.codec.Name())
		} else {
			// avpipeline uses the opaque field to store packet info when dealing with delayed frames:
			flags |= astiav.CodecContextFlags(astiav.CodecContextFlagCopyOpaque)
		}
	}
	flags2 := 0 |
		astiav.CodecContextFlags2(astiav.CodecFlag2LocalHeader) | // to make sure we can route dynamically without issues
		astiav.CodecContextFlags2(astiav.CodecFlag2Chunks) // to make sure we can route dynamically without issues
	c.codecContext.SetFlags(flags)
	c.codecContext.SetFlags2(flags2)

	if isEncoder {
		if timeBase.Num() == 0 {
			return nil, fmt.Errorf("TimeBase must be set")
		}
	} else {
		c.codecContext.SetExtraData(codecParameters.ExtraData())
	}

	c.setQuirks(ctx)
	c.logHints(ctx)
	logger.Tracef(ctx, "c.codecContext.Open(%#+v, %#+v)", c.codec, options)
	if err := c.codecContext.Open(c.codec, options); err != nil {
		return nil, fmt.Errorf("unable to open codec context: %w", err)
	}

	setFinalizer(ctx, c.codecInternals, func(c *codecInternals) { c.closeLocked(ctx) })
	return c, nil
}

func (c *Codec) setQuirks(ctx context.Context) {
}

func (c *Codec) logHints(ctx context.Context) {
	if strings.HasSuffix(c.codec.Name(), "_mediacodec") {
		height := c.codecContext.Height()
		suggestedHeight := (height + 15) &^ 15
		if suggestedHeight != height {
			logger.Debugf(ctx, "in MediaCodec H264/HEVC heights are aligned with 16, while AV1 is not, so there could be is a green strip at the bottom during recoding H264->AV1 (due to %dp != %dp)", height, suggestedHeight)
		}
	}
}

type ErrNotImplemented struct {
	Err error
}

func (e ErrNotImplemented) Error() string {
	return fmt.Sprintf("not implemented: %v", e.Err)
}

func (c *Codec) initHardware(
	ctx context.Context,
	hardwareDeviceType globaltypes.HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	options *astiav.Dictionary,
	hwDevFlags int,
	reusableResources *Resources,
) (_err error) {
	logger.Tracef(ctx, "initHardware(%s, '%s', %#+v, %X)", hardwareDeviceType, hardwareDeviceName, options, hwDevFlags)
	defer func() {
		logger.Tracef(ctx, "/initHardware(%s, '%s', %#+v, %X): %v", hardwareDeviceType, hardwareDeviceName, options, hwDevFlags, _err)
	}()
	err := c.initHardwarePixelFormat(ctx, hardwareDeviceType)
	if err != nil {
		return fmt.Errorf("unable to init hardware pixel format: %w", err)
	}

	err = c.initHardwareDeviceContext(
		ctx,
		hardwareDeviceType,
		hardwareDeviceName,
		options,
		hwDevFlags,
		reusableResources,
	)
	if err != nil {
		return fmt.Errorf("unable to get or create hardware device context: %w", err)
	}

	c.platformSpecificHWSanityChecks(ctx)
	return nil
}

func (c *Codec) initHardwarePixelFormat(
	ctx context.Context,
	hardwareDeviceType HardwareDeviceType,
) (_err error) {
	logger.Tracef(ctx, "initHardwarePixelFormat")
	defer func() { logger.Tracef(ctx, "/initHardwarePixelFormat: %v %v", c.hardwarePixelFormat, _err) }()

	for _, hwCfgs := range c.codec.HardwareConfigs() {
		logger.Tracef(ctx, "hw config: %v %v %v", hwCfgs.PixelFormat(), hwCfgs.MethodFlags(), hwCfgs.HardwareDeviceType())
		if hwCfgs.HardwareDeviceType() != astiav.HardwareDeviceType(hardwareDeviceType) {
			logger.Tracef(ctx, "skipping this config, since it is for another hardware device type")
			continue
		}
		switch {
		case hwCfgs.MethodFlags().Has(astiav.CodecHardwareConfigMethodFlagHwFramesCtx):
			c.hardwareContextType = hardwareContextTypeFrames
			continue // TODO: implement this
		case hwCfgs.MethodFlags().Has(astiav.CodecHardwareConfigMethodFlagHwDeviceCtx):
			c.hardwareContextType = hardwareContextTypeDevice
		default:
			logger.Tracef(ctx, "skipping this config, since it doesn't support neither HW frames nor HW device context")
			continue
		}
		c.hardwarePixelFormat = hwCfgs.PixelFormat()
		break
	}

	if c.hardwareContextType == undefinedHardwareContextType {
		return fmt.Errorf("hardware device type '%v' is not supported", hardwareDeviceType)
	}

	if c.hardwarePixelFormat == astiav.PixelFormatNone {
		return nil
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
	hardwareDeviceType HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	options *astiav.Dictionary,
	hwDevFlags int,
	reusableResources *Resources,
) (_err error) {
	logger.Tracef(ctx, "initHardwareDeviceContext(%s, '%s', %#+v, %X)", hardwareDeviceType, hardwareDeviceName, options, hwDevFlags)
	defer func() {
		logger.Tracef(ctx, "/initHardwareDeviceContext(%s, '%s', %#+v, %X): %v", hardwareDeviceType, hardwareDeviceName, options, hwDevFlags, _err)
	}()
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
		astiav.HardwareDeviceType(hardwareDeviceType),
		string(hardwareDeviceName),
		options,
		hwDevFlags,
	)
	if err != nil {
		return fmt.Errorf("unable to create hardware (%s:%s) device context: %w", hardwareDeviceType, hardwareDeviceName, err)
	}
	c.closer.Add(c.hardwareDeviceContext.Free)
	c.codecContext.SetHardwareDeviceContext(c.hardwareDeviceContext)
	logger.Tracef(ctx, "HardwareDeviceContext: %p", c.hardwareDeviceContext)
	return nil
}
