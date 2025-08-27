package types

import (
	"context"
	"time"

	"github.com/xaionaro-go/avpipeline/indicator"
	"github.com/xaionaro-go/avpipeline/logger"
)

// AutoBitrateCalculatorLogK tries to keep the queue size around QueueOptimal
// by forcing a bitrate to ensure a queue size derivative that decays to the
// optimal queue size.
type AutoBitrateCalculatorQueueSizeGapDecay struct {
	QueueOptimal       time.Duration
	Decay              time.Duration
	DerivativeSmoothed MovingAverage[UBps]
}

var _ AutoBitRateCalculator = (*AutoBitrateCalculatorQueueSizeGapDecay)(nil)

func DefaultAutoBitrateCalculatorQueueSizeGapDecay() *AutoBitrateCalculatorQueueSizeGapDecay {
	return &AutoBitrateCalculatorQueueSizeGapDecay{
		QueueOptimal:       time.Second,
		Decay:              time.Second,
		DerivativeSmoothed: indicator.NewMAMA[UBps](20, 0.3, 0.05),
	}
}

func (d *AutoBitrateCalculatorQueueSizeGapDecay) CalculateBitRate(
	ctx context.Context,
	req CalculateBitRateRequest,
) (_ret BitRateChangeRequest) {
	logger.Tracef(ctx, "CalculateBitRate: %#+v", req)
	defer func() {
		logger.Tracef(ctx, "/CalculateBitRate: %#+v: %v", req, _ret)
	}()

	queueDerivative := d.DerivativeSmoothed.Update(req.QueueSizeDerivative)
	if !d.DerivativeSmoothed.Valid() {
		logger.Tracef(ctx, "CalculateBitRate: not enough data for derivative smoothing")
		return BitRateChangeRequest{BitRate: req.CurrentBitrateSetting, IsCritical: false}
	}

	if req.ActualOutputBitrate < req.CurrentBitrateSetting/2 {
		logger.Tracef(ctx, "CalculateBitRate: actualOutputBitrate %s is less than half of currentBitrateSetting %s; we are so deep in a congestion that the numbers are already nonsensical for the calculations; so just setting a half of the actualOutputBitrate as the setting",
			Ubps(req.ActualOutputBitrate), Ubps(req.CurrentBitrateSetting),
		)
		return BitRateChangeRequest{BitRate: req.ActualOutputBitrate / 2, IsCritical: true}
	}

	queueDuration := US(time.Duration(
		float64(req.QueueSize) * 8 /
			float64(req.ActualOutputBitrate) *
			float64(time.Second),
	)) // s

	gap := queueDuration - US(d.QueueOptimal)            // s
	gapB := req.ActualOutputBitrate.Tob(gap).ToB()       // B
	desiredDerivative := -gapB.ToBps(US(d.Decay))        // B/s
	derivativeGap := desiredDerivative - queueDerivative // B/s
	bitRateDiff := derivativeGap.Tobps()                 // b/s
	newBitRate := max(Ubps(req.CurrentBitrateSetting)+bitRateDiff, 1)

	if newBitRate > req.CurrentBitrateSetting && req.ActualOutputBitrate < req.CurrentBitrateSetting*0.8 {
		logger.Tracef(ctx, "CalculateBitRate: we calculated an increase of bitrate, but the actual bitrate is below the current setting; which means that we are still congested; so reducing the bitrate to the actual output bitrate (* 0.9)")
		return BitRateChangeRequest{BitRate: req.ActualOutputBitrate * 0.9, IsCritical: false}
	}

	logger.Tracef(ctx, "CalculateBitRate: queueDuration=%s, gap=%s, gapB=%s, queueDerivative=%s, desiredDerivative=%s, derivativeGap=%s, bitRateDiff=%s, newBitRate=%s, currentBitRateSetting=%s, actualOutputBitrate=%s",
		queueDuration, gap, gapB, queueDerivative, desiredDerivative, derivativeGap, bitRateDiff, newBitRate, Ubps(req.CurrentBitrateSetting), Ubps(req.ActualOutputBitrate),
	)
	return BitRateChangeRequest{
		BitRate:    Ubps(newBitRate),
		IsCritical: newBitRate < max(req.ActualOutputBitrate, req.InputBitrate)/2,
	}
}
