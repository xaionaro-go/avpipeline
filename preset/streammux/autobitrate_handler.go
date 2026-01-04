package streammux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/go-ng/xatomic"
	audio "github.com/xaionaro-go/audio/pkg/audio/types"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/net/sockinfo"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/quality"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type (
	AutoBitRateCalculator                  = types.AutoBitRateCalculator
	AutoBitRateVideoConfig                 = types.AutoBitRateVideoConfig
	AutoBitRateResolutionAndBitRateConfig  = types.AutoBitRateResolutionAndBitRateConfig
	AutoBitRateResolutionAndBitRateConfigs = types.AutoBitRateResolutionAndBitRateConfigs
	BitRateChangeRequest                   = types.BitRateChangeRequest
	CalculateBitRateRequest                = types.CalculateBitRateRequest
)

func multiplyBitRates(
	resolutions []AutoBitRateResolutionAndBitRateConfig,
	k float64,
) []AutoBitRateResolutionAndBitRateConfig {
	out := make([]AutoBitRateResolutionAndBitRateConfig, len(resolutions))
	for i := range resolutions {
		out[i] = resolutions[i]
		out[i].BitrateHigh = types.Ubps(float64(out[i].BitrateHigh) * k)
		out[i].BitrateLow = types.Ubps(float64(out[i].BitrateLow) * k)
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
				BitrateHigh: 8_000_000, BitrateLow: 3_000_000, // 6 Mbps .. 3 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 1280, Height: 720},
				BitrateHigh: 4_000_000, BitrateLow: 2_000_000, // 4 Mbps .. 2 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 960, Height: 540},
				BitrateHigh: 3_000_000, BitrateLow: 1_000_000, // 3 Mbps .. 1 Mbps
			},
			{
				Resolution:  codec.Resolution{Width: 640, Height: 360},
				BitrateHigh: 1_500_000, BitrateLow: 200_000, // 1.5 Mbps .. 200 Kbps
			},
			{
				Resolution:  codec.Resolution{Width: 320, Height: 180},
				BitrateHigh: 500_000, BitrateLow: 20_000, // 500 Kbps .. 20 Kbps
			},
		}
	case astiav.CodecIDHevc:
		return multiplyBitRates(GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDH264), 0.85)
	case astiav.CodecIDAv1:
		return multiplyBitRates(GetDefaultAutoBitrateResolutionsConfig(astiav.CodecIDH264), 0.7)
	default:
		panic(fmt.Errorf("unsupported codec for DefaultAutoBitRateVideoConfig: %s", codecID))
	}
}

type (
	AutoBitrateCalculatorThresholds        = types.AutoBitrateCalculatorThresholds
	AutoBitrateCalculatorConstantQueueSize = types.AutoBitrateCalculatorLogK
	AutoBitrateCalculatorQueueSizeGapDecay = types.AutoBitrateCalculatorQueueSizeGapDecay
	FPSReducerConfig                       = types.FPSReducerConfig
)

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

func DefaultAutoBitRateVideoConfig(
	codecID astiav.CodecID,
) AutoBitRateVideoConfig {
	resolutions := GetDefaultAutoBitrateResolutionsConfig(codecID)
	resBest := resolutions.Best()
	resWorst := resolutions.Worst()
	result := AutoBitRateVideoConfig{
		ResolutionsAndBitRates: resolutions,
		FPSReducer:             DefaultFPSReducerConfig(),
		Calculator:             DefaultAutoBitrateCalculatorQueueSizeGapDecay(),
		CheckInterval:          time.Second / 4,
		MinBitRate:             resWorst.BitrateLow,     // limiting just to avoid nonsensical values that makes automation and calculations weird
		MaxBitRate:             resBest.BitrateHigh * 2, // limiting since there is no need to consume more channel if we already provide enough bitrate
		MinFPSFraction:         0.2,

		BitRateIncreaseSlowdown:             time.Second * 7 / 8, // essentially just skip three iterations of increasing after a decrease (to dampen oscillations)
		ResolutionSlowdownDurationUpgrade:   time.Second * 15,
		ResolutionSlowdownDurationDowngrade: time.Second * 2,
	}
	return result
}

func (s *StreamMux[C]) newAutoBitRateHandler(
	ctx context.Context,
	cfg AutoBitRateVideoConfig,
) (_ret *AutoBitRateHandler[C], _err error) {
	logger.Debugf(ctx, "newAutoBitRateHandler()")
	defer func() { logger.Debugf(ctx, "/newAutoBitRateHandler(): %v", _err) }()
	if len(cfg.ResolutionsAndBitRates) == 0 {
		return nil, fmt.Errorf("at least one resolution must be specified for automatic bitrate control")
	}
	h := &AutoBitRateHandler[C]{
		AutoBitRateVideoConfig: cfg,
		StreamMux:              s,
		closureSignaler:        closuresignaler.New(),
	}
	h.resetTemporaryFPSReduction(ctx, cfg.ResolutionsAndBitRates.Best().BitrateHigh, "initialization")
	return h, nil
}

func (s *StreamMux[C]) removeAutoBitRateHandler(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "removeAutoBitRateHandler()")
	defer func() { logger.Debugf(ctx, "/removeAutoBitRateHandler(): %v", _err) }()
	return nil
}

type resolutionChangeRequest struct {
	IsUpgrade bool // otherwise is downgrade
	StartedAt time.Time
	LatestAt  time.Time
}

type fpsReductionMultiplier struct {
	UpdatedAt time.Time
	globaltypes.Rational
}

type AutoBitRateHandler[C any] struct {
	AutoBitRateVideoConfig
	StreamMux                      *StreamMux[C]
	wg                             sync.WaitGroup
	closureSignaler                *closuresignaler.ClosureSignaler
	previousQueueSize              xsync.Map[kernel.GetInternalQueueSizer, uint64]
	lastVideoBitRate               uint64
	lastTotalQueueSize             uint64
	lastCheckTS                    time.Time
	currentResolutionChangeRequest *resolutionChangeRequest
	lastBitRateDecreaseTS          time.Time

	temporaryFPSReductionMultiplier xatomic.Value[fpsReductionMultiplier]
}

func (h *AutoBitRateHandler[C]) start(ctx context.Context) (_err error) {
	h.wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer h.wg.Done()
		defer logger.Debugf(ctx, "autoBitRateHandler.ServeContext(): done")
		err := h.ServeContext(ctx)
		logger.Debugf(ctx, "autoBitRateHandler.ServeContext(): %v", err)
	})

	return nil
}

func (h *AutoBitRateHandler[C]) String() string {
	return "AutoBitRateHandler"
}

func (h *AutoBitRateHandler[C]) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close()")
	defer func() { logger.Debugf(ctx, "/Close(): %v", _err) }()
	h.closureSignaler.Close(ctx)
	h.wg.Wait()
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
	defer func() {
		errmon.ObserveRecoverCtx(ctx, recover())
	}()

	var activeVideoOutput *Output[C]
	var getRawConners []kernel.GetSyscallRawConner
	var getQueueSizers []kernel.GetInternalQueueSizer
	err := h.StreamMux.withActiveVideoOutput(ctx, func(output *Output[C]) error {
		activeVideoOutput = output
		h.StreamMux.OutputsMap.Range(func(_ SenderKey, o *Output[C]) bool {
			if o == nil {
				return true
			}
			outputProc := o.SendingNode.GetProcessor()
			getRawConner, ok := outputProc.(kernel.GetSyscallRawConner)
			if ok {
				getRawConners = append(getRawConners, getRawConner)
			} else {
				logger.Errorf(ctx, "processor %s does not implement GetSyscallRawConner", outputProc)
			}
			getQueueSizer, ok := outputProc.(kernel.GetInternalQueueSizer)
			if ok {
				getQueueSizers = append(getQueueSizers, getQueueSizer)
			} else {
				logger.Errorf(ctx, "processor %s does not implement GetInternalQueueSizer", outputProc)
			}
			return true
		})
		return nil
	})
	if err != nil {
		logger.Errorf(ctx, "unable to get active video output: %v", err)
		return
	}
	if activeVideoOutput == nil {
		logger.Warnf(ctx, "no active output; skipping bitrate check")
		return
	}

	logger.Tracef(ctx, "checking bitrate with %d raw conners and %d queue sizers", len(getRawConners), len(getQueueSizers))

	for _, proc := range getRawConners {
		logger.Tracef(ctx, "getting raw connection from %s", proc)
		err := proc.UnsafeWithRawNetworkConn(
			ctx,
			func(
				ctx context.Context,
				rawConn syscall.RawConn,
				networkName string,
			) error {
				logger.Tracef(ctx, "getting connection info from %T", rawConn)
				connInfo, err := sockinfo.GetRawConnInfo(ctx, rawConn, networkName)
				if err != nil {
					return fmt.Errorf("unable to get connection info from %T: %w", proc, err)
				}
				logger.Tracef(ctx, "connInfo: %s", spew.Sdump(connInfo))
				return nil
			})
		if err != nil {
			if errors.As(err, &kernel.ErrNotImplemented{}) {
				logger.Debugf(ctx, "unable to get raw connection from %T: %v", proc, err)
			} else {
				logger.Errorf(ctx, "unable to get raw connection from %T: %v", proc, err)
			}
		}
	}

	now := time.Now()
	tsDiff := now.Sub(h.lastCheckTS)
	h.lastCheckTS = now

	var haveAnError atomic.Bool
	var totalQueue atomic.Uint64
	assert(ctx, activeVideoOutput.SendingNode != nil)
	proc := activeVideoOutput.SendingNode.GetProcessor()
	assert(ctx, proc != nil)
	activeOutputProc, _ := proc.(kernel.GetInternalQueueSizer)
	var wg sync.WaitGroup
	for _, proc := range getQueueSizers {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			queueCheckTimeout := max(h.AutoBitRateVideoConfig.CheckInterval/4-50*time.Millisecond, 50*time.Millisecond)
			nodeReqCtx, cancelFn := context.WithTimeout(ctx, queueCheckTimeout)
			defer cancelFn()
			queueSize := proc.GetInternalQueueSize(nodeReqCtx)
			reqErr := nodeReqCtx.Err()
			if queueSize == nil && reqErr == nil {
				logger.Warnf(ctx, "unable to get queue size of %T:%s", proc, proc)
				return
			}
			var nodeTotalQueue uint64
			if reqErr != nil {
				previousQueueSize, _ := h.previousQueueSize.Load(proc)
				if proc == activeOutputProc {
					nodeTotalQueue = previousQueueSize + uint64(tsDiff.Seconds()*float64(h.lastVideoBitRate)/8.0)
					logger.Infof(ctx, "timed out on getting queue size on the active output; assuming the queue increased by %v*%d (and now is: %d)", tsDiff, h.lastVideoBitRate/8, nodeTotalQueue)
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
		h.lastVideoBitRate = h.StreamMux.getTrackMeasurements(astiav.MediaTypeVideo).OutputBitRate.Load()
	}

	encoderV, _ := h.StreamMux.GetEncoders(ctx)
	if encoderV == nil {
		logger.Warnf(ctx, "unable to get encoder")
		return
	}

	var curReqBitRate types.Ubps
	if codec.IsEncoderCopy(encoderV) {
		logger.Tracef(ctx, "encoder is in copy mode; using the input bitrate as the current bitrate")
		curReqBitRate = types.Ubps(h.StreamMux.getTrackMeasurements(astiav.MediaTypeVideo).InputBitRate.Load())
	} else {
		logger.Tracef(ctx, "getting current bitrate from the encoder")
		q := encoderV.GetQuality(ctx)
		if q, ok := q.(quality.ConstantBitrate); ok {
			curReqBitRate = types.Ubps(q)
		} else {
			logger.Debugf(ctx, "unable to get current bitrate")
		}
	}

	if curReqBitRate == 0 {
		logger.Debugf(ctx, "current bitrate is 0; skipping this check")
		return
	}

	actualOutputBitrate := types.Ubps(h.StreamMux.getTrackMeasurements(astiav.MediaTypeVideo).OutputBitRate.Load())
	totalQueueSizeDerivative := (float64(totalQueue.Load()) - float64(h.lastTotalQueueSize)) / tsDiff.Seconds()
	h.lastTotalQueueSize = totalQueue.Load()
	bitRateRequest := h.Calculator.CalculateBitRate(
		ctx,
		CalculateBitRateRequest{
			CurrentBitrateSetting: types.Ubps(curReqBitRate),
			InputBitrate:          types.Ubps(h.StreamMux.getTrackMeasurements(astiav.MediaTypeVideo).InputBitRate.Load()),
			ActualOutputBitrate:   types.Ubps(actualOutputBitrate),
			QueueSize:             types.UB(totalQueue.Load()),
			QueueSizeDerivative:   types.UBps(totalQueueSizeDerivative),
			Config:                &h.AutoBitRateVideoConfig,
		},
	)
	if bitRateRequest.BitRate == 0 {
		logger.Errorf(ctx, "calculated bitrate is 0; ignoring the calculators result")
		return
	}
	logger.Debugf(ctx, "calculated new bitrate: %v (current: %v); isCritical: %t; queue size: %d", bitRateRequest.BitRate, curReqBitRate, bitRateRequest.IsCritical, totalQueue.Load())

	if err := h.trySetVideoBitrate(ctx, curReqBitRate, bitRateRequest, actualOutputBitrate); err != nil {
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
	force bool,
	allowTrafficLoss bool,
) (_err error) {
	logger.Debugf(ctx, "enableByPass: %v, %t, %t", enable, force, allowTrafficLoss)
	defer func() {
		logger.Debugf(ctx, "/enableByPass: %v, %t, %t: %v", enable, force, allowTrafficLoss, _err)
	}()

	if !h.StreamMux.IsAllowedDifferentOutputs() {
		return fmt.Errorf("changing output is not allowed in the current MuxMode: %v", h.StreamMux.MuxMode)
	}

	if enable {
		return h.setVideoOutput(
			ctx,
			&SenderKey{
				VideoCodec: codectypes.NameCopy,
			},
			0,
			force,
			allowTrafficLoss,
		)
	}

	return h.setVideoOutput(
		ctx,
		nil,
		types.Ubps(float64(h.StreamMux.getTrackMeasurements(astiav.MediaTypeVideo).InputBitRate.Load())*0.9),
		force,
		allowTrafficLoss,
	)
}

func (h *AutoBitRateHandler[C]) trySetVideoBitrate(
	ctx context.Context,
	oldBitRate types.Ubps,
	req BitRateChangeRequest,
	_ types.Ubps,
) (_err error) {
	logger.Tracef(ctx, "trySetVideoBitrate: %v->%v (isCritical:%t)", oldBitRate, req.BitRate, req.IsCritical)
	defer func() {
		logger.Tracef(ctx, "/trySetVideoBitrate: %v->%v (isCritical:%t): %v", oldBitRate, req.BitRate, req.IsCritical, _err)
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
		case req.BitRate > oldBitRate && !h.currentResolutionChangeRequest.IsUpgrade:
			// we are increasing bitrate while a downgrade is in progress; cancel the downgrade
			logger.Tracef(ctx, "cancelling the downgrade since we are increasing bitrate: %v > %v", req.BitRate, oldBitRate)
			h.currentResolutionChangeRequest = nil
		case req.BitRate < oldBitRate && h.currentResolutionChangeRequest.IsUpgrade:
			// we are decreasing bitrate while an upgrade is in progress; cancel the upgrade
			logger.Tracef(ctx, "cancelling the upgrade since we are decreasing bitrate: %v < %v", req.BitRate, oldBitRate)
			h.currentResolutionChangeRequest = nil
		}
	}

	videoInputBitRate := types.Ubps(h.StreamMux.getTrackMeasurements(astiav.MediaTypeVideo).InputBitRate.Load())
	if h.StreamMux.AutoBitRateHandler.AutoByPass {
		if !h.StreamMux.IsAllowedDifferentOutputs() {
			logger.Errorf(ctx, "AutoByPass is enabled, but changing output is not allowed in the current MuxMode: %v", h.StreamMux.MuxMode)
		}
		byPassEnabled := h.isBypassEnabled(ctx)
		logger.Tracef(ctx, "AutoByPass is enabled: oldBitRate:%v reqBitRate:%v inputBitRate:%v; byPass:%t", types.Ubps(oldBitRate), req.BitRate, types.Ubps(videoInputBitRate), byPassEnabled)
		if byPassEnabled {
			if req.BitRate < videoInputBitRate {
				if err := h.enableBypass(ctx, false, req.IsCritical, req.IsCritical); err != nil {
					return fmt.Errorf("unable to disable bypass mode: %w", err)
				}
			}
		} else {
			if float64(req.BitRate) > float64(videoInputBitRate)*1.2 {
				err := h.enableBypass(ctx, true, req.IsCritical, false)
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

	h.updateFPSFraction(ctx, req.BitRate)

	maxVideoBitRate := h.MaxBitRate
	if videoInputBitRate > h.MinBitRate*2 && float64(videoInputBitRate)*1.5 < float64(maxVideoBitRate) {
		maxVideoBitRate = types.Ubps(1.5 * float64(videoInputBitRate))
	}

	clampedVideoBitRate := req.BitRate
	switch {
	case req.BitRate < h.MinBitRate:
		clampedVideoBitRate = h.MinBitRate
	case req.BitRate > maxVideoBitRate:
		clampedVideoBitRate = maxVideoBitRate
	}
	logger.Debugf(ctx, "clamped bitrate: %v (min:%v, max:%v, input:%v)", clampedVideoBitRate, h.MinBitRate, maxVideoBitRate, videoInputBitRate)

	allowTrafficLoss := false
	if req.BitRate < oldBitRate && req.IsCritical {
		allowTrafficLoss = true
	}

	err := h.changeResolutionIfNeeded(ctx, clampedVideoBitRate, req.IsCritical, allowTrafficLoss)
	var errNotThisTime ErrNotThisTime
	switch {
	case err == nil:
		if h.StreamMux.GetVideoOutputIDSwitchingTo(ctx) == nil {
			h.resetTemporaryFPSReduction(ctx, clampedVideoBitRate, "no output switching in progress")
		}
	case errors.As(err, &errNotThisTime):
		if errNotThisTime.BitrateBeyondThreshold < 0 {
			if err := h.temporaryReduceFPS(ctx, req.BitRate, errNotThisTime.BitrateBeyondThreshold); err != nil {
				logger.Errorf(ctx, "unable to temporary reduce FPS before resolution change: %v", err)
			}
		} else {
			if h.StreamMux.GetVideoOutputIDSwitchingTo(ctx) == nil {
				h.resetTemporaryFPSReduction(ctx, clampedVideoBitRate, "no downgrading output switching in progress")
			}
		}
	default:
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

	resCfg := h.AutoBitRateVideoConfig.ResolutionsAndBitRates.Find(*res)
	if resCfg == nil {
		return fmt.Errorf("unable to find a resolution config for the current resolution %v", *res)
	}

	if clampedVideoBitRate == oldBitRate {
		logger.Debugf(ctx, "bitrate remains unchanged after clamping: %v (resCfg: %#+v)", oldBitRate, resCfg)
		return nil
	}

	logger.Infof(ctx, "changing bitrate from %v to %v (resCfg: %#+v); max:%v, min:%v, in:%v", oldBitRate, clampedVideoBitRate, resCfg, maxVideoBitRate, h.MinBitRate, videoInputBitRate)
	if err := encoderV.SetQuality(ctx, quality.ConstantBitrate(clampedVideoBitRate), nil); err != nil {
		return fmt.Errorf("unable to set bitrate to %v: %w", clampedVideoBitRate, err)
	}

	return nil
}

func (h *AutoBitRateHandler[C]) updateFPSFraction(
	ctx context.Context,
	bitRate types.Ubps,
) {
	temporaryFPSReductionMultiplier := h.temporaryFPSReductionMultiplier.Load()
	fpsFractionReq := h.AutoBitRateVideoConfig.FPSReducer.GetFraction(bitRate)
	fpsFraction := fpsFractionReq.Mul(temporaryFPSReductionMultiplier.Rational)
	if fpsFraction.Float64() > 1.0 {
		logger.Errorf(ctx, "fpsFraction %v is greater than 1.0; clamping to 1.0: %v * %v == %v", fpsFraction, fpsFractionReq, temporaryFPSReductionMultiplier.Rational, fpsFraction)
		fpsFraction.Num = 1
		fpsFraction.Den = 1
	}
	if fpsFraction.Float64() < h.AutoBitRateVideoConfig.MinFPSFraction {
		fpsFraction = globaltypes.RationalFromApproxFloat64(h.AutoBitRateVideoConfig.MinFPSFraction)
		logger.Debugf(ctx, "clamped fpsFraction %v to minFPSFraction %v", fpsFraction, h.AutoBitRateVideoConfig.MinFPSFraction)
		if fpsFraction.Float64() > 1.0 {
			logger.Errorf(ctx, "fpsFraction %v is greater than 1.0; clamping to 1.0: %f was approximated to be %v (%f)", fpsFraction, h.AutoBitRateVideoConfig.MinFPSFraction, fpsFraction.Float64(), fpsFraction, fpsFraction.Float64())
			fpsFraction.Num = 1
			fpsFraction.Den = 1
		}
	}
	h.StreamMux.SetFPSFraction(ctx, fpsFraction)
}

func (h *AutoBitRateHandler[C]) onOutputSwitch(
	ctx context.Context,
	inputType InputType,
	from int32,
	to int32,
) {
	if inputType != InputTypeVideoOnly {
		return
	}
	logger.Debugf(ctx, "onOutputSwitch: from %v to %v", from, to)
	h.resetTemporaryFPSReduction(ctx, h.AutoBitRateVideoConfig.ResolutionsAndBitRates.Best().BitrateHigh, "output switch ended")
}

func (h *AutoBitRateHandler[C]) changeResolutionIfNeeded(
	ctx context.Context,
	bitrate types.Ubps,
	force bool,
	allowTrafficLoss bool,
) (_err error) {
	logger.Tracef(ctx, "changeResolutionIfNeeded(bitrate=%v, force=%t, allowTrafficLoss=%t)", bitrate, force, allowTrafficLoss)
	defer func() {
		logger.Tracef(ctx, "/changeResolutionIfNeeded(bitrate=%v, force=%t, allowTrafficLoss=%t): %v", bitrate, force, allowTrafficLoss, _err)
	}()

	if !h.StreamMux.IsAllowedDifferentOutputs() {
		return fmt.Errorf("changing output is not allowed in the current MuxMode: %v", h.StreamMux.MuxMode)
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

	resCfg := h.AutoBitRateVideoConfig.ResolutionsAndBitRates.Find(*res)
	if resCfg == nil {
		return fmt.Errorf("unable to find a resolution config for the current resolution %v", *res)
	}

	logger.Tracef(ctx, "current resolution: %v; resCfg: %v", *res, resCfg)

	var newRes AutoBitRateResolutionAndBitRateConfig
	var bitrateBeyondThreshold types.Ubps
	switch {
	case bitrate < resCfg.BitrateLow:
		_newRes := h.AutoBitRateVideoConfig.ResolutionsAndBitRates.BitRate(bitrate).Best()
		if _newRes == nil {
			_newRes = h.AutoBitRateVideoConfig.ResolutionsAndBitRates.Worst()
		}
		if _newRes.Resolution == *res {
			logger.Debugf(ctx, "already at the lowest resolution %v (resCfg: %v), minBitRate: %d", *res, resCfg, resCfg.BitrateLow)
			return nil
		}
		newRes = *_newRes
		bitrateBeyondThreshold = bitrate - resCfg.BitrateLow
	case bitrate > resCfg.BitrateHigh:
		_newRes := h.AutoBitRateVideoConfig.ResolutionsAndBitRates.BitRate(bitrate).Worst()
		if _newRes == nil {
			_newRes = h.AutoBitRateVideoConfig.ResolutionsAndBitRates.Best()
		}
		if _newRes.Resolution == *res {
			logger.Debugf(ctx, "already at the highest resolution %v (resCfg: %v), maxBitRate: %d", *res, resCfg, resCfg.BitrateHigh)
			return nil
		}
		newRes = *_newRes
		if newRes.Resolution == *res {
			// if already the highest resolution, then enable bypass:
			err := h.enableBypass(ctx, true, force, allowTrafficLoss)
			if err != nil {
				return fmt.Errorf("unable to enable bypass mode: %w", err)
			}
		}
		bitrateBeyondThreshold = bitrate - resCfg.BitrateHigh
	default:
		return nil
	}

	err := h.setVideoOutput(ctx, &SenderKey{
		VideoResolution: newRes.Resolution,
	}, bitrate, force, allowTrafficLoss)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotThisTime{}):
		return ErrNotThisTime{
			BitrateBeyondThreshold: bitrateBeyondThreshold,
		}
	default:
		return fmt.Errorf("unable to set new resolution %v: %w", newRes.Resolution, err)
	}
}

func (h *AutoBitRateHandler[C]) setVideoOutput(
	ctx context.Context,
	videoOutputKey *SenderKey,
	bitrate types.Ubps,
	force bool,
	allowTrafficLoss bool,
) (_err error) {
	logger.Tracef(ctx, "setVideoOutput: %v, %v (force:%t, allowTrafficLoss:%t)", videoOutputKey, bitrate, force, allowTrafficLoss)
	defer func() {
		logger.Tracef(ctx, "/setVideoOutput: %v, %v (force:%t, allowTrafficLoss:%t): %v", videoOutputKey, bitrate, force, allowTrafficLoss, _err)
	}()

	if !h.StreamMux.IsAllowedDifferentOutputs() {
		return fmt.Errorf("changing output is not allowed in the current MuxMode: %v", h.StreamMux.MuxMode)
	}

	encV, encA := h.StreamMux.GetEncoders(ctx)
	var curACodecName codec.Name
	var curASampleRate audio.SampleRate
	if encA != nil {
		curACodecName = codec.Name(encA.Codec().Name())
		curASampleRate = audio.SampleRate(encA.CodecContext().SampleRate())
	}
	var curRes codec.Resolution
	var curVCodecName codec.Name
	if codec.IsEncoderCopy(encV) {
		curVCodecName = codec.NameCopy
	} else {
		if encV != nil {
			encVCtx := encV.CodecContext()
			curRes = codec.Resolution{
				Width:  uint32(encVCtx.Width()),
				Height: uint32(encVCtx.Height()),
			}
			curVCodecName = codec.Name(encV.Codec().Name())
		}
	}
	logger.Tracef(ctx, "current encoder state: vCodec=%s, aCodec=%s, res=%v", curVCodecName, curACodecName, curRes)

	if videoOutputKey == nil {
		videoOutputKey = ptr(h.StreamMux.GetBestNotBypassOutput(ctx).GetKey())
		logger.Tracef(ctx, "using best not-bypass output key: %v", videoOutputKey)
		origConfig := h.StreamMux.GetTranscoderConfig(ctx)
		logger.Tracef(ctx, "original transcoder config: %+v", origConfig)
		if len(origConfig.Output.VideoTrackConfigs) > 0 {
			videoCfg := origConfig.Output.VideoTrackConfigs[0]
			videoOutputKey.VideoCodec = codectypes.Name(videoCfg.CodecName)
			videoOutputKey.VideoResolution = videoCfg.Resolution
		}
		if len(origConfig.Output.AudioTrackConfigs) > 0 {
			audioCfg := origConfig.Output.AudioTrackConfigs[0]
			videoOutputKey.AudioCodec = codectypes.Name(audioCfg.CodecName)
			videoOutputKey.AudioSampleRate = audioCfg.SampleRate
		}
	} else {
		if videoOutputKey.AudioCodec == "" {
			videoOutputKey.AudioCodec = codectypes.Name(curACodecName)
		}
		if videoOutputKey.AudioSampleRate == 0 {
			videoOutputKey.AudioSampleRate = curASampleRate
		}
		if videoOutputKey.VideoCodec == "" {
			videoOutputKey.VideoCodec = codectypes.Name(curVCodecName)
		}
		if videoOutputKey.VideoResolution == (codec.Resolution{}) {
			videoOutputKey.VideoResolution = curRes
		}
	}

	videoOutputCur := h.StreamMux.GetActiveVideoOutput(ctx)
	videoOutputCurKey := videoOutputCur.GetKey()
	logger.Tracef(ctx, "target output key: %v (cur: %v)", videoOutputKey, videoOutputCurKey)

	isUpgrade := false
	switch videoOutputKey.Compare(videoOutputCurKey) {
	case 0:
		return fmt.Errorf("output is already set to %v", videoOutputKey)
	case 1:
		isUpgrade = true
	case -1:
		isUpgrade = false
	default:
		return fmt.Errorf("unable to compare output keys: %v and %v", videoOutputKey, videoOutputCurKey)
	}

	if force {
		logger.Debugf(ctx, "force-resolution-change request: cancelling any previous resolution change requests")
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
			return ErrNotThisTime{}
		}
		prevRequest.LatestAt = now

		reqDur := now.Sub(prevRequest.StartedAt)
		switch {
		case isUpgrade && reqDur < h.ResolutionSlowdownDurationUpgrade:
			logger.Debugf(ctx, "waiting before upgrading resolution: %v < %v", reqDur, h.ResolutionSlowdownDurationUpgrade)
			return ErrNotThisTime{}
		case !isUpgrade && reqDur < h.ResolutionSlowdownDurationDowngrade:
			logger.Debugf(ctx, "waiting before downgrading resolution: %v < %v; meanwhile just trying to temporary reduce the FPS", reqDur, h.ResolutionSlowdownDurationDowngrade)
			return ErrNotThisTime{}
		}
		h.currentResolutionChangeRequest = nil
	}
	logger.Debugf(ctx, "proceeding with resolution change to %v", videoOutputKey.VideoResolution)

	if videoOutputKey.VideoCodec == codectypes.NameCopy {
		logger.Infof(ctx, "switching to bypass mode")
		err := h.StreamMux.EnableVideoTranscodingBypass(ctx)
		if err != nil {
			return fmt.Errorf("unable to enable video transcoding bypass: %w", err)
		}
		return nil
	}

	if allowTrafficLoss {
		err := sendingNodeSetDropOnClose(ctx, videoOutputCur.SendingNode, true)
		switch {
		case err == nil:
		case errors.As(err, &ErrNoSetDropOnClose{}):
			logger.Debugf(ctx, "current output's sending node %T does not implement SetDropOnCloser; cannot allow traffic loss during resolution change", videoOutputCur.SendingNode)
		default:
			logger.Errorf(ctx, "unable to set drop-on-close on the current output sending node: %v", err)
		}
	}
	err := h.StreamMux.setResolutionBitRateCodec(
		ctx,
		videoOutputKey.VideoResolution,
		bitrate,
		videoOutputKey.VideoCodec, videoOutputKey.AudioCodec,
	)
	switch {
	case err == nil:
		logger.Infof(ctx, "changed resolution to %v (bitrate: %s)", videoOutputKey.VideoResolution, bitrate)
		return nil
	case errors.As(err, &ErrAlreadySet{}):
		logger.Debugf(ctx, "resolution is already set to %v: %v", videoOutputKey.VideoResolution, err)
		return nil
	case errors.As(err, &ErrNotImplemented{}):
		logger.Debugf(ctx, "resolution change is not implemented: %v", err)
		return nil
	default:
		return fmt.Errorf("unable to set resolution to %v: %w", videoOutputKey.VideoResolution, err)
	}
}

type ErrNotThisTime struct {
	BitrateBeyondThreshold types.Ubps
}

func (e ErrNotThisTime) Error() string {
	if e.BitrateBeyondThreshold != 0 {
		return fmt.Sprintf("not this time; bitrate beyond threshold: %s", e.BitrateBeyondThreshold)
	}
	return "not this time"
}

// this is essentially a pre-emergency mechanism that prevents output channel congestion
// by reducing FPS temporarily until resolution change can be performed, so that the encoder
// is capable to produce lower overall bitrate.
func (h *AutoBitRateHandler[C]) temporaryReduceFPS(
	ctx context.Context,
	bitrate types.Ubps,
	bitrateBeyondThreshold types.Ubps,
) (_err error) {
	logger.Tracef(ctx, "temporaryReduceFPS: %v: ", bitrate)
	defer func() { logger.Tracef(ctx, "/temporaryReduceFPS: %v: %v", bitrate, _err) }()
	assert(ctx, bitrateBeyondThreshold < 0)
	now := time.Now()
	temporaryFPSReductionMultiplier := h.temporaryFPSReductionMultiplier.Load()
	if now.Sub(temporaryFPSReductionMultiplier.UpdatedAt) < time.Second {
		logger.Tracef(ctx, "temporary FPS reduction was updated recently at %v; skipping update", temporaryFPSReductionMultiplier.UpdatedAt)
		return nil
	}

	curBitRate := h.StreamMux.getTrackMeasurements(astiav.MediaTypeVideo).EncodedBitRate.Load()
	fpsReductionMultiplier0 := temporaryFPSReductionMultiplier.Float64() * float64(bitrate) / float64(curBitRate)
	fpsReductionMultiplier1 := float64(bitrate) / float64(bitrate-bitrateBeyondThreshold)
	fpsReductionMultiplierAvg := (fpsReductionMultiplier0 + fpsReductionMultiplier1) / 2
	fpsReductionMultiplier := globaltypes.RationalFromApproxFloat64(fpsReductionMultiplierAvg)
	logger.Infof(ctx,
		"temporaryReduceFPS: current encoded bitrate: %v; requested bitrate: %v; fpsReductionMultiplier: %v (%f) -> %v (%f): ((%f * %f / %f) + (%f / (%f + %f))) / 2",
		types.Ubps(curBitRate), bitrate, temporaryFPSReductionMultiplier.Rational, temporaryFPSReductionMultiplier.Float64(), fpsReductionMultiplier, fpsReductionMultiplier.Float64(),
		temporaryFPSReductionMultiplier.Float64(), float64(bitrate), float64(curBitRate),
		float64(bitrate), float64(bitrate), -float64(bitrateBeyondThreshold),
	)
	h.setTemporaryFPSReductionMultiplier(ctx, fpsReductionMultiplier, now)
	h.updateFPSFraction(ctx, bitrate)
	return nil
}

func (h *AutoBitRateHandler[C]) resetTemporaryFPSReduction(
	ctx context.Context,
	bitrate types.Ubps,
	reason string,
) {
	logger.Tracef(ctx, "resetTemporaryFPSReduction")
	defer func() { logger.Tracef(ctx, "/resetTemporaryFPSReduction") }()
	temporaryFPSReductionMultiplier := h.temporaryFPSReductionMultiplier.Load()
	if temporaryFPSReductionMultiplier.Num == 1 && temporaryFPSReductionMultiplier.Den == 1 {
		return
	}
	logger.Debugf(ctx, "resetting temporary FPS reduction multiplier: %v (%f) -> 1 due to: '%s'", temporaryFPSReductionMultiplier, temporaryFPSReductionMultiplier.Float64(), reason)
	h.setTemporaryFPSReductionMultiplier(ctx, globaltypes.Rational{Num: 1, Den: 1}, time.Time{})
	h.updateFPSFraction(ctx, bitrate)
}

func (h *AutoBitRateHandler[C]) setTemporaryFPSReductionMultiplier(
	ctx context.Context,
	multiplier globaltypes.Rational,
	updateAt time.Time,
) {
	logger.Tracef(ctx, "setTemporaryFPSReductionMultiplier: %v", multiplier)
	defer func() { logger.Tracef(ctx, "/setTemporaryFPSReductionMultiplier: %v", multiplier) }()
	h.temporaryFPSReductionMultiplier.Store(fpsReductionMultiplier{
		UpdatedAt: updateAt,
		Rational:  multiplier,
	})
}
