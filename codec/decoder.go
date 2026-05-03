// decoder.go provides the public Decoder API and initialization logic.

package codec

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

type Decoder DecoderLocked

func NewDecoder(
	ctx context.Context,
	decInput DecoderInput,
) (_ret *Decoder, _err error) {
	_codecParameters := astiav.AllocCodecParameters()
	defer _codecParameters.Free()
	decInput.CodecParameters.Copy(_codecParameters)
	input := Input{
		IsEncoder: false,
		Params: CodecParams{
			CodecName:             decInput.CodecName,
			CodecParameters:       _codecParameters,
			HardwareDeviceType:    decInput.HardwareDeviceType,
			HardwareDeviceName:    decInput.HardwareDeviceName,
			ErrorRecognitionFlags: decInput.ErrorRecognitionFlags,
			TimeBase:              astiav.NewRational(0, 0),
			CustomOptions:         decInput.CustomOptions,
			HWDevFlags:            decInput.Flags,
			ResourceManager:       decInput.ResourceManager,
			Options:               decInput.Options,
		},
	}
	c, err := newCodec(
		ctx,
		input,
	)
	if err != nil {
		return nil, err
	}
	return &Decoder{Codec: c}, nil
}

func (d *Decoder) locked() *DecoderLocked {
	return (*DecoderLocked)(d)
}

func (d *Decoder) String() string {
	return d.locked().String()
}

func (d *Decoder) SendPacket(
	ctx context.Context,
	p *astiav.Packet,
) error {
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &d.locker, d.locked().SendPacket, ctx, p)
}

func (d *Decoder) ReceiveFrame(
	ctx context.Context,
	f *astiav.Frame,
) error {
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &d.locker, d.locked().ReceiveFrame, ctx, f)
}

func (d *Decoder) GetQuality(
	ctx context.Context,
) Quality {
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &d.locker, d.locked().GetQuality, ctx)
}

func (d *Decoder) SetLowLatency(
	ctx context.Context,
	v bool,
) (_err error) {
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &d.locker, d.locked().SetLowLatency, ctx, v)
}

func (d *Decoder) Reset(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Reset")
	defer func() { logger.Debugf(ctx, "/Reset: %v", _err) }()
	return xsync.DoA1R1(ctx, &d.locker, d.locked().Reset, ctx)
}

func (d *Decoder) Flush(
	ctx context.Context,
	callback CallbackFrameReceiver,
) error {
	return xsync.DoA2R1(ctx, &d.locker, d.locked().Flush, ctx, callback)
}

func (d *Decoder) Drain(
	ctx context.Context,
	callback CallbackFrameReceiver,
) error {
	return xsync.DoA2R1(ctx, &d.locker, d.locked().Drain, ctx, callback)
}

func (d *Decoder) LockDo(
	ctx context.Context,
	callback func(context.Context, *DecoderLocked) error,
) error {
	return xsync.DoR1(ctx, &d.locker, func() error {
		return callback(ctx, d.locked())
	})
}

func (d *Decoder) IsDirty(
	ctx context.Context,
) bool {
	return d.isDirty.Load()
}

// HardwareFramesContextLockless returns the decoder's hw_frames_ctx
// without acquiring d.locker.
//
// The hw_frames_ctx pointer is set during newCodec() and never reassigned
// during steady-state lifetime — only freed at Close(). Reading it without
// a lock is race-free during normal operation; the encoder hot path that
// invokes this already holds Ref()s on the decoder keeping it alive.
//
// The locking variant deadlocks the encoder hot path: encoder holds its own
// locker while the decoder's locker is held by a CGO-blocked SendPacket
// waiting for downstream drain — circular deadlock. Lockless read breaks
// the cycle.
//
// nil is returned when the decoder has no hw_frames_ctx attached (SW path)
// or when the codec context itself is nil (pre-init or post-Close); the
// encoder's getScaledFrame falls through to its "no hw_frames_ctx" error
// path which drops the frame.
func (d *Decoder) HardwareFramesContextLockless() *astiav.HardwareFramesContext {
	if d.hardwareFramesContext != nil {
		return d.hardwareFramesContext
	}
	if d.codecContext != nil {
		return d.codecContext.HardwareFramesContext()
	}
	return nil
}
