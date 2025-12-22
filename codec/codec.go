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
	"github.com/xaionaro-go/avpipeline/codec/resource"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/unsafetools"
	"github.com/xaionaro-go/xsync"
)

const (
	doFullCopyOfParameters   = false
	setRateControlParameters = false
	setSetPktTimeBase        = false
	setEncoderExtraData      = false // <- this is wrong, don't use it unless you are temporary debugging something
	setPipelinishFlags       = true
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
	codec                 *astiav.Codec
	codecContext          *astiav.CodecContext
	hardwareDeviceContext *astiav.HardwareDeviceContext
	hardwarePixelFormat   astiav.PixelFormat
	hardwareContextType   hardwareContextType
	closer                *astikit.Closer
	quirks                Quirks
	isDirty               bool
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
	return xsync.DoR1(context.TODO(), &c.locker, c.mediaTypeLocked)
}

func (c *Codec) mediaTypeLocked() astiav.MediaType {
	if c.codecContext == nil {
		logger.Errorf(context.TODO(), "codecContext == nil")
		return astiav.MediaTypeUnknown
	}
	return c.codecContext.MediaType()
}

func (c *Codec) TimeBase() astiav.Rational {
	return xsync.DoR1(context.TODO(), &c.locker, c.timeBaseLocked)
}

func (c *Codec) timeBaseLocked() astiav.Rational {
	if c.codecContext == nil {
		logger.Errorf(context.TODO(), "codecContext == nil")
		return astiav.Rational{}
	}
	return c.codecContext.TimeBase()
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
	if c.isDirty {
		logger.Debugf(ctx, "resetting")
		if err := c.reset(ctx); err != nil {
			logger.Errorf(ctx, "unable to reset the codec: %v", err)
			if err == io.EOF {
				return err
			}
		}
	}
	logger.Debugf(ctx, "closing the codec internals")
	belt.Flush(ctx) // we want to flush the logs before a SEGFAULT/SIGTRAP-risky operation:
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

func (c *codecInternals) IsOpen() bool {
	if c.codecContext == nil {
		return false
	}
	return c.codecContext.IsOpen()
}

func (c *codecInternals) IsDecoder() bool {
	if c.codec == nil {
		return false
	}
	return c.codec.IsDecoder()
}

func (c *codecInternals) IsEncoder() bool {
	if c.codec == nil {
		return false
	}
	return c.codec.IsEncoder()
}

func (c *codecInternals) isMediaCodec() bool {
	if c.codec == nil {
		return false
	}
	return strings.HasSuffix(c.codec.Name(), "_mediacodec")
}

func (c *codecInternals) reset(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "reset")
	defer func() { logger.Tracef(ctx, "/reset: %v", _err) }()
	if c.codecContext == nil {
		return fmt.Errorf("codec is closed")
	}
	if c.codec == nil {
		return fmt.Errorf("internal error: c.codec == nil")
	}
	if !c.codecContext.IsOpen() {
		return fmt.Errorf("codec context is not opened")
	}
	if !c.IsEncoder() {
		logger.Debugf(ctx, "is decoder, flushing buffers")
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
	logger.Debugf(ctx, "findCodec(ctx, %t, %s, '%s')", isEncoder, codecID, codecName)
	defer func() {
		logger.Debugf(ctx, "/findCodec(ctx, %t, %s, '%s'): %v", isEncoder, codecID, codecName, _ret)
	}()
	if isEncoder {
		return findEncoderCodec(codecID, codecName)
	}
	return findDecoderCodec(codecID, codecName)
}

type Input struct {
	IsEncoder bool // otherwise: decoder
	Params    CodecParams
}

func newCodec(
	ctx context.Context,
	input Input,
) (_ret *Codec, _err error) {
	isEncoder := input.IsEncoder
	params := input.Params.Clone(ctx)
	codecName := params.CodecName
	codecParameters := params.CodecParameters
	hardwareDeviceType := params.HardwareDeviceType
	hardwareDeviceName := params.HardwareDeviceName
	timeBase := params.TimeBase
	customOptions := params.CustomOptions
	hwDevFlags := params.HWDevFlags
	opts := params.Options
	var reusableResources *Resources
	if input.Params.ResourceManager != nil {
		reusableResources = input.Params.ResourceManager.GetReusable(
			ctx,
			input.IsEncoder,
			params.CodecParameters,
			params.TimeBase,
			opts...,
		)
	}
	ctx = belt.WithField(ctx, "is_encoder", isEncoder)
	if codecParameters.CodecID() != astiav.CodecIDNone {
		ctx = belt.WithField(ctx, "codec_id", codecParameters.CodecID())
	}
	ctx = belt.WithField(ctx, "codec_name", codecName)
	ctx = belt.WithField(ctx, "hw_dev_type", hardwareDeviceType)

	logger.Debugf(ctx, "newCodec(ctx, '%s', %s, %#+v, %t, %s, '%s', %s, %#+v, %X, %v)", codecName, codecParameters.CodecID(), codecParameters, isEncoder, hardwareDeviceType, hardwareDeviceName, timeBase, customOptions, hwDevFlags, opts)
	defer func() {
		logger.Debugf(ctx, "/newCodec(ctx, '%s', %s, %#+v, %t, %s, '%s', %s, %#+v, %X): %p %v", codecName, codecParameters.CodecID(), codecParameters, isEncoder, hardwareDeviceType, hardwareDeviceName, timeBase, customOptions, hwDevFlags, _ret, _err)
	}()
	c := &Codec{
		codecInternals: &codecInternals{
			InitParams: params,
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
		if customOptions != nil {
			return
		}
		customOptions = astiav.NewDictionary()
		setFinalizerFree(ctx, customOptions)
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
		if c.isMediaCodec() {
			if reusableResources != nil && reusableResources.HWDeviceContext != nil {
				defaultMediaCodecPixelFormat = astiav.PixelFormatMediacodec
			}
			logger.Debugf(ctx, "MediaCodec: enforcing NDK codec")
			customOptions.Set("ndk_codec", "1", 0) // NDK path
			customOptions.Set("ndk_async", "0", 0) // disable async (avoid restart-after-flush issue)
		}

		if isEncoder {
			customOptions.Set("gpu", "0", 0)
			if v := customOptions.Get("g", nil, 0); v == nil {
				fps := codecParameters.FrameRate().Float64()
				if fps < 1 {
					logger.Warnf(ctx, "unable to detect the FPS, assuming 30")
					fps = 30
				}
				gopSize = int64(0.999+fps) * 2
				logger.Warnf(ctx, "gop_size is not set, defaulting to the FPS*2 value (%d <- %f)", gopSize, fps)
				logIfError(customOptions.Set("g", fmt.Sprintf("%d", gopSize), 0))
			} else {
				var err error
				gopSize, err = strconv.ParseInt(v.Value(), 10, 64)
				logIfError(err)
			}
			if v := customOptions.Get("bf", nil, 0); v == nil {
				logger.Debugf(ctx, "bf is not set, defaulting to zero")
				logIfError(customOptions.Set("bf", "0", 0))
				bFrames = 0
			} else {
				var err error
				bFrames, err = strconv.ParseInt(v.Value(), 10, 64)
				logIfError(err)
			}
			if bFrames == 0 {
				logIfError(customOptions.Set("pts_as_dts", "1", 0))
			}
			if v := customOptions.Get("forced-idr", nil, 0); v == nil {
				logger.Debugf(ctx, "forced-idr is not set, defaulting to 1")
				logIfError(customOptions.Set("forced-idr", "1", 0))
			}
			if codecParameters.BitRate() > 0 {
				customOptions.Set("b", fmt.Sprintf("%d", codecParameters.BitRate()), 0) // TODO: figure out: do we need this?
				rcMode := "vbr"
				if v := customOptions.Get("rc", nil, 0); v == nil {
					customOptions.Set("rc", rcMode, 0)
				}
				if v := customOptions.Get("bitrate_mode", nil, 0); v == nil {
					customOptions.Set("bitrate_mode", rcMode, 0) // TODO: do we need to deduplicate this with the line above?
				}
			}
			if c.isMediaCodec() {
				{
					// TODO: delete this block, this is a temporary workaround
					//       until it'll become clear how to bypass the quality floor
					//       clamping of MediaCodec.

					// to allow low bitrates:
					h := codecParameters.Height()
					switch {
					case h <= 360:
						logger.Debugf(ctx, "setting qp parameters for MediaCodec: 80")
						customOptions.Set(mediacodec.KEY_VIDEO_QP_I_MIN, "80", 0)
						customOptions.Set(mediacodec.KEY_VIDEO_QP_B_MIN, "82", 0)
						customOptions.Set(mediacodec.KEY_VIDEO_QP_P_MIN, "84", 0)
					case h <= 560:
						logger.Debugf(ctx, "setting qp parameters for MediaCodec: 60")
						customOptions.Set(mediacodec.KEY_VIDEO_QP_I_MIN, "60", 0)
						customOptions.Set(mediacodec.KEY_VIDEO_QP_B_MIN, "62", 0)
						customOptions.Set(mediacodec.KEY_VIDEO_QP_P_MIN, "64", 0)
					case h <= 640:
						logger.Debugf(ctx, "setting qp parameters for MediaCodec: 48")
						customOptions.Set(mediacodec.KEY_VIDEO_QP_I_MIN, "48", 0)
						customOptions.Set(mediacodec.KEY_VIDEO_QP_B_MIN, "50", 0)
						customOptions.Set(mediacodec.KEY_VIDEO_QP_P_MIN, "52", 0)
					case h <= 720:
						logger.Debugf(ctx, "setting qp parameters for MediaCodec: 38")
						customOptions.Set(mediacodec.KEY_VIDEO_QP_I_MIN, "38", 0)
						customOptions.Set(mediacodec.KEY_VIDEO_QP_B_MIN, "40", 0)
						customOptions.Set(mediacodec.KEY_VIDEO_QP_P_MIN, "42", 0)
					}
				}

				if customOptions.Get("pix_fmt", nil, 0) == nil {
					logger.Warnf(ctx, "is MediaCodec, but pixel format is not set; forcing %s pixel format", defaultMediaCodecPixelFormat)
					logIfError(customOptions.Set("pix_fmt", defaultMediaCodecPixelFormat.String(), 0))
					forcePixelFormat = defaultMediaCodecPixelFormat
				}

				if customOptions.Get("sample_fmt", nil, 0) == nil {
					if strings.HasPrefix(c.codec.Name(), "aac") {
						logger.Warnf(ctx, "is AAC, but sample format is not set; forcing 'fltp' sample format", defaultMediaCodecPixelFormat)
						logIfError(customOptions.Set("sample_fmt", "fltp", 0))
					}
				}
			}
		} else {
			if c.isMediaCodec() {
				if customOptions.Get("pixel_format", nil, 0) == nil {
					logger.Warnf(ctx, "is MediaCodec, but pixel format is not set; forcing %s pixel format", defaultMediaCodecPixelFormat)
					logIfError(customOptions.Set("pixel_format", defaultMediaCodecPixelFormat.String(), 0))
					forcePixelFormat = defaultMediaCodecPixelFormat
				}

				height := codecParameters.Height()
				alignedHeight := (height + 15) &^ 15
				logger.Tracef(ctx, "MediaCodec aligned height: %d (current: %d)", alignedHeight, height)
				if alignedHeight != height && customOptions.Get("create_window", nil, 0) == nil {
					logger.Warnf(ctx, "in MediaCodec H264/HEVC heights are aligned with 16, while AV1 is not, so there could be is a green strip at the bottom during transcoding H264->AV1 (due to %dp != %dp); to handle you may want to use create_window=1 (and please use pixel_format 'mediacodec')", codecParameters.Height(), (codecParameters.Height()+15)&^15)
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
			customOptions,
			hwDevFlags,
			reusableResources,
		)
		switch {
		case err == nil:
		case errors.As(err, &ErrNotImplemented{}):
			logger.Warnf(ctx, "hardware initialization of this type is not implemented, yet: %v", err)
		default:
			switch c.codec.Name() {
			case "rawvideo":
				logger.Errorf(ctx, "unable to init hardware device context for 'rawvideo' codec, ignoring the error: %v", err)
			default:
				return nil, fmt.Errorf("unable to init hardware device context: %w", err)
			}
		}
	}

	switch codecParameters.MediaType() {
	case astiav.MediaTypeVideo:
		if bitRate := codecParameters.BitRate(); bitRate > 0 {
			logger.Tracef(ctx, "bitrate: %d", bitRate)
			c.codecContext.SetBitRate(bitRate)
			if setRateControlParameters {
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
		logger.Debugf(ctx,
			"pixel_format: %s; frame_rate: %s; gop_size: %d; device_type: %s; hw_pixel_format: %s",
			c.codecContext.PixelFormat(), c.codecContext.Framerate(),
			gopSize,
			hardwareDeviceType, c.hardwarePixelFormat,
		)
	case astiav.MediaTypeAudio:
		c.codecContext.SetChannelLayout(codecParameters.ChannelLayout())
		c.codecContext.SetSampleFormat(codecParameters.SampleFormat())
		c.codecContext.SetSampleRate(codecParameters.SampleRate())
		if customOptions != nil {
			if v := customOptions.Get("ac", nil, 0); v != nil {
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
			if v := customOptions.Get("sample_fmt", nil, 0); v != nil {
				logger.Debugf(ctx, "sample_fmt option is set to '%s'", v.Value())
				sampleFmt, err := sampleFormatFromString(v.Value())
				if err != nil {
					return nil, fmt.Errorf("unable to parse sample_fmt option value '%s': %w", v.Value(), err)
				}
				c.codecContext.SetSampleFormat(sampleFmt)
			}
			if v := customOptions.Get("ar", nil, 0); v != nil {
				logger.Debugf(ctx, "ar option is set to '%s'", v.Value())
				sampleRate, err := strconv.ParseInt(v.Value(), 10, 64)
				if err != nil {
					return nil, fmt.Errorf("unable to parse ar option value '%s' as int: %w", v.Value(), err)
				}
				c.codecContext.SetSampleRate(int(sampleRate))
			}
		}
		logger.Tracef(ctx, "sample_rate: %d; channel_layout: %s; sample_format: %s", c.codecContext.SampleRate(), c.codecContext.ChannelLayout(), c.codecContext.SampleRate())
	}

	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(ctx, "codec_parameters: %s", spew.Sdump(unsafetools.FieldByNameInValue(reflect.ValueOf(codecParameters), "c").Elem().Elem().Interface()))
	}

	logger.Debugf(ctx, "time_base == %v", timeBase)
	c.codecContext.SetTimeBase(timeBase)
	if setSetPktTimeBase {
		c.codecContext.SetPktTimeBase(timeBase)
	}
	flags := astiav.CodecContextFlags(0)
	if setPipelinishFlags {
		flags |= 0 |
			astiav.CodecContextFlags(astiav.CodecContextFlagLowDelay) // this is a streaming focused library
	}
	if setPipelinishFlags && isEncoder {
		flags |= 0 |
			astiav.CodecContextFlags(astiav.CodecContextFlagClosedGop) // to make sure we can route dynamically without issues
	}
	if c.codec.Capabilities()&astiav.CodecCapabilityDelay != 0 {
		if isEncoder && c.codec.Capabilities()&astiav.CodecCapabilityEncoderReorderedOpaque == 0 {
			logger.Warnf(ctx, "codec '%s' has 'delay' capability, but doesn't have 'encoder_reordered_opaque' capability, so it is not supported by avpipeline", c.codec.Name())
		} else {
			// avpipeline uses the opaque field to store packet info when dealing with delayed frames:
			flags |= astiav.CodecContextFlags(astiav.CodecContextFlagCopyOpaque)
		}
	}
	flags2 := astiav.CodecContextFlags2(0)
	if isEncoder && setPipelinishFlags {
		flags2 |= 0 |
			astiav.CodecContextFlags2(astiav.CodecFlag2LocalHeader) // to make sure we can route dynamically without issues
		//astiav.CodecContextFlags2(astiav.CodecFlag2Chunks) // to make sure we can route dynamically without issues
		//astiav.CodecContextFlags2(astiav.CodecFlag2ShowAll) // to do not skip frames (pre the first key frame)
	}
	c.codecContext.SetFlags(flags)
	c.codecContext.SetFlags2(flags2)
	c.codecContext.SetErrorRecognitionFlags(input.Params.ErrorRecognitionFlags)

	if isEncoder {
		if timeBase.Num() == 0 {
			return nil, fmt.Errorf("TimeBase must be set")
		}
		if setEncoderExtraData {
			c.codecContext.SetExtraData(codecParameters.ExtraData())
		}
	} else {
		c.codecContext.SetExtraData(codecParameters.ExtraData())
	}

	c.setQuirks(ctx)
	c.logHints(ctx)
	logger.Debugf(ctx, "c.codecContext.Open(%#+v, %#+v)", c.codec, customOptions)
	err := c.codecContext.Open(c.codec, customOptions)
	switch {
	case err == nil:
	case errors.Is(err, astiav.ErrExternal):
		// "Generic error in an external library"
		if c.isMediaCodec() {
			// there were known cases where MediaCodec returned ErrExternal due to
			// "ERROR_INSUFFICIENT_RESOURCE" (https://developer.android.com/reference/android/media/MediaCodec.CodecException#ERROR_INSUFFICIENT_RESOURCE)
			if input.Params.ResourceManager == nil {
				return nil, fmt.Errorf("MediaCodec returned ErrExternal: %w", err)
			}
			logger.Warnf(ctx, "MediaCodec returned ErrExternal, which could be due to ERROR_INSUFFICIENT_RESOURCE (1100); calling FreeUnneeded() and retrying")
			resourceType := resource.TypeDecoder
			if isEncoder {
				resourceType = resource.TypeEncoder
			}
			cnt := input.Params.ResourceManager.FreeUnneeded(ctx, resourceType, c.codec, opts...)
			logger.Infof(ctx, "FreeUnneeded() freed %d %ss; retrying to open the codec context", cnt, resourceType)
			newErr := c.codecContext.Open(c.codec, customOptions)
			if newErr != nil {
				return nil, fmt.Errorf("unable to open codec context (case #1): %w (before an attempt to remediate: %w)", newErr, err)
			}
		}
	default:
		return nil, fmt.Errorf("unable to open codec context (case #0): %w", err)
	}

	setFinalizer(ctx, c.codecInternals, func(c *codecInternals) { c.closeLocked(ctx) })
	return c, nil
}

func (c *Codec) setQuirks(ctx context.Context) {}

func (c *Codec) logHints(ctx context.Context) {
	if c.isMediaCodec() {
		height := c.codecContext.Height()
		suggestedHeight := (height + 15) &^ 15
		if suggestedHeight != height {
			logger.Debugf(ctx, "in MediaCodec H264/HEVC heights are aligned with 16, while AV1 is not, so there could be is a green strip at the bottom during transcoding H264->AV1 (due to %dp != %dp)", height, suggestedHeight)
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
