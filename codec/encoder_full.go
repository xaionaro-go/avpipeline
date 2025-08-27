package codec

import (
	"context"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

type EncoderFullBackend = Codec
type EncoderFull EncoderFullLocked

var _ Encoder = (*EncoderFull)(nil)

func NewEncoder(
	ctx context.Context,
	params CodecParams,
	opts ...EncoderFactoryOption,
) (_ret Encoder, _err error) {
	logger.Tracef(ctx, "NewEncoder(%#+v)", params)
	defer func() { logger.Tracef(ctx, "/NewEncoder(%#+v): %T %v", params, _ret, _err) }()
	switch params.CodecName {
	case NameCopy:
		return EncoderCopy{}, nil
	case NameRaw:
		return EncoderRaw{}, nil
	}
	if v, ok := EncoderFactoryOptionLatest[EncoderFactoryOptionOnlyDummy](opts); ok {
		if v.OnlyDummy {
			return nil, ErrNotDummy{}
		}
	}
	e, err := newEncoderFullUnlocked(ctx, params, nil, opts...)
	if err != nil {
		return nil, err
	}
	return e.AsUnlocked(), nil
}

func (e *EncoderFull) unlocked() *EncoderFullLocked {
	return (*EncoderFullLocked)(e)
}

func (e *EncoderFull) String() string {
	ctx := context.TODO()
	if !e.locker.ManualTryRLock(ctx) {
		return "Encoder(<locked>)"
	}
	defer e.locker.ManualRUnlock(ctx)
	return e.unlocked().String()
}

func (e *EncoderFull) GetInitTS() time.Time {
	return e.InitTS
}

func (e *EncoderFull) SendFrame(
	ctx context.Context,
	f *astiav.Frame,
) (_err error) {
	logger.Tracef(ctx, "SendFrame")
	defer func() { logger.Tracef(ctx, "/SendFrame: %v", _err) }()
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &e.locker, e.unlocked().SendFrame, ctx, f)
}

func (e *EncoderFull) ReceivePacket(
	ctx context.Context,
	p *astiav.Packet,
) (_err error) {
	logger.Tracef(ctx, "ReceivePacket")
	defer func() { logger.Tracef(ctx, "/ReceivePacket: %v", _err) }()
	return xsync.DoA2R1(xsync.WithNoLogging(ctx, true), &e.locker, e.unlocked().ReceivePacket, ctx, p)
}

func (e *EncoderFull) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Close")
	defer func() { logger.Tracef(ctx, "/Close: %v", _err) }()
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &e.locker, e.unlocked().Close, ctx)
}

func (e *EncoderFull) SanityCheck(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "SanityCheck")
	defer func() { logger.Tracef(ctx, "/SanityCheck: %v", _err) }()
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &e.locker, e.unlocked().SanityCheck, ctx)
}

func (e *EncoderFull) Flush(
	ctx context.Context,
	callback CallbackPacketReceiver,
) error {
	return xsync.DoA2R1(ctx, &e.locker, e.unlocked().Flush, ctx, callback)
}

func (e *EncoderFull) Drain(
	ctx context.Context,
	callback CallbackPacketReceiver,
) error {
	return xsync.DoA2R1(ctx, &e.locker, e.unlocked().Drain, ctx, callback)
}

func (e *EncoderFull) IsDirty() bool {
	return e.IsDirtyValue.Load()
}
