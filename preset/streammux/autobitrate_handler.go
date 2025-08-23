package streammux

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/quality"
)

type AutoBitRateCalculator = types.AutoBitRateCalculator
type AutoBitRateConfig = types.AutoBitRateConfig
type AutoBitRateResolutionAndBitRateConfig = types.AutoBitRateResolutionAndBitRateConfig

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

func GetDefaultAutoBitrateResolutionsConfig(codecID astiav.CodecID) []AutoBitRateResolutionAndBitRateConfig {
	switch codecID {
	case astiav.CodecIDH264:
		return []AutoBitRateResolutionAndBitRateConfig{
			{
				Resolution:  codec.Resolution{Width: 3840, Height: 2160},
				BitrateHigh: 24 << 20, BitrateLow: 8 << 20, // 24 Mbps .. 8 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 2560, Height: 1440},
				BitrateHigh: 12 << 20, BitrateLow: 4 << 20, // 12 Mbps .. 4 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 1920, Height: 1080},
				BitrateHigh: 6 << 20, BitrateLow: 2 << 20, // 6 Mbps .. 2 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 1280, Height: 720},
				BitrateHigh: 3 << 20, BitrateLow: 1 << 20, // 3 Mbps .. 1 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 854, Height: 480},
				BitrateHigh: 2 << 20, BitrateLow: 128 << 10, // 2 Mbps .. 128 Kbps
			},
		}
	case astiav.CodecIDHevc:
		return multiplyBitRates(GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDH264), 0.7)
	case astiav.CodecIDAv1:
		return multiplyBitRates(GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDH264), 0.5)
	default:
		panic(fmt.Errorf("unsupported codec for DefaultAutoBitrateConfig: %s", codecID))
	}
}

func DefaultAutoBitrateConfig(
	codecID astiav.CodecID,
	highestResolutionHeight uint32,
) AutoBitRateConfig {
	resolutions := GetDefaultAutoBitrateResolutionsConfig(codecID)
	for i := range resolutions {
		if resolutions[i].Height <= highestResolutionHeight {
			resolutions = resolutions[i:]
			break
		}
	}
	result := AutoBitRateConfig{
		ResolutionsAndBitRates: resolutions,
		Calculator:             DefaultAutoBitrateCalculatorThresholds(),
		CheckInterval:          time.Second,
	}
	return result
}

func (o *Output[C]) autoBitRateHandler(
	cfg AutoBitRateConfig,
	queueSizer processor.GetInternalQueueSizer,
) *AutoBitRateHandler[C] {
	r := &AutoBitRateHandler[C]{
		AutoBitRateConfig: cfg,
		Output:            o,
		QueueSizer:        queueSizer,
		closureSignaler:   closuresignaler.New(),
	}
	o.AutoBitRateHandler = r
	return r
}

type AutoBitRateHandler[C any] struct {
	AutoBitRateConfig
	Output          *Output[C]
	QueueSizer      processor.GetInternalQueueSizer
	closureSignaler *closuresignaler.ClosureSignaler
}

func (h *AutoBitRateHandler[C]) String() string {
	return fmt.Sprintf("AutoBitrateHandler(output_id=%d)", h.Output.ID)
}

func (h *AutoBitRateHandler[C]) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "AutoBitrateHandler.Close()")
	defer func() { logger.Debugf(ctx, "/AutoBitrateHandler.Close(): %v", _err) }()
	h.closureSignaler.Close(ctx)
	return nil
}

func (h *AutoBitRateHandler[C]) ServeContext(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "autoBitRateHandler(%d).ServeContext()", h.Output.ID)
	defer func() { logger.Debugf(ctx, "autoBitRateHandler(%d).ServeContext(): %v", h.Output.ID, _err) }()
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
	encoders := h.Output.RecoderNode.Processor.Kernel.Encoder.EncoderFactory.VideoEncoders
	if len(encoders) != 1 {
		panic(fmt.Sprintf("expected exactly one video encoder, got %d", len(encoders)))
	}
	return encoders[0]
}

func (h *AutoBitRateHandler[C]) checkOnce(
	ctx context.Context,
) {
	queueSize := h.QueueSizer.GetInternalQueueSize(ctx)
	if queueSize == nil {
		logger.Tracef(ctx, "autoBitRateHandler: unable to get queue size")
		return
	}

	logger.Tracef(ctx, "autoBitRateHandler: %+v", queueSize)

	var totalQueue uint64
	for _, q := range queueSize {
		totalQueue += q
	}

	encoder := h.GetEncoder()

	var curBitRate uint64
	q := encoder.GetQuality(ctx)
	if q, ok := q.(quality.ConstantBitrate); ok {
		curBitRate = uint64(q)
	} else {
		logger.Debugf(ctx, "autoBitRateHandler: unable to get current bitrate")
	}

	newBitRate := h.Calculator.CalculateBitRate(
		ctx,
		curBitRate,
		totalQueue,
		&h.AutoBitRateConfig,
	)

	if newBitRate == curBitRate {
		logger.Tracef(ctx, "autoBitRateHandler: bitrate remains unchanged: %d", curBitRate)
		return
	}

	logger.Infof(ctx, "autoBitRateHandler: changing bitrate from %d to %d", curBitRate, newBitRate)
	h.setBitrate(ctx, newBitRate)
}

func (h *AutoBitRateHandler[C]) setBitrate(
	ctx context.Context,
	bitrate uint64,
) {
	logger.Infof(ctx, "autoBitRateHandler: setting bitrate to %d", bitrate)
	defer func() { logger.Infof(ctx, "autoBitRateHandler: setting bitrate to %d: done", bitrate) }()

	if err := h.changeResolutionIfNeeded(ctx, bitrate); err != nil {
		logger.Errorf(ctx, "autoBitRateHandler: unable to change resolution: %v", err)
	}

	err := h.GetEncoder().SetQuality(ctx, quality.ConstantBitrate(bitrate), nil)
	if err != nil {
		logger.Errorf(ctx, "autoBitRateHandler: unable to set bitrate to %d: %v", bitrate, err)
		return
	}
}

func (h *AutoBitRateHandler[C]) changeResolutionIfNeeded(
	ctx context.Context,
	bitrate uint64,
) (_err error) {
	logger.Tracef(ctx, "autoBitRateHandler: changeResolutionIfNeeded(bitrate=%d)", bitrate)
	defer func() {
		logger.Tracef(ctx, "autoBitRateHandler: changeResolutionIfNeeded(bitrate=%d): %v", bitrate, _err)
	}()

	encoder := h.GetEncoder()

	res := encoder.GetResolution(ctx)
	if res == nil {
		return fmt.Errorf("unable to get current resolution")
	}

	resCfg := h.AutoBitRateConfig.ResolutionsAndBitRates.Find(*res)
	if resCfg == nil {
		return fmt.Errorf("unable to find a resolution config for the current resolution %v", *res)
	}

	var newRes AutoBitRateResolutionAndBitRateConfig
	switch {
	case bitrate < resCfg.BitrateLow:
		_newRes := h.AutoBitRateConfig.ResolutionsAndBitRates.BitRate(bitrate).Best()
		if _newRes == nil {
			logger.Debugf(ctx, "autoBitRateHandler: already at the lowest resolution %v", *res)
			return nil
		}
		newRes = *_newRes
	case bitrate > resCfg.BitrateHigh:
		_newRes := h.AutoBitRateConfig.ResolutionsAndBitRates.BitRate(bitrate).Worst()
		if _newRes == nil {
			logger.Debugf(ctx, "autoBitRateHandler: already at the highest resolution %v", *res)
			return nil
		}
		newRes = *_newRes
	default:
		return nil
	}

	logger.Infof(ctx, "autoBitRateHandler: changing resolution from %v to %v", *res, newRes.Resolution)
	err := encoder.SetResolution(ctx, newRes.Resolution, nil)
	if err != nil {
		return fmt.Errorf("unable to set resolution to %v: %w", newRes.Resolution, err)
	}
	return nil
}
