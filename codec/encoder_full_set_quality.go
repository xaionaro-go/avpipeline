// encoder_full_set_quality.go provides quality control methods for the full encoder.

package codec

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/xsync"
)

func (e *EncoderFull) SetQuality(
	ctx context.Context,
	q Quality,
	when condition.Condition,
) (_err error) {
	logger.Debugf(ctx, "SetQuality(ctx, %#+v)", q)
	defer func() { logger.Tracef(ctx, "/SetQuality(ctx, %#+v): %v", q, _err) }()
	return xsync.DoA3R1(xsync.WithNoLogging(ctx, true), &e.locker, e.asLocked().SetQuality, ctx, q, when)
}

func (e *EncoderFullLocked) SetQuality(
	ctx context.Context,
	q Quality,
	when condition.Condition,
) (_err error) {
	if when == nil {
		return e.setQualityNow(ctx, q)
	}
	logger.Tracef(ctx, "SetQuality(): will set the new quality when condition '%s' is satisfied", when)
	e.Next.Set(SwitchEncoderParams{
		When:    when,
		Quality: q,
	})
	return nil
}

func (e *EncoderFullLocked) setQualityNow(
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

	if q == e.Quality {
		logger.Debugf(ctx, "the quality is already %T(%v)", q, q)
		return nil
	}

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

func (e *EncoderFullLocked) setQualityGeneric(
	ctx context.Context,
	q Quality,
) (_err error) {
	logger.Tracef(ctx, "setQualityGeneric: %T(%v)", q, q)
	defer func() { logger.Tracef(ctx, "/setQualityGeneric: %T(%v): %v", q, q, _err) }()

	oldQ := e.Quality
	defer func() {
		if _err != nil {
			e.Quality = oldQ
		}
	}()
	e.Quality = q

	switch q := q.(type) {
	case quality.ConstantBitrate:
		e.codecContext.SetBitRate(int64(q))
		return nil
	case quality.ConstantQuality:
		e.codecContext.PrivateData().Options().Set("crf", fmt.Sprintf("%d", int(q)), 0)
		return nil
	default:
		return fmt.Errorf("quality type %T is not supported, yet", q)
	}
}

func (e *EncoderFullLocked) setQualityCodecReinit(
	ctx context.Context,
	q Quality,
) (_err error) {
	logger.Tracef(ctx, "setQualityGeneric: %T(%v)", q, q)
	defer func() { logger.Tracef(ctx, "/setQualityGeneric: %T(%v): %v", q, q, _err) }()
	oldQ := e.Quality
	defer func() {
		if _err != nil {
			e.Quality = oldQ
		}
	}()
	e.Quality = q
	logger.Infof(ctx, "SetQuality (generic): %T(%v)", q, q)
	e.InitParams.CodecParameters.SetBitRate(0) // TODO: explain why it's needed
	logger.Debugf(ctx, "reinitializing encoder to apply new quality %T(%v)", q, q)
	return e.reinitEncoder(ctx)
}
