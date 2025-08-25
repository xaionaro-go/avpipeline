package streammux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/quality"
)

type AutoBitRateCalculator = types.AutoBitRateCalculator
type AutoBitRateConfig = types.AutoBitRateConfig
type AutoBitRateResolutionAndBitRateConfig = types.AutoBitRateResolutionAndBitRateConfig
type AutoBitRateResolutionAndBitRateConfigs = types.AutoBitRateResolutionAndBitRateConfigs

func multiplyBitRates(
	resolutions []AutoBitRateResolutionAndBitRateConfig,
	k float64,
) []AutoBitRateResolutionAndBitRateConfig {
	out := make([]AutoBitRateResolutionAndBitRateConfig, len(resolutions))
	for i := range resolutions {
		out[i] = resolutions[i]
		out[i].BitrateHigh = uint64(float64(out[i].BitrateHigh) * k)
		out[i].BitrateLow = uint64(float64(out[i].BitrateLow) * k)
	}
	return out
}

func GetDefaultAutoBitrateResolutionsConfig(codecID astiav.CodecID) AutoBitRateResolutionAndBitRateConfigs {
	switch codecID {
	case astiav.CodecIDH264:
		return AutoBitRateResolutionAndBitRateConfigs{
			{
				Resolution:  codec.Resolution{Width: 3840, Height: 2160},
				BitrateHigh: 24_000_000, BitrateLow: 8_000_000, // 24 Mbps .. 8 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 2560, Height: 1440},
				BitrateHigh: 12_000_000, BitrateLow: 4_000_000, // 12 Mbps .. 4 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 1920, Height: 1080},
				BitrateHigh: 6_000_000, BitrateLow: 2_000_000, // 6 Mbps .. 2 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 1280, Height: 720},
				BitrateHigh: 3_000_000, BitrateLow: 1_000_000, // 3 Mbps .. 1 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 854, Height: 480},
				BitrateHigh: 2_000_000, BitrateLow: 500_000, // 2 Mbps .. 500 Kbps
			},
		}
	case astiav.CodecIDHevc:
		return multiplyBitRates(GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDH264), 0.85)
	case astiav.CodecIDAv1:
		return multiplyBitRates(GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDH264), 0.7)
	default:
		panic(fmt.Errorf("unsupported codec for DefaultAutoBitrateConfig: %s", codecID))
	}
}

type AutoBitrateCalculatorThresholds = types.AutoBitrateCalculatorThresholds

func DefaultAutoBitrateCalculatorThresholds() *AutoBitrateCalculatorThresholds {
	return types.DefaultAutoBitrateCalculatorThresholds()
}

func DefaultAutoBitrateConfig(
	codecID astiav.CodecID,
) AutoBitRateConfig {
	resolutions := GetDefaultAutoBitrateResolutionsConfig(codecID)
	resBest := resolutions.Best()
	resWorst := resolutions.Worst()
	result := AutoBitRateConfig{
		ResolutionsAndBitRates: resolutions,
		Calculator:             DefaultAutoBitrateCalculatorThresholds(),
		CheckInterval:          time.Second / 2,
		MinBitRate:             resWorst.BitrateLow / 10, // limiting just to avoid nonsensical values that makes automation and calculations weird
		MaxBitRate:             resBest.BitrateHigh * 2,  // limiting since there is no need to consume more channel if we already provide enough bitrate
	}
	return result
}

func (s *StreamMux[C]) initAutoBitRateHandler(
	cfg AutoBitRateConfig,
) *AutoBitRateHandler[C] {
	if s.AutoBitRateHandler != nil {
		panic("AutoBitRateHandler is already initialized")
	}
	r := &AutoBitRateHandler[C]{
		AutoBitRateConfig: cfg,
		StreamMux:         s,
		closureSignaler:   closuresignaler.New(),
		previousQueueSize: map[kernel.GetInternalQueueSizer]uint64{},
	}
	s.AutoBitRateHandler = r
	return r
}

type AutoBitRateHandler[C any] struct {
	AutoBitRateConfig
	StreamMux         *StreamMux[C]
	closureSignaler   *closuresignaler.ClosureSignaler
	previousQueueSize map[kernel.GetInternalQueueSizer]uint64
	lastBitRate       uint64
	lastCheckTS       time.Time
}

func (h *AutoBitRateHandler[C]) String() string {
	return "AutoBitRateHandler"
}

func (h *AutoBitRateHandler[C]) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close()")
	defer func() { logger.Debugf(ctx, "/Close(): %v", _err) }()
	h.closureSignaler.Close(ctx)
	return nil
}

func (h *AutoBitRateHandler[C]) ServeContext(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "ServeContext")
	defer func() { logger.Debugf(ctx, "/ServeContext: %v", _err) }()
	t := time.NewTicker(h.CheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-h.closureSignaler.CloseChan():
			return io.EOF
		case <-t.C:
		}
		h.checkOnce(ctx)
	}
}

func (h *AutoBitRateHandler[C]) GetEncoder() codec.Encoder {
	o := h.StreamMux.GetActiveOutput()
	if o == nil {
		return nil
	}
	encoders := o.RecoderNode.Processor.Kernel.Encoder.EncoderFactory.VideoEncoders
	if len(encoders) != 1 {
		return nil
	}
	return encoders[0]
}

func (h *AutoBitRateHandler[C]) checkOnce(
	ctx context.Context,
) {
	var activeOutput *Output[C]
	var getQueueSizers []kernel.GetInternalQueueSizer
	h.StreamMux.Locker.Do(ctx, func() {
		activeOutput = h.StreamMux.GetActiveOutput()
		for _, o := range h.StreamMux.Outputs {
			if o == nil {
				continue
			}
			outputProc, ok := o.OutputNode.GetProcessor().(kernel.GetInternalQueueSizer)
			if !ok {
				logger.Errorf(ctx, "processor %s does not implement GetInternalQueueSizer", o.OutputNode.GetProcessor())
				continue
			}
			getQueueSizers = append(getQueueSizers, outputProc)
		}
	})

	now := time.Now()
	tsDiff := now.Sub(h.lastCheckTS)
	h.lastCheckTS = now

	var totalQueue uint64
	for _, proc := range getQueueSizers {
		nodeReqCtx, cancelFn := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancelFn()
		queueSize := proc.GetInternalQueueSize(nodeReqCtx)
		reqErr := nodeReqCtx.Err()
		if queueSize == nil && reqErr == nil {
			logger.Warnf(ctx, "unable to get queue size")
			continue
		}
		var nodeTotalQueue uint64
		if reqErr != nil {
			if any(activeOutput.OutputNode.GetProcessor) == any(proc) {
				logger.Errorf(ctx, "timed out on getting queue size on the active output; assuming the queue increased by %v*%d", tsDiff, h.lastBitRate/8)
				nodeTotalQueue = h.previousQueueSize[proc] + uint64(tsDiff.Seconds()*float64(h.lastBitRate)/8.0)
			} else {
				logger.Errorf(ctx, "timed out on getting queue size on a non-active output; assuming the queue size remained the same")
				nodeTotalQueue = h.previousQueueSize[proc]
			}
		} else {
			logger.Tracef(ctx, "node queue size details: %+v", queueSize)
			for _, q := range queueSize {
				nodeTotalQueue += q
			}
			h.previousQueueSize[proc] = nodeTotalQueue
		}
		logger.Tracef(ctx, "nodeTotalQueue: %d", nodeTotalQueue)
		totalQueue += nodeTotalQueue
	}
	logger.Tracef(ctx, "total queue size: %d", totalQueue)

	encoder := h.GetEncoder()
	if encoder == nil {
		logger.Warnf(ctx, "unable to get encoder")
		return
	}

	var curBitRate uint64
	q := encoder.GetQuality(ctx)
	if q, ok := q.(quality.ConstantBitrate); ok {
		curBitRate = uint64(q)
	} else {
		logger.Debugf(ctx, "unable to get current bitrate")
	}

	newBitRate := h.Calculator.CalculateBitRate(
		ctx,
		curBitRate,
		totalQueue,
		&h.AutoBitRateConfig,
	)
	logger.Debugf(ctx, "calculated new bitrate: %d (current: %d); queue size: %d", newBitRate, curBitRate, totalQueue)

	if newBitRate == curBitRate {
		logger.Tracef(ctx, "bitrate remains unchanged: %d", curBitRate)
		return
	}

	if err := h.setBitrate(ctx, curBitRate, newBitRate); err != nil {
		logger.Errorf(ctx, "unable to set new bitrate: %v", err)
		return
	}
}

func (h *AutoBitRateHandler[C]) setBitrate(
	ctx context.Context,
	oldBitRate uint64,
	newBitRate uint64,
) (_err error) {
	logger.Tracef(ctx, "setBitrate: %d->%d", oldBitRate, newBitRate)
	defer func() { logger.Tracef(ctx, "/setBitrate: %d->%d: %v", oldBitRate, newBitRate, _err) }()

	if err := h.changeResolutionIfNeeded(ctx, newBitRate); err != nil {
		return fmt.Errorf("unable to change resolution: %w", err)
	}

	encoder := h.GetEncoder()
	if encoder == nil {
		logger.Warnf(ctx, "unable to get encoder")
		return
	}

	res := encoder.GetResolution(ctx)
	if res == nil {
		return fmt.Errorf("unable to get current resolution")
	}

	resCfg := h.AutoBitRateConfig.ResolutionsAndBitRates.Find(*res)
	if resCfg == nil {
		return fmt.Errorf("unable to find a resolution config for the current resolution %v", *res)
	}

	clampedBitRate := newBitRate
	switch {
	case newBitRate < h.MinBitRate:
		clampedBitRate = h.MinBitRate
	case newBitRate > h.MaxBitRate:
		clampedBitRate = h.MaxBitRate
	}

	if clampedBitRate == oldBitRate {
		logger.Debugf(ctx, "bitrate remains unchanged after clamping: %d (resCfg: %#+v)", oldBitRate, resCfg)
		return nil
	}

	logger.Infof(ctx, "changing bitrate from %d to %d (resCfg: %#+v)", oldBitRate, clampedBitRate, resCfg)
	if err := encoder.SetQuality(ctx, quality.ConstantBitrate(clampedBitRate), nil); err != nil {
		return fmt.Errorf("unable to set bitrate to %d: %w", clampedBitRate, err)
	}

	return nil
}

func (h *AutoBitRateHandler[C]) changeResolutionIfNeeded(
	ctx context.Context,
	bitrate uint64,
) (_err error) {
	logger.Tracef(ctx, "changeResolutionIfNeeded(bitrate=%d)", bitrate)
	defer func() {
		logger.Tracef(ctx, "/changeResolutionIfNeeded(bitrate=%d): %v", bitrate, _err)
	}()

	encoder := h.GetEncoder()
	if encoder == nil {
		logger.Warnf(ctx, "unable to get encoder")
		return
	}

	res := encoder.GetResolution(ctx)
	if res == nil {
		return fmt.Errorf("unable to get current resolution")
	}

	resCfg := h.AutoBitRateConfig.ResolutionsAndBitRates.Find(*res)
	if resCfg == nil {
		return fmt.Errorf("unable to find a resolution config for the current resolution %v", *res)
	}

	logger.Tracef(ctx, "current resolution: %v; resCfg: %v", *res, resCfg)

	var newRes AutoBitRateResolutionAndBitRateConfig
	switch {
	case bitrate < resCfg.BitrateLow:
		_newRes := h.AutoBitRateConfig.ResolutionsAndBitRates.BitRate(bitrate).Best()
		if _newRes == nil {
			_newRes = h.AutoBitRateConfig.ResolutionsAndBitRates.Worst()
		}
		if _newRes.Resolution == *res {
			logger.Debugf(ctx, "already at the lowest resolution %v (resCfg: %v), minBitRate: %d", *res, resCfg, resCfg.BitrateLow)
			return nil
		}
		newRes = *_newRes
	case bitrate > resCfg.BitrateHigh:
		_newRes := h.AutoBitRateConfig.ResolutionsAndBitRates.BitRate(bitrate).Worst()
		if _newRes == nil {
			_newRes = h.AutoBitRateConfig.ResolutionsAndBitRates.Best()
		}
		if _newRes.Resolution == *res {
			logger.Debugf(ctx, "already at the highest resolution %v (resCfg: %v), maxBitRate: %d", *res, resCfg, resCfg.BitrateHigh)
			return nil
		}
		newRes = *_newRes
	default:
		return nil
	}

	err := h.StreamMux.SetResolution(ctx, newRes.Resolution)
	switch {
	case err == nil:
		logger.Infof(ctx, "changed resolution from %v to %v (bitrate: %d)", *res, newRes.Resolution, bitrate)
		return nil
	case errors.As(err, &ErrNotImplemented{}):
		logger.Debugf(ctx, "resolution change is not implemented: %v", err)
		return nil
	default:
		return fmt.Errorf("unable to set resolution to %v: %w", newRes.Resolution, err)
	}
}
