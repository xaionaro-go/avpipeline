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
	width, height uint32,
	when condition.Condition,
) (_err error) {
	logger.Debugf(ctx, "SetResolution(ctx, %dx%d)", width, height)
	defer func() { logger.Tracef(ctx, "/SetResolution(ctx, %dx%d): %v", width, height, _err) }()
	return xsync.DoA4R1(xsync.WithNoLogging(ctx, true), &e.locker, e.setResolutionLocked, ctx, width, height, when)
}

func (e *EncoderFull) setResolutionLocked(
	ctx context.Context,
	width, height uint32,
	when condition.Condition,
) (_err error) {
	if when == nil {
		return e.setResolutionNow(ctx, width, height)
	}
	logger.Tracef(ctx, "setResolutionLocked(): will set the new resolution when condition '%s' is satisfied", when)
	e.Next.Set(SwitchEncoderParams{
		When: when,
		Resolution: &Resolution{
			Width:  width,
			Height: height,
		},
	})
	return nil
}

func (e *EncoderFull) setResolutionNow(
	ctx context.Context,
	width, height uint32,
) (_err error) {
	codecName := e.codec.Name()
	logger.Debugf(ctx, "setResolutionNow(ctx, %dx%d): %s:%v", width, height, codecName, e.InitParams.HardwareDeviceType)
	defer func() {
		logger.Debugf(ctx, "/setResolutionNow(ctx, %dx%d): %s:%v: %v", width, height, codecName, e.InitParams.HardwareDeviceType, _err)
	}()
	defer func() {
		if _err != nil {
			_err = fmt.Errorf("%s: %w", codecName, _err)
		}
	}()

	return e.setResolutionGeneric(ctx, width, height)
}

func (e *EncoderFull) setResolutionGeneric(
	ctx context.Context,
	width, height uint32,
) (_err error) {
	curW, curH := e.getResolutionLocked(ctx)
	if width == curW && height == curH {
		logger.Debugf(ctx, "the resolution is already %dx%d", width, height)
		return nil
	}
	logger.Infof(ctx, "SetResolution (generic): %dx%d", width, height)
	e.InitParams.CodecParameters.SetWidth(int(width))
	e.InitParams.CodecParameters.SetHeight(int(height))
	defer func() {
		if _err != nil {
			e.InitParams.CodecParameters.SetWidth(int(curW))
			e.InitParams.CodecParameters.SetHeight(int(curH))
		}
	}()
	newEncoder, err := newEncoder(ctx, e.InitParams, e.Quality)
	if err != nil {
		return fmt.Errorf("unable to initialize new encoder for resolution %dx%d: %w", width, height, err)
	}
	if err := e.closeLocked(ctx); err != nil {
		logger.Errorf(ctx, "unable to close the old encoder: %v", err)
	}
	*e = *newEncoder
	return nil
}
