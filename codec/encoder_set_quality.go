package codec

import (
	"context"
	"fmt"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/xsync"
)

func (e *EncoderFull) SetQuality(
	ctx context.Context,
	q Quality,
	when condition.Condition,
) (_err error) {
	logger.Debugf(ctx, "SetQuality(ctx, %#+v)", q)
	defer func() { logger.Tracef(ctx, "/SetQuality(ctx, %#+v): %v", q, _err) }()
	return xsync.DoA3R1(xsync.WithNoLogging(ctx, true), &e.locker, e.setQualityLocked, ctx, q, when)
}

func (e *EncoderFull) setQualityLocked(
	ctx context.Context,
	q Quality,
	when condition.Condition,
) (_err error) {
	if when == nil {
		return e.setQualityNow(ctx, q)
	}
	logger.Tracef(ctx, "setQualityLocked(): will set the new quality when condition '%s' is satisfied", when)
	e.Next.Set(SwitchEncoderParams{
		When:    when,
		Quality: q,
	})
	return nil
}

func (e *EncoderFull) setQualityNow(
	ctx context.Context,
	q Quality,
) (_err error) {
	codecName := e.codec.Name()
	logger.Debugf(ctx, "setQualityNow(ctx, %T(%v)): %s:%v", q, q, codecName, e.InitParams.HardwareDeviceType)
	defer func() {
		logger.Debugf(ctx, "/setQualityNow(ctx, %T(%v)): %s:%v: %v", q, q, codecName, e.InitParams.HardwareDeviceType, _err)
	}()
	defer func() {
		if _err != nil {
			_err = fmt.Errorf("%s: %w", codecName, _err)
		}
	}()

	codecWords := strings.Split(codecName, "_")
	if len(codecWords) != 2 {
		return e.setQualityGeneric(ctx, q)
	}
	codecModifier := codecWords[1]
	switch strings.ToLower(codecModifier) {
	case "mediacodec":
		return e.setQualityMediacodec(ctx, q)
	}
	return e.setQualityGeneric(ctx, q)
}

func (e *EncoderFull) setQualityGeneric(
	ctx context.Context,
	q Quality,
) (_err error) {
	if q == e.Quality {
		logger.Debugf(ctx, "the quality is already %T(%v)", q, q)
		return nil
	}
	logger.Infof(ctx, "SetQuality (generic): %T(%v)", q, q)
	e.InitParams.CodecParameters.SetBitRate(0)
	// TODO: consider user Reset() instead of reimplementing the same logic
	newEncoder, err := newEncoder(ctx, e.InitParams, q)
	if err != nil {
		return fmt.Errorf("unable to initialize new encoder for quality %#+v: %w", q, err)
	}
	if err := e.closeLocked(ctx); err != nil {
		logger.Errorf(ctx, "unable to close the old encoder: %v", err)
	}
	*e = *newEncoder
	e.Quality = q
	return nil
}
