package codec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/quality"
)

const (
	decoderDropNonKeyFramesBeforeKeyFrame = true
)

type CallbackFrameReceiver func(
	context.Context,
	*DecoderLocked,
	astiav.CodecCapabilities,
	*astiav.Frame,
) error

type DecoderLocked struct {
	*Codec
	receivedKeyFrame bool
	IsDirtyValue     atomic.Bool
}

type DecoderInput struct {
	CodecName             Name
	CodecParameters       *astiav.CodecParameters
	HardwareDeviceType    HardwareDeviceType
	HardwareDeviceName    HardwareDeviceName
	ErrorRecognitionFlags astiav.ErrorRecognitionFlags
	CustomOptions         *astiav.Dictionary
	Flags                 int
	ResourceManager       ResourceManager
	Options               []Option
}

func (d *DecoderLocked) AsUnlocked() *Decoder {
	return (*Decoder)(d)
}

func (d *DecoderLocked) String() string {
	return fmt.Sprintf("Decoder(%s)", d.codec.Name())
}

type ErrNotKeyFrame struct{}

func (e ErrNotKeyFrame) Error() string {
	return "not a key frame"
}

func (d *DecoderLocked) SendPacket(
	ctx context.Context,
	p *astiav.Packet,
) error {
	if !d.receivedKeyFrame {
		if decoderDropNonKeyFramesBeforeKeyFrame && d.codecContext.MediaType() == astiav.MediaTypeVideo && !p.Flags().Has(astiav.PacketFlagKey) {
			return ErrNotKeyFrame{}
		}
		d.receivedKeyFrame = true
	}
	d.IsDirtyValue.Store(true)
	return d.codecContext.SendPacket(p)
}

func (d *DecoderLocked) ReceiveFrame(
	ctx context.Context,
	f *astiav.Frame,
) error {
	return d.codecContext.ReceiveFrame(f)
}

func (d *DecoderLocked) GetQuality(
	ctx context.Context,
) Quality {
	bitRate := d.codecContext.BitRate()
	if bitRate != 0 {
		return quality.ConstantBitrate(d.codecContext.BitRate())
	}
	return nil
}

func (d *DecoderLocked) SetLowLatency(
	ctx context.Context,
	v bool,
) (_err error) {
	codecName := d.codec.Name()
	logger.Debugf(ctx, "SetLowLatency(ctx): %s:%v: %v", codecName, d.InitParams.HardwareDeviceType, v)
	defer func() {
		logger.Debugf(ctx, "/SetLowLatency(ctx): %s:%v: %v: %v", codecName, d.InitParams.HardwareDeviceType, v, _err)
	}()
	defer func() {
		if _err != nil {
			_err = fmt.Errorf("%s: %w", codecName, _err)
		}
	}()

	codecWords := strings.Split(codecName, "_")
	if len(codecWords) != 2 {
		return d.setLowLatencyGeneric(ctx, v)
	}
	codecModifier := codecWords[1]
	switch strings.ToLower(codecModifier) {
	case "mediacodec":
		return d.setLowLatencyMediacodec(ctx, v)
	}
	return d.setLowLatencyGeneric(ctx, v)
}

func (d *DecoderLocked) setLowLatencyGeneric(
	ctx context.Context,
	v bool,
) error {
	logger.Infof(ctx, "SetLowLatency (Generic): %v", v)
	return fmt.Errorf("not implemented, yet")
}

func (d *DecoderLocked) Flush(
	ctx context.Context,
	callback CallbackFrameReceiver,
) (_err error) {
	logger.Tracef(ctx, "Flush")
	defer func() { logger.Tracef(ctx, "/Flush: %v", _err) }()

	defer func() {
		if _err == nil {
			if d.IsDirtyValue.Load() {
				logger.Errorf(ctx, "%v is still dirty after flush; forcing IsDirty:false", d)
				d.IsDirtyValue.Store(false)
			}
		}
	}()

	caps := d.codec.Capabilities()
	logger.Tracef(ctx, "Capabilities: %08x", caps)

	if caps&astiav.CodecCapabilityDelay == 0 {
		logger.Tracef(ctx, "the decoder has no delay, nothing to flush")
		return nil
	}

	logger.Tracef(ctx, "sending the FLUSH REQUEST pseudo-packet")
	err := d.codecContext.SendPacket(nilPacket)
	switch {
	case err == nil:
		// flushing had just been initiated
	case errors.Is(err, astiav.ErrEof):
		return nil // the decoder is already flushed
	default:
		return fmt.Errorf("unable to send the FLUSH REQUEST pseudo-packet: %w", err)
	}

	err = d.Drain(ctx, callback)
	if err != nil {
		return fmt.Errorf("unable to drain: %w", err)
	}

	logger.Tracef(ctx, "flushing buffers")
	d.codecContext.FlushBuffers()

	return nil
}

func (d *DecoderLocked) Drain(
	ctx context.Context,
	callback CallbackFrameReceiver,
) error {
	logger.Tracef(ctx, "drain")
	defer func() { logger.Tracef(ctx, "/drain") }()
	caps := d.codec.Capabilities()
	for {
		f := frame.Pool.Get()
		err := d.ReceiveFrame(ctx, f)
		if err != nil {
			frame.Pool.Pool.Put(f)
			isEOF := errors.Is(err, astiav.ErrEof)
			isEAgain := errors.Is(err, astiav.ErrEagain)
			// isEOF means that the decoder has been fully flushed
			// isEAgain means that there are no more frames to receive right now
			logger.Tracef(ctx, "decoder.ReceiveFrame(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
			if isEOF {
				d.IsDirtyValue.Store(false)
				return nil
			}
			if isEAgain {
				if caps&astiav.CodecCapabilityDelay == 0 {
					d.IsDirtyValue.Store(false)
				}
				return nil
			}
			return fmt.Errorf("unable to receive a frame from the decoder: %w", err)
		}
		logger.Tracef(ctx, "decoder.ReceiveFrame(): received a frame")
		if callback == nil {
			frame.Pool.Pool.Put(f)
			continue
		}
		err = callback(ctx, d, caps, f)
		if err != nil {
			frame.Pool.Pool.Put(f)
			return fmt.Errorf("unable to process the frame: %w", err)
		}
	}
}

func (d *DecoderLocked) ToCodecParameters(cp *astiav.CodecParameters) error {
	return d.Codec.toCodecParametersLocked(cp)
}

func (c *DecoderLocked) Reset(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Reset")
	defer func() { logger.Debugf(ctx, "/Reset: %v", _err) }()
	c.receivedKeyFrame = false
	return c.Codec.reset(ctx)
}

func (c *DecoderLocked) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Close")
	defer func() { logger.Tracef(ctx, "/Close: %v", _err) }()
	return c.closeLocked(ctx)
}
