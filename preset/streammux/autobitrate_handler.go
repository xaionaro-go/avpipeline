package streammux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type AutoBitRateCalculator = types.AutoBitRateCalculator
type AutoBitRateConfig = types.AutoBitRateConfig
type AutoBitRateResolutionAndBitRateConfig = types.AutoBitRateResolutionAndBitRateConfig
type AutoBitRateResolutionAndBitRateConfigs = types.AutoBitRateResolutionAndBitRateConfigs
type BitRateChangeRequest = types.BitRateChangeRequest
type CalculateBitRateRequest = types.CalculateBitRateRequest

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
type AutoBitrateCalculatorConstantQueueSize = types.AutoBitrateCalculatorLogK
type AutoBitrateCalculatorQueueSizeGapDecay = types.AutoBitrateCalculatorQueueSizeGapDecay
type FPSReducerConfig = types.FPSReducerConfig

func DefaultAutoBitrateCalculatorThresholds() *AutoBitrateCalculatorThresholds {
	return types.DefaultAutoBitrateCalculatorThresholds()
}

func DefaultAutoBitrateCalculatorLogK() *AutoBitrateCalculatorConstantQueueSize {
	return types.DefaultAutoBitrateCalculatorLogK()
}

func DefaultAutoBitrateCalculatorQueueSizeGapDecay() *AutoBitrateCalculatorQueueSizeGapDecay {
	return types.DefaultAutoBitrateCalculatorQueueSizeGapDecay()
}

func DefaultFPSReducerConfig() FPSReducerConfig {
	return types.DefaultFPSReducerConfig()
}

func DefaultAutoBitrateConfig(
	codecID astiav.CodecID,
) AutoBitRateConfig {
	resolutions := GetDefaultAutoBitrateResolutionsConfig(codecID)
	resBest := resolutions.Best()
	resWorst := resolutions.Worst()
	result := AutoBitRateConfig{
		ResolutionsAndBitRates: resolutions,
		FPSReducer:             DefaultFPSReducerConfig(),
		Calculator:             DefaultAutoBitrateCalculatorQueueSizeGapDecay(),
		CheckInterval:          time.Second / 4,
		MinBitRate:             resWorst.BitrateLow / 10, // limiting just to avoid nonsensical values that makes automation and calculations weird
		MaxBitRate:             resBest.BitrateHigh * 2,  // limiting since there is no need to consume more channel if we already provide enough bitrate

		BitRateIncreaseSlowdown:             time.Second * 7 / 8, // essentially just skip three iterations of increasing after a decrease (to dumpen oscillations)
		ResolutionSlowdownDurationUpgrade:   time.Minute,
		ResolutionSlowdownDurationDowngrade: time.Second * 10,
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
	}
	s.AutoBitRateHandler = r
	return r
}

type resolutionChangeRequest struct {
	IsUpgrade bool // otherwise is downgrade
	StartedAt time.Time
	LatestAt  time.Time
}

type AutoBitRateHandler[C any] struct {
	AutoBitRateConfig
	StreamMux                      *StreamMux[C]
	closureSignaler                *closuresignaler.ClosureSignaler
	previousQueueSize              xsync.Map[kernel.GetInternalQueueSizer, uint64]
	lastBitRate                    uint64
	lastTotalQueueSize             uint64
	lastCheckTS                    time.Time
	currentResolutionChangeRequest *resolutionChangeRequest
	lastBitRateDecreaseTS          time.Time
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

func (h *AutoBitRateHandler[C]) checkOnce(
	ctx context.Context,
) {
	var activeOutput *Output
	var getQueueSizers []kernel.GetInternalQueueSizer
	h.StreamMux.Locker.Do(ctx, func() {
		activeOutput = h.StreamMux.waitForActiveOutputLocked(ctx)
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

	var haveAnError atomic.Bool
	var totalQueue atomic.Uint64
	activeOutputProc, _ := activeOutput.OutputNode.GetProcessor().(kernel.GetInternalQueueSizer)
	var wg sync.WaitGroup
	for _, proc := range getQueueSizers {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			nodeReqCtx, cancelFn := context.WithTimeout(ctx, max(h.AutoBitRateConfig.CheckInterval/4-50*time.Millisecond, 50*time.Millisecond))
			defer cancelFn()
			queueSize := proc.GetInternalQueueSize(nodeReqCtx)
			reqErr := nodeReqCtx.Err()
			if queueSize == nil && reqErr == nil {
				logger.Warnf(ctx, "unable to get queue size")
				return
			}
			var nodeTotalQueue uint64
			if reqErr != nil {
				previousQueueSize, _ := h.previousQueueSize.Load(proc)
				if proc == activeOutputProc {
					nodeTotalQueue = previousQueueSize + uint64(tsDiff.Seconds()*float64(h.lastBitRate)/8.0)
					logger.Infof(ctx, "timed out on getting queue size on the active output; assuming the queue increased by %v*%d (and now is: %d)", tsDiff, h.lastBitRate/8, nodeTotalQueue)
				} else {
					logger.Infof(ctx, "timed out on getting queue size on a non-active output; assuming the queue size remained the same")
					nodeTotalQueue = previousQueueSize
				}
				haveAnError.Store(true)
			} else {
				logger.Tracef(ctx, "node queue size details: %+v", queueSize)
				for _, q := range queueSize {
					nodeTotalQueue += q
				}
			}
			h.previousQueueSize.Store(proc, nodeTotalQueue)
			logger.Tracef(ctx, "nodeTotalQueue: %d", nodeTotalQueue)
			totalQueue.Add(nodeTotalQueue)
		})
	}
	wg.Wait()
	logger.Tracef(ctx, "total queue size: %d", totalQueue.Load())
	if !haveAnError.Load() {
		h.lastBitRate = h.StreamMux.CurrentOutputBitRate.Load()
	}

	encoderV, _ := h.StreamMux.GetEncoders(ctx)
	if encoderV == nil {
		logger.Warnf(ctx, "unable to get encoder")
		return
	}

	var curReqBitRate uint64
	if codec.IsEncoderCopy(encoderV) {
		logger.Tracef(ctx, "encoder is in copy mode; using the input bitrate as the current bitrate")
		curReqBitRate = h.StreamMux.CurrentInputBitRate.Load()
	} else {
		logger.Tracef(ctx, "getting current bitrate from the encoder")
		q := encoderV.GetQuality(ctx)
		if q, ok := q.(quality.ConstantBitrate); ok {
			curReqBitRate = uint64(q)
		} else {
			logger.Debugf(ctx, "unable to get current bitrate")
		}
	}

	if curReqBitRate == 0 {
		logger.Debugf(ctx, "current bitrate is 0; skipping this check")
		return
	}

	actualOutputBitrate := h.StreamMux.CurrentOutputBitRate.Load()
	totalQueueSizeDerivative := (float64(totalQueue.Load()) - float64(h.lastTotalQueueSize)) / tsDiff.Seconds()
	h.lastTotalQueueSize = totalQueue.Load()
	bitRateRequest := h.Calculator.CalculateBitRate(
		ctx,
		CalculateBitRateRequest{
			CurrentBitrateSetting: types.Ubps(curReqBitRate),
			InputBitrate:          types.Ubps(h.StreamMux.CurrentInputBitRate.Load()),
			ActualOutputBitrate:   types.Ubps(actualOutputBitrate),
			QueueSize:             types.UB(totalQueue.Load()),
			QueueSizeDerivative:   types.UBps(totalQueueSizeDerivative),
			Config:                &h.AutoBitRateConfig,
		},
	)
	if bitRateRequest.BitRate == 0 {
		logger.Errorf(ctx, "calculated bitrate is 0; ignoring the calculators result")
		return
	}
	logger.Debugf(ctx, "calculated new bitrate: %s (current: %d); queue size: %d", bitRateRequest.BitRate, curReqBitRate, totalQueue.Load())

	if uint64(bitRateRequest.BitRate) == curReqBitRate {
		logger.Tracef(ctx, "bitrate remains unchanged: %d", curReqBitRate)
		return
	}

	if err := h.trySetBitrate(ctx, curReqBitRate, bitRateRequest, actualOutputBitrate); err != nil {
		logger.Errorf(ctx, "unable to set new bitrate: %v", err)
		return
	}
}

func (h *AutoBitRateHandler[C]) isBypassEnabled(
	ctx context.Context,
) bool {
	encoderV, _ := h.StreamMux.GetEncoders(ctx)
	if encoderV == nil {
		return false
	}
	return codec.IsEncoderCopy(encoderV)
}

func (h *AutoBitRateHandler[C]) enableBypass(
	ctx context.Context,
	enable bool,
	isCritical bool,
) (_err error) {
	logger.Tracef(ctx, "enableByPass: %v", enable)
	defer func() { logger.Tracef(ctx, "/enableByPass: %v: %v", enable, _err) }()

	if enable {
		return h.setOutput(
			ctx,
			&OutputKey{
				AudioCodec: codectypes.NameCopy,
				VideoCodec: codectypes.NameCopy,
			},
			0,
			isCritical,
		)
	}

	return h.setOutput(
		ctx,
		nil,
		uint64(float64(h.StreamMux.CurrentInputBitRate.Load())*0.9),
		isCritical,
	)
}

func (h *AutoBitRateHandler[C]) trySetBitrate(
	ctx context.Context,
	oldBitRate uint64,
	req BitRateChangeRequest,
	actualOutputBitrate uint64,
) (_err error) {
	logger.Tracef(ctx, "setBitrate: %d->%s (isCritical:%t)", oldBitRate, req.BitRate, req.IsCritical)
	defer func() {
		logger.Tracef(ctx, "/setBitrate: %d->%s (isCritical:%t): %v", oldBitRate, req.BitRate, req.IsCritical, _err)
	}()

	now := time.Now()
	if req.BitRate > types.Ubps(oldBitRate) && now.Sub(h.lastBitRateDecreaseTS) < h.BitRateIncreaseSlowdown {
		logger.Tracef(ctx, "bitrate increase is slowed down since the last bitrate decrease was at %v (now: %v); skipping the increase", h.lastBitRateDecreaseTS, now)
		return nil
	}
	if req.BitRate < types.Ubps(oldBitRate) {
		h.lastBitRateDecreaseTS = now
	}

	if h.currentResolutionChangeRequest != nil {
		switch {
		case uint64(req.BitRate) > oldBitRate && !h.currentResolutionChangeRequest.IsUpgrade:
			// we are increasing bitrate while a downgrade is in progress; cancel the downgrade
			logger.Tracef(ctx, "cancelling the downgrade since we are increasing bitrate: %d > %d", uint64(req.BitRate), oldBitRate)
			h.currentResolutionChangeRequest = nil
		case uint64(req.BitRate) < oldBitRate && h.currentResolutionChangeRequest.IsUpgrade:
			// we are decreasing bitrate while an upgrade is in progress; cancel the upgrade
			logger.Tracef(ctx, "cancelling the upgrade since we are decreasing bitrate: %d < %d", uint64(req.BitRate), oldBitRate)
			h.currentResolutionChangeRequest = nil
		}
	}

	inputBitRate := h.StreamMux.CurrentInputBitRate.Load()
	if h.StreamMux.AutoBitRateHandler.AutoByPass {
		logger.Tracef(ctx, "AutoByPass is enabled: %v %v %v", types.Ubps(oldBitRate), req.BitRate, types.Ubps(inputBitRate))
		if h.isBypassEnabled(ctx) {
			if uint64(req.BitRate) < inputBitRate {
				if err := h.enableBypass(ctx, false, req.IsCritical); err != nil {
					return fmt.Errorf("unable to disable bypass mode: %w", err)
				}
			}
		} else {
			if float64(req.BitRate) > float64(inputBitRate)*1.2 {
				err := h.enableBypass(ctx, true, req.IsCritical)
				if err != nil {
					return fmt.Errorf("unable to enable bypass mode: %w", err)
				}
			}
		}
	}

	if h.isBypassEnabled(ctx) {
		logger.Tracef(ctx, "bypass mode is enabled; skipping bitrate change")
		return nil
	}

	maxBitRate := h.MaxBitRate
	if inputBitRate > h.MinBitRate*2 && float64(inputBitRate)*1.5 < float64(maxBitRate) {
		maxBitRate = uint64(1.5 * float64(inputBitRate))
	}

	clampedBitRate := uint64(req.BitRate)
	switch {
	case uint64(req.BitRate) < h.MinBitRate:
		clampedBitRate = h.MinBitRate
	case uint64(req.BitRate) > maxBitRate:
		clampedBitRate = maxBitRate
	}

	fpsFractionNum, fpsFractionDen := h.AutoBitRateConfig.FPSReducer.GetFraction(clampedBitRate)
	h.StreamMux.SetFPSFraction(ctx, fpsFractionNum, fpsFractionDen)

	if err := h.changeResolutionIfNeeded(ctx, clampedBitRate, req.IsCritical); err != nil {
		return fmt.Errorf("unable to change resolution: %w", err)
	}

	encoderV, _ := h.StreamMux.GetEncoders(ctx)
	if encoderV == nil {
		logger.Warnf(ctx, "unable to get encoder")
		return
	}

	res := encoderV.GetResolution(ctx)
	if res == nil {
		return fmt.Errorf("unable to get current resolution")
	}

	resCfg := h.AutoBitRateConfig.ResolutionsAndBitRates.Find(*res)
	if resCfg == nil {
		return fmt.Errorf("unable to find a resolution config for the current resolution %v", *res)
	}

	if clampedBitRate == oldBitRate {
		logger.Debugf(ctx, "bitrate remains unchanged after clamping: %d (resCfg: %#+v)", oldBitRate, resCfg)
		return nil
	}

	logger.Infof(ctx, "changing bitrate from %d to %d (resCfg: %#+v); max:%d, min:%d, in:%d", oldBitRate, clampedBitRate, resCfg, maxBitRate, h.MinBitRate, inputBitRate)
	if err := encoderV.SetQuality(ctx, quality.ConstantBitrate(clampedBitRate), nil); err != nil {
		return fmt.Errorf("unable to set bitrate to %d: %w", clampedBitRate, err)
	}

	return nil
}

func (h *AutoBitRateHandler[C]) changeResolutionIfNeeded(
	ctx context.Context,
	bitrate uint64,
	isCritical bool,
) (_err error) {
	logger.Tracef(ctx, "changeResolutionIfNeeded(bitrate=%d, isCritical=%t)", bitrate, isCritical)
	defer func() {
		logger.Tracef(ctx, "/changeResolutionIfNeeded(bitrate=%d, isCritical=%t): %v", bitrate, isCritical, _err)
	}()

	encoderV, _ := h.StreamMux.GetEncoders(ctx)
	if encoderV == nil {
		logger.Warnf(ctx, "unable to get encoder")
		return
	}

	res := encoderV.GetResolution(ctx)
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

	err := h.setOutput(ctx, &OutputKey{
		Resolution: newRes.Resolution,
	}, bitrate, isCritical)
	if err != nil {
		return fmt.Errorf("unable to set new resolution %v: %w", newRes.Resolution, err)
	}
	return nil
}

func (h *AutoBitRateHandler[C]) setOutput(
	ctx context.Context,
	outputKey *OutputKey,
	bitrate uint64,
	isCritical bool,
) (_err error) {
	logger.Tracef(ctx, "setOutput: %v, %d (isCritical:%t)", outputKey, bitrate, isCritical)
	defer func() {
		logger.Tracef(ctx, "/setOutput: %v, %d (isCritical:%t): %v", outputKey, bitrate, isCritical, _err)
	}()

	encV, encA := h.StreamMux.GetEncoders(ctx)
	var curACodecName, curVCodecName codec.Name
	var curRes codec.Resolution
	if codec.IsEncoderCopy(encV) {
		curACodecName = codec.NameCopy
		curVCodecName = codec.NameCopy
	} else {
		encCtx := encV.CodecContext()
		curRes = codec.Resolution{
			Width:  uint32(encCtx.Width()),
			Height: uint32(encCtx.Height()),
		}
		curVCodecName = codec.Name(encV.Codec().Name())
		curACodecName = codec.Name(encA.Codec().Name())
	}
	logger.Tracef(ctx, "current encoder state: vCodec=%s, aCodec=%s, res=%v", curVCodecName, curACodecName, curRes)

	if outputKey == nil {
		outputKey = ptr(h.StreamMux.GetBestNotBypassOutput(ctx).GetKey())
		logger.Tracef(ctx, "using best not-bypass output key: %v", outputKey)
		origConfig := h.StreamMux.GetRecoderConfig(ctx)
		logger.Tracef(ctx, "original recoder config: %+v", origConfig)
		if len(origConfig.VideoTrackConfigs) > 0 {
			outputKey.VideoCodec = codectypes.Name(origConfig.VideoTrackConfigs[0].CodecName)
		}
		if len(origConfig.AudioTrackConfigs) > 0 {
			outputKey.AudioCodec = codectypes.Name(origConfig.AudioTrackConfigs[0].CodecName)
		}
	} else {
		if outputKey.AudioCodec == "" {
			outputKey.AudioCodec = codectypes.Name(curACodecName)
		}
		if outputKey.VideoCodec == "" {
			outputKey.VideoCodec = codectypes.Name(curVCodecName)
		}
		if outputKey.Resolution == (codec.Resolution{}) {
			outputKey.Resolution = curRes
		}
	}
	logger.Tracef(ctx, "target output key: %v", outputKey)

	outputCur := h.StreamMux.GetActiveOutput(ctx)

	isUpgrade := false
	switch outputKey.Compare(outputCur.GetKey()) {
	case 0:
		return fmt.Errorf("output is already set to %v", outputKey)
	case 1:
		isUpgrade = true
	case -1:
		isUpgrade = false
	default:
		return fmt.Errorf("unable to compare output keys: %v and %v", outputKey, outputCur.GetKey())
	}

	if isCritical {
		logger.Debugf(ctx, "critical resolution change request: cancelling any previous resolution change request")
		h.currentResolutionChangeRequest = nil
	} else {
		now := time.Now()
		prevRequest := h.currentResolutionChangeRequest
		if prevRequest == nil || prevRequest.IsUpgrade != isUpgrade || now.Sub(prevRequest.LatestAt) > time.Minute {
			h.currentResolutionChangeRequest = &resolutionChangeRequest{
				IsUpgrade: isUpgrade,
				StartedAt: now,
				LatestAt:  now,
			}
			logger.Debugf(ctx, "started resolution change request: %v (prev: %v, isUpgrade: %v)", h.currentResolutionChangeRequest, prevRequest, isUpgrade)
			return nil
		}
		prevRequest.LatestAt = now

		reqDur := now.Sub(prevRequest.StartedAt)
		switch {
		case isUpgrade && reqDur < h.ResolutionSlowdownDurationUpgrade:
			logger.Debugf(ctx, "waiting before upgrading resolution: %v < %v", reqDur, h.ResolutionSlowdownDurationUpgrade)
			return nil
		case !isUpgrade && reqDur < h.ResolutionSlowdownDurationDowngrade:
			logger.Debugf(ctx, "waiting before downgrading resolution: %v < %v", reqDur, h.ResolutionSlowdownDurationDowngrade)
			return nil
		}
	}

	if outputKey.VideoCodec == codectypes.NameCopy {
		logger.Infof(ctx, "switching to bypass mode")
		return h.StreamMux.EnableRecodingBypass(ctx)
	}

	err := h.StreamMux.setResolutionBitRateCodec(ctx, outputKey.Resolution, bitrate, outputKey.VideoCodec, outputKey.AudioCodec)
	switch {
	case err == nil:
		logger.Infof(ctx, "changed resolution to %v (bitrate: %d)", outputKey.Resolution, bitrate)
		return nil
	case errors.As(err, &ErrNotImplemented{}):
		logger.Debugf(ctx, "resolution change is not implemented: %v", err)
		return nil
	default:
		return fmt.Errorf("unable to set resolution to %v: %w", outputKey.Resolution, err)
	}
}
