// autobitrate_calculator_queue_size_gap_decay.go implements a bitrate calculator based on queue size gap decay.

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
	QueueDurationOptimal time.Duration
	QueueSizeMin         UB
	GapDecay             time.Duration
	InertiaIncrease      time.Duration
	InertiaDecrease      time.Duration
	DerivativeSmoothed   MovingAverage[UBps]
}

var _ AutoBitRateCalculator = (*AutoBitrateCalculatorQueueSizeGapDecay)(nil)

func DefaultAutoBitrateCalculatorQueueSizeGapDecay() *AutoBitrateCalculatorQueueSizeGapDecay {
	return &AutoBitrateCalculatorQueueSizeGapDecay{
		QueueDurationOptimal: 3 * time.Second, // pings up to 3 seconds are quite normal
		QueueSizeMin:         200_000,         // setting some minimum to avoid bottlenecking on ACK-latency instead of bandwidth
		GapDecay:             3 * time.Second,
		InertiaIncrease:      10 * time.Second,
		InertiaDecrease:      2 * time.Second,
		DerivativeSmoothed:   indicator.NewMAMA[UBps](20, 0.3, 0.05),
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
		logger.Debugf(ctx, "CalculateBitRate: not enough data for derivative smoothing")
		return BitRateChangeRequest{BitRate: req.CurrentBitrateSetting, IsCritical: false}
	}

	queueDuration := US(time.Duration(
		float64(req.QueueSize) * 8 /
			float64(req.ActualOutputBitrate) *
			float64(time.Second),
	)) // s

	queueDurationOptimal := max(
		US(d.QueueDurationOptimal),
		d.QueueSizeMin.Tob().ToS(req.ActualOutputBitrate),
	) // s

	gap := queueDuration - queueDurationOptimal                       // s
	gapB := req.ActualOutputBitrate.Tob(gap).ToB()                    // B
	desiredDerivative := -gapB.ToBps(US(d.GapDecay))                  // B/s
	derivativeGap := desiredDerivative - queueDerivative              // B/s
	bitRateDiff := derivativeGap.Tobps()                              // b/s
	newBitRate := max(Ubps(req.CurrentBitrateSetting)+bitRateDiff, 1) // b/s

	if bitRateDiff == 0 {
		return BitRateChangeRequest{BitRate: Ubps(newBitRate), IsCritical: false}
	}

	// slowdown the increase of bitrate to avoid oscillations
	if bitRateDiff > 0 {
		ratio := float64(newBitRate) / float64(max(req.CurrentBitrateSetting, 1))
		fraction := req.Config.CheckInterval.Seconds() / d.InertiaIncrease.Seconds()
		if ratio > 1+fraction {
			logger.Tracef(ctx, "CalculateBitRate: increasing bitrate too fast: ratio=%s, fraction=%s", ratio, fraction)
			newBitRate = Ubps(float64(req.CurrentBitrateSetting) * (1 + fraction))
		}
	} else {
		ratio := float64(max(req.CurrentBitrateSetting, 1)) / float64(newBitRate)
		fraction := req.Config.CheckInterval.Seconds() / d.InertiaDecrease.Seconds()
		if ratio > 1+fraction {
			logger.Tracef(ctx, "CalculateBitRate: decreasing bitrate too fast: ratio=%s, fraction=%s", ratio, fraction)
			newBitRate = Ubps(1 + float64(req.CurrentBitrateSetting)/(1+fraction))
		}
	}

	logger.Debugf(ctx, "CalculateBitRate: queueSize=%s, queueDuration=%s, queueDurationOptimal=%s, gap=%s, gapB=%s, queueDerivative=%s, desiredDerivative=%s, derivativeGap=%s, bitRateDiff=%s, newBitRate=%s, currentBitRateSetting=%s, actualOutputBitrate=%s",
		req.QueueSize, queueDuration, queueDurationOptimal, gap, gapB, queueDerivative, desiredDerivative, derivativeGap, bitRateDiff, newBitRate, Ubps(req.CurrentBitrateSetting), Ubps(req.ActualOutputBitrate),
	)
	return BitRateChangeRequest{
		BitRate:    Ubps(newBitRate),
		IsCritical: bitRateDiff < 0 && newBitRate < max(req.ActualOutputBitrate, req.InputBitrate)/5,
	}
}
