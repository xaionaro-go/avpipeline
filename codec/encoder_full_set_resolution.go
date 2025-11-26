package codec

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/xsync"
)

func (e *EncoderFull) SetResolution(
	ctx context.Context,
	res Resolution,
	when condition.Condition,
) (_err error) {
	logger.Debugf(ctx, "SetResolution(ctx, %v)", res)
	defer func() { logger.Tracef(ctx, "/SetResolution(ctx, %v): %v", res, _err) }()
	return xsync.DoA3R1(xsync.WithNoLogging(ctx, true), &e.locker, e.asLocked().SetResolution, ctx, res, when)
}

func (e *EncoderFullLocked) SetResolution(
	ctx context.Context,
	res Resolution,
	when condition.Condition,
) (_err error) {
	if when == nil {
		return e.setResolutionNow(ctx, res)
	}
	logger.Tracef(ctx, "setResolutionLocked(): will set the new resolution when condition '%s' is satisfied", when)
	e.Next.Set(SwitchEncoderParams{
		When:       when,
		Resolution: &res,
	})
	return nil
}

func (e *EncoderFullLocked) setResolutionNow(
	ctx context.Context,
	res Resolution,
) (_err error) {
	codecName := e.codec.Name()
	logger.Debugf(ctx, "setResolutionNow(ctx, %v): %s:%v", res, codecName, e.InitParams.HardwareDeviceType)
	defer func() {
		logger.Debugf(ctx, "/setResolutionNow(ctx, %v): %s:%v: %v", res, codecName, e.InitParams.HardwareDeviceType, _err)
	}()
	defer func() {
		if _err != nil {
			_err = fmt.Errorf("%s: %w", codecName, _err)
		}
	}()

	return e.setResolutionGeneric(ctx, res)
}

func (e *EncoderFullLocked) setResolutionGeneric(
	ctx context.Context,
	res Resolution,
) (_err error) {
	oldRes := e.GetResolution(ctx)
	if oldRes == nil {
		return fmt.Errorf("cannot get current resolution")
	}
	if res == *oldRes {
		logger.Debugf(ctx, "the resolution is already %v", res)
		return nil
	}
	logger.Infof(ctx, "SetResolution (generic): %v", res)
	e.InitParams.CodecParameters.SetWidth(int(res.Width))
	e.InitParams.CodecParameters.SetHeight(int(res.Height))
	e.InitParams.CustomOptions.Set("s", fmt.Sprintf("%dx%d", res.Width, res.Height), 0)
	defer func() {
		if _err != nil {
			e.InitParams.CodecParameters.SetWidth(int(oldRes.Width))
			e.InitParams.CodecParameters.SetHeight(int(oldRes.Height))
			e.InitParams.CustomOptions.Set("s", fmt.Sprintf("%dx%d", oldRes.Width, oldRes.Height), 0)
		}
	}()
	logger.Debugf(ctx, "reinitializing encoder to apply new resolution %v", res)
	return e.reinitEncoder(ctx)
}
