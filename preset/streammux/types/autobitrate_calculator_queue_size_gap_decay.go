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
	DerivativeSmoothed MovingAverage[float64]
}

var _ AutoBitRateCalculator = (*AutoBitrateCalculatorQueueSizeGapDecay)(nil)

func DefaultAutoBitrateCalculatorQueueSizeGapDecay() *AutoBitrateCalculatorQueueSizeGapDecay {
	return &AutoBitrateCalculatorQueueSizeGapDecay{
		QueueOptimal:       time.Second,
		Decay:              time.Second,
		DerivativeSmoothed: indicator.NewMAMA[float64](20, 0.3, 0.05),
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

	queueDuration := time.Duration(float64(req.QueueSize) * 8 / float64(req.CurrentBitrateSetting) * float64(time.Second))
	gap := queueDuration - d.QueueOptimal
	desiredDerivative := -gap.Seconds() / d.Decay.Seconds()
	derivativeGap := desiredDerivative - queueDerivative
	bitRateDiff := uint64(derivativeGap * 8)
	newBitRate := max(int64(req.CurrentBitrateSetting)+int64(bitRateDiff), 1)
	logger.Tracef(ctx, "CalculateBitRate: queueDuration=%s, gap=%s, queueDerivative=%.2f, desiredDerivative=%.2f, derivativeGap=%.2f, bitRateDiff=%d, newBitRate=%d",
		queueDuration, gap, queueDerivative, desiredDerivative, derivativeGap, bitRateDiff, newBitRate,
	)
	return BitRateChangeRequest{
		BitRate:    uint64(newBitRate),
		IsCritical: newBitRate < int64(req.ActualOutputBitrate)/2 || newBitRate < int64(req.InputBitrate)/2,
	}
}
