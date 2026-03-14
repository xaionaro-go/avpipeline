// Package codec provides a high-level, thread-safe abstraction for audio and video codecs,
// wrapping FFmpeg (via astiav) to provide unified interfaces for decoding and encoding.
// It supports hardware acceleration (e.g., MediaCodec), dynamic quality/resolution
// adjustments, and resource management.
//
// codec.go defines the core Codec struct and methods for interacting with astiav.Codec.
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
	"github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	xastiav "github.com/xaionaro-go/avpipeline/types/astiav"
	"github.com/xaionaro-go/unsafetools"
	"github.com/xaionaro-go/xsync"
)

const (
	doFullCopyOfParameters   = false
	setRateControlParameters = false
	setEncoderExtraData = false // <- this is wrong, don't use it unless you are temporary debugging something
	setPipelinishFlags       = true
)

// FallbackToSoftwareOnNoHWCodec controls whether the codec initialization
// falls back to software decoding/encoding when no hardware codec variant
// is found (e.g. no mjpeg_mediacodec). When false (default), missing HW
// codec variants cause an error.
var FallbackToSoftwareOnNoHWCodec = false

type hardwareContextType int

const (
	undefinedHardwareContextType hardwareContextType = iota
	hardwareContextTypeDevice
	hardwareContextTypeFrames
	endOfHardwareContextType
)

type codecInternals struct {
	InitParams             CodecParams
	codec                  *astiav.Codec
	codecContext           *astiav.CodecContext
	hardwareDeviceContext  *astiav.HardwareDeviceContext
	hardwareFramesContext  *astiav.HardwareFramesContext
	hardwarePixelFormat    astiav.PixelFormat
	hardwareContextType    hardwareContextType
	closer                 *astikit.Closer
	quirks                 Quirks
	isDirty                bool
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

func (c *codecInternals) isNVENC() bool {
	if c.codec == nil {
		return false
	}
	return strings.HasSuffix(c.codec.Name(), "_nvenc")
}

// detectHardwareDeviceType determines the hardware device type from a codec's
// name suffix. This is the single source of truth for whether a resolved codec
// is a hardware codec and what device type it needs.
func detectHardwareDeviceType(codecName string) HardwareDeviceType {
	switch {
	case strings.HasSuffix(codecName, "_mediacodec"):
		return globaltypes.HardwareDeviceTypeMediaCodec
	case strings.HasSuffix(codecName, "_nvenc"),
		strings.HasSuffix(codecName, "_cuvid"):
		return globaltypes.HardwareDeviceTypeCUDA
	case strings.HasSuffix(codecName, "_qsv"):
		return globaltypes.HardwareDeviceTypeQSV
	case strings.HasSuffix(codecName, "_vaapi"):
		return globaltypes.HardwareDeviceTypeVAAPI
	case strings.HasSuffix(codecName, "_videotoolbox"):
		return globaltypes.HardwareDeviceTypeVideoToolbox
	case strings.HasSuffix(codecName, "_vdpau"):
		return globaltypes.HardwareDeviceTypeVDPAU
	case strings.HasSuffix(codecName, "_vulkan"):
		return globaltypes.HardwareDeviceTypeVulkan
	default:
		return globaltypes.HardwareDeviceTypeNone
	}
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
	if v, ok := types.OptionLatest[types.OptionOverrideHardwareDeviceType](opts); ok {
		hardwareDeviceType = HardwareDeviceType(v)
	}
	if v, ok := types.OptionLatest[types.OptionOverrideCustomOptions](opts); ok {
		customOptions = xastiav.DictionaryItemsToAstiav(ctx, globaltypes.DictionaryItems(v))
	}
	ctx = belt.WithField(ctx, "is_encoder", isEncoder)
	if codecParameters.CodecID() != astiav.CodecIDNone {
		ctx = belt.WithField(ctx, "codec_id", codecParameters.CodecID())
	}
	ctx = belt.WithField(ctx, "codec_name", codecName)
	ctx = belt.WithField(ctx, "hw_dev_type", hardwareDeviceType)

	logger.Debugf(ctx, "newCodec(ctx, '%s', %s, %#+v, %t, %s, '%s', %s, %#+v, %X, %v)", codecName, codecParameters.CodecID(), codecParameters, isEncoder, hardwareDeviceType, hardwareDeviceName, timeBase, customOptions, hwDevFlags, opts)
	defer func() {
		logger.Debugf(ctx, "/newCodec(ctx, '%s', %s, %#+v, %t, %s, '%s', %s, %#+v, %X, %v): %p %v", codecName, codecParameters.CodecID(), codecParameters, isEncoder, hardwareDeviceType, hardwareDeviceName, timeBase, customOptions, hwDevFlags, opts, _ret, _err)
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

	isHW := false
	c.codec = nil
	if codecName != "" && hardwareDeviceType != globaltypes.HardwareDeviceTypeNone {
		hwCodec := codecName.hwName(ctx, isEncoder, hardwareDeviceType).Codec(ctx, isEncoder)
		if hwCodec != nil {
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

	// Determine hardware nature from the resolved codec's name. This handles
	// the case where the user specified a HW codec directly (e.g. "hevc_mediacodec")
	// or findCodec resolved to one by codecID.
	if !isHW {
		detectedHWType := detectHardwareDeviceType(c.codec.Name())
		switch {
		case detectedHWType != globaltypes.HardwareDeviceTypeNone:
			// The resolved codec is inherently hardware-accelerated.
			isHW = true
			hardwareDeviceType = detectedHWType
			logger.Debugf(ctx, "codec %q is a %s hardware codec", c.codec.Name(), detectedHWType)
		case hardwareDeviceType != globaltypes.HardwareDeviceTypeNone:
			// Caller requested hardware, but we got a software codec. Try the HW variant.
			hwCodec := Name(c.codec.Name()).hwName(ctx, isEncoder, hardwareDeviceType).Codec(ctx, isEncoder)
			switch {
			case hwCodec != nil:
				isHW = true
				c.codec = hwCodec
			case FallbackToSoftwareOnNoHWCodec:
				logger.Warnf(ctx, "no %s codec found for %q, falling back to software", hardwareDeviceType, c.codec.Name())
				hardwareDeviceType = globaltypes.HardwareDeviceTypeNone
			}
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
	switch codecParameters.MediaType() {
	case astiav.MediaTypeVideo:
		lazyInitOptions()

		if c.isMediaCodec() {
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
				logger.Warnf(ctx, "gop_size is not set, defaulting to FPS*2 (%d <- %f)", gopSize, fps)
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

				if customOptions.Get("sample_fmt", nil, 0) == nil {
					if strings.HasPrefix(c.codec.Name(), "aac") {
						logger.Warnf(ctx, "is AAC, but sample format is not set; forcing 'fltp' sample format")
						logIfError(customOptions.Set("sample_fmt", "fltp", 0))
					}
				}
			}
		} else {
			if c.isMediaCodec() {
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
				logger.Debugf(ctx, "unable to init hardware device context for 'rawvideo' codec, ignoring the error: %v", err)
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
		if err := c.setupPixelFormat(ctx,
			isEncoder,
			codecParameters, customOptions,
			reusableResources,
		); err != nil {
			return nil, fmt.Errorf("unable to setup pixel format: %w", err)
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
		// If the encoder doesn't support the chosen sample format, pick the
		// best supported one. The kernel-level resampler (kernel/encoder.go)
		// converts frames at runtime, so the codec context must open with a
		// format the encoder actually accepts.
		if c.IsEncoder() {
			chosenFmt := c.codecContext.SampleFormat()
			supportedFmts := c.codec.SupportedSampleFormats()
			if len(supportedFmts) > 0 {
				supported := false
				for _, sf := range supportedFmts {
					if sf == chosenFmt {
						supported = true
						break
					}
				}
				if !supported {
					best := bestSampleFormat(supportedFmts)
					logger.Warnf(ctx, "sample format '%s' is not supported by encoder '%s', using '%s' instead (resampler will convert at runtime)", chosenFmt, c.codec.Name(), best)
					c.codecContext.SetSampleFormat(best)
				}
			}
		}
		logger.Tracef(ctx, "sample_rate: %d; channel_layout: %s; sample_format: %s", c.codecContext.SampleRate(), c.codecContext.ChannelLayout(), c.codecContext.SampleRate())
	}

	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(ctx, "codec_parameters: %s", spew.Sdump(unsafetools.FieldByNameInValue(reflect.ValueOf(codecParameters), "c").Elem().Elem().Interface()))
	}

	logger.Debugf(ctx, "time_base == %v", timeBase)
	c.codecContext.SetTimeBase(timeBase)
	if !isEncoder {
		// Decoders need pkt_timebase to interpret packet timestamps correctly.
		// Without it, cuvid and other decoders warn "Invalid pkt_timebase".
		pktTimeBase := timeBase
		if pktTimeBase.Num() == 0 {
			pktTimeBase = astiav.NewRational(1, 90000)
		}
		c.codecContext.SetPktTimeBase(pktTimeBase)
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
			// Encoder has delay but doesn't support opaque round-trip.
			// The encoder kernel handles this via a FrameInfo FIFO that
			// tracks input frame timestamps in order, so CopyOpaque is
			// not required.
			logger.Debugf(ctx, "codec '%s' has 'delay' but not 'encoder_reordered_opaque'; using FrameInfo FIFO for timestamp tracking", c.codec.Name())
		} else {
			// avpipeline uses the opaque field to store packet info when dealing with delayed frames:
			flags |= astiav.CodecContextFlags(astiav.CodecContextFlagCopyOpaque)
		}
	}
	flags2 := astiav.CodecContextFlags2(0)
	if isEncoder && setPipelinishFlags {
		flags2 |= 0 |
			astiav.CodecContextFlags2(astiav.CodecFlag2LocalHeader) // to make sure we can route dynamically without issues
		// astiav.CodecContextFlags2(astiav.CodecFlag2Chunks) // to make sure we can route dynamically without issues
		// astiav.CodecContextFlags2(astiav.CodecFlag2ShowAll) // to do not skip frames (pre the first key frame)
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

	// HwFramesCtx encoders need an explicit hw_frames_ctx allocated and set on the
	// codec context before avcodec_open2. HwDeviceCtx encoders skip this — they
	// accept software frames and upload internally (see initHardwarePixelFormat).
	if isEncoder && c.hardwareContextType == hardwareContextTypeFrames && c.hardwareDeviceContext != nil {
		err := c.initHardwareFramesContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to init hardware frames context: %w", err)
		}
	}

	logger.Debugf(ctx, "c.codecContext.Open(%#+v, %#+v)", c.codec, customOptions)
	logger.Debugf(ctx, "opening codec %s: type=%v, codec_id=%v, bit_rate=%v, sample_fmt=%v, sample_rate=%v, channel_layout=%v, width=%v, height=%v, pix_fmt=%v",
		c.codec.Name(), c.codecContext.MediaType(), c.codecContext.CodecID(), c.codecContext.BitRate(),
		c.codecContext.SampleFormat(), c.codecContext.SampleRate(), c.codecContext.ChannelLayout(),
		c.codecContext.Width(), c.codecContext.Height(), c.codecContext.PixelFormat())
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

func (c *codecInternals) setupPixelFormat(
	ctx context.Context,
	isEncoder bool,
	codecParameters *astiav.CodecParameters,
	customOptions *astiav.Dictionary,
	reusableResources *Resources,
) (_err error) {
	logger.Debugf(ctx, "setupPixelFormat(%t, %s, %#+v, %#+v)", isEncoder, codecParameters.MediaType(), codecParameters, customOptions)
	defer func() {
		logger.Debugf(ctx, "/setupPixelFormat(%t, %s, %#+v, %#+v): %v", isEncoder, codecParameters.MediaType(), codecParameters, customOptions, _err)
	}()
	if codecParameters.MediaType() != astiav.MediaTypeVideo {
		logger.Tracef(ctx, "not a video stream, skipping pixel format setup")
		return nil
	}

	// For hardware decoders (e.g. h264_cuvid), the pixel format is selected via a
	// callback set in initHardwarePixelFormat. No need to guess here.
	// Exception: MediaCodec decoders require pix_fmt to be set explicitly before
	// avcodec_open2, unlike CUVID which negotiates via the get_format callback.
	if !isEncoder && c.hardwareContextType != undefinedHardwareContextType {
		if c.isMediaCodec() {
			logger.Debugf(ctx, "MediaCodec decoder: setting pixel format to %s before codec open", c.hardwarePixelFormat)
			c.codecContext.SetPixelFormat(c.hardwarePixelFormat)
		} else {
			logger.Tracef(ctx, "hardware pixel format callback is set (%s), skipping pixel format setup", c.hardwarePixelFormat)
		}
		return nil
	}

	c.codecContext.SetPixelFormat(astiav.PixelFormatNone)

	pixelFormatOptionName := "pixel_format"
	if isEncoder {
		pixelFormatOptionName = "pix_fmt"
	}

	var forcePixelFormat astiav.PixelFormat
	if v := customOptions.Get(pixelFormatOptionName, nil, 0); v != nil {
		logger.Debugf(ctx, "%q option is set to '%s'", pixelFormatOptionName, v.Value())
		pixFmt := astiav.FindPixelFormatByName(v.Value())
		if pixFmt != 0 {
			forcePixelFormat = pixFmt
		}
	} else {
		logger.Debugf(ctx, "%q option is not set", pixelFormatOptionName)
		if c.isMediaCodec() {
			defaultMediaCodecPixelFormat := astiav.PixelFormatNv12
			if reusableResources != nil && reusableResources.HWDeviceContext != nil {
				defaultMediaCodecPixelFormat = astiav.PixelFormatMediacodec
			}
			logger.Warnf(ctx, "is MediaCodec, but pixel format is not set; forcing %s pixel format", defaultMediaCodecPixelFormat)
			if err := customOptions.Set(pixelFormatOptionName, defaultMediaCodecPixelFormat.String(), 0); err != nil {
				return fmt.Errorf("unable to set %q option: %w", pixelFormatOptionName, err)
			}
			forcePixelFormat = defaultMediaCodecPixelFormat
		}
	}

	if forcePixelFormat != 0 {
		logger.Tracef(ctx, "forcing pixel format to %s", forcePixelFormat)
		c.codecContext.SetPixelFormat(forcePixelFormat)
	}

	if c.codecContext.PixelFormat() != astiav.PixelFormatNone {
		logger.Tracef(ctx, "pixel format is already set to %s", c.codecContext.PixelFormat())
		return nil
	}

	supportedPixFmts := map[astiav.PixelFormat]struct{}{}
	for _, pixFmt := range c.codec.SupportedPixelFormats() {
		logger.Debugf(ctx, "supported pixel format: %s", pixFmt)
		supportedPixFmts[pixFmt] = struct{}{}
	}

	paramPixFmt := codecParameters.PixelFormat()

	// For HW encoders using HwDeviceCtx, the codec parameters might carry a
	// hardware pixel format (e.g. cuda from the decoder). The encoder expects
	// a software pixel format (nv12, yuv420p) and handles upload internally.
	// Skip the codec parameters match if the format is not a valid software
	// pixel format for the hardware device.
	skipParamPixFmt := false
	if isEncoder && c.hardwareDeviceContext != nil {
		if constraints := c.hardwareDeviceContext.HardwareFramesConstraints(); constraints != nil {
			validSW := constraints.ValidSoftwarePixelFormats()
			constraints.Free()
			isSWFormat := false
			for _, sw := range validSW {
				if sw == paramPixFmt {
					isSWFormat = true
					break
				}
			}
			if !isSWFormat {
				logger.Debugf(ctx, "codec parameters pixel format %s is not a valid SW format for the HW device; skipping", paramPixFmt)
				skipParamPixFmt = true
			}
		}
	}

	if !skipParamPixFmt {
		if _, ok := supportedPixFmts[paramPixFmt]; ok {
			c.codecContext.SetPixelFormat(paramPixFmt)
			logger.Tracef(ctx, "using pixel format from codec parameters: %s", paramPixFmt)
			return nil
		}
	}

	switch {
	case c.isNVENC():
		logger.Debugf(ctx, "pixel format is not set, defaulting to nv12 for NVENC")
		c.codecContext.SetPixelFormat(astiav.PixelFormatNv12)
	default:
		logger.Warnf(ctx, "pixel format is not set, so applying the first supported one")
		if pixFmts := c.codec.SupportedPixelFormats(); len(pixFmts) > 0 {
			c.codecContext.SetPixelFormat(pixFmts[0])
		} else {
			defaultPixelFormat := codecParameters.PixelFormat()
			if codecParameters.PixelFormat() == astiav.PixelFormatNone {
				defaultPixelFormat = astiav.PixelFormatNv12
			}
			logger.Warnf(ctx, "codec doesn't report supported pixel formats, unable to select one; guessing %s should be fine", defaultPixelFormat)
			c.codecContext.SetPixelFormat(defaultPixelFormat)
		}
	}

	logger.Tracef(ctx, "selected pixel format: %s", c.codecContext.PixelFormat())
	return nil
}

func (c *Codec) initHardwarePixelFormat(
	ctx context.Context,
	hardwareDeviceType HardwareDeviceType,
) (_err error) {
	logger.Tracef(ctx, "initHardwarePixelFormat")
	defer func() { logger.Tracef(ctx, "/initHardwarePixelFormat: %v %v", c.hardwarePixelFormat, _err) }()

	// Prefer HwDeviceCtx over HwFramesCtx because HwDeviceCtx lets the encoder
	// accept software frames (e.g. nv12) and handle GPU upload internally.
	// HwFramesCtx requires us to: allocate a hw_frames_ctx, set it on the codec
	// context, transfer every frame from SW→HW via TransferHardwareData before
	// encoding, and match the hw_frames_ctx dimensions/format exactly. NVENC
	// exposes both modes (config[0]=HwFramesCtx/cuda, config[1]=HwDeviceCtx/None);
	// HwDeviceCtx is the correct choice for our pipeline because frames arrive
	// from decoders in software format (or are transferred to SW in getScaledFrame).
	for _, hwCfgs := range c.codec.HardwareConfigs() {
		logger.Tracef(ctx, "hw config: %v %v %v", hwCfgs.PixelFormat(), hwCfgs.MethodFlags(), hwCfgs.HardwareDeviceType())
		if hwCfgs.HardwareDeviceType() != astiav.HardwareDeviceType(hardwareDeviceType) {
			continue
		}
		if hwCfgs.MethodFlags().Has(astiav.CodecHardwareConfigMethodFlagHwDeviceCtx) {
			c.hardwareContextType = hardwareContextTypeDevice
			c.hardwarePixelFormat = hwCfgs.PixelFormat()
			break
		}
	}

	// Fall back to HwFramesCtx if no HwDeviceCtx config was found.
	if c.hardwareContextType == undefinedHardwareContextType {
		for _, hwCfgs := range c.codec.HardwareConfigs() {
			if hwCfgs.HardwareDeviceType() != astiav.HardwareDeviceType(hardwareDeviceType) {
				continue
			}
			if hwCfgs.MethodFlags().Has(astiav.CodecHardwareConfigMethodFlagHwFramesCtx) {
				c.hardwareContextType = hardwareContextTypeFrames
				c.hardwarePixelFormat = hwCfgs.PixelFormat()
				break
			}
		}
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

func (c *Codec) initHardwareFramesContext(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "initHardwareFramesContext(hw_pix_fmt=%s, %dx%d)",
		c.hardwarePixelFormat,
		c.codecContext.Width(), c.codecContext.Height(),
	)
	defer func() { logger.Debugf(ctx, "/initHardwareFramesContext: %v", _err) }()

	if c.hardwareDeviceContext == nil {
		return fmt.Errorf("hardware device context is nil")
	}

	// Determine the correct software pixel format from hardware constraints,
	// matching the pattern in the reference example (go-astiav hardware_encoding).
	constraints := c.hardwareDeviceContext.HardwareFramesConstraints()
	if constraints == nil {
		return fmt.Errorf("unable to get hardware frames constraints")
	}
	defer constraints.Free()

	validSWFormats := constraints.ValidSoftwarePixelFormats()
	if len(validSWFormats) == 0 {
		return fmt.Errorf("no valid software pixel formats for this hardware device")
	}

	// Use the codec context's current pixel format if it's a valid software format.
	// Otherwise fall back to the first valid software format from constraints.
	softwarePixelFormat := astiav.PixelFormatNone
	codecCtxPixFmt := c.codecContext.PixelFormat()
	for _, swFmt := range validSWFormats {
		if swFmt == codecCtxPixFmt {
			softwarePixelFormat = codecCtxPixFmt
			break
		}
	}
	if softwarePixelFormat == astiav.PixelFormatNone {
		softwarePixelFormat = validSWFormats[0]
		logger.Debugf(ctx, "codec context pixel format %s is not a valid SW format for this HW device; using %s",
			codecCtxPixFmt, softwarePixelFormat)
	}

	hfc := astiav.AllocHardwareFramesContext(c.hardwareDeviceContext)
	if hfc == nil {
		return fmt.Errorf("unable to allocate hardware frames context")
	}

	hfc.SetWidth(c.codecContext.Width())
	hfc.SetHeight(c.codecContext.Height())
	hfc.SetHardwarePixelFormat(c.hardwarePixelFormat)
	hfc.SetSoftwarePixelFormat(softwarePixelFormat)
	hfc.SetInitialPoolSize(20)

	if err := hfc.Initialize(); err != nil {
		hfc.Free()
		return fmt.Errorf("unable to initialize hardware frames context (hw=%s, sw=%s): %w",
			c.hardwarePixelFormat, softwarePixelFormat, err)
	}

	c.hardwareFramesContext = hfc
	c.closer.Add(hfc.Free)

	// For hw_frames_ctx mode, the codec context pixel format must be set to the
	// hardware pixel format. The encoder receives hardware frames, and the
	// SW→HW transfer is handled in SendFrame.
	c.codecContext.SetPixelFormat(c.hardwarePixelFormat)
	c.codecContext.SetHardwareFramesContext(hfc)

	logger.Debugf(ctx, "initialized hardware frames context: hw_pix_fmt=%s sw_pix_fmt=%s %dx%d",
		c.hardwarePixelFormat, softwarePixelFormat,
		c.codecContext.Width(), c.codecContext.Height(),
	)
	return nil
}
