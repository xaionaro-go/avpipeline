package types

import (
	"context"
	"time"

	"github.com/xaionaro-go/avpipeline/indicator"
	"github.com/xaionaro-go/avpipeline/logger"
)

type MovingAverage = indicator.MovingAverage[uint64]

// AutoBitrateCalculatorConstantQueueSize tries to keep the queue size around QueueOptimal
// and to smooth the bitrate changes. To make sure smoothing does not prevent reacting to
// drastic changes, QueueRidiculouslyLow and QueueCriticallyHigh define the thresholds
// for very quick bitrate changes.
type AutoBitrateCalculatorConstantQueueSize struct {
	QueueOptimal  time.Duration
	MovingAverage MovingAverage
}

var _ AutoBitRateCalculator = (*AutoBitrateCalculatorConstantQueueSize)(nil)

func DefaultAutoBitrateCalculatorConstantQueueSize() *AutoBitrateCalculatorConstantQueueSize {
	return &AutoBitrateCalculatorConstantQueueSize{
		QueueOptimal:  time.Second,
		MovingAverage: indicator.NewMAMA[uint64](100, 0.3, 0.05),
	}
}

func (d *AutoBitrateCalculatorConstantQueueSize) CalculateBitRate(
	ctx context.Context,
	currentBitrate uint64,
	queueSize uint64,
	config *AutoBitRateConfig,
) (_ret uint64) {
	queueDuration := time.Duration(float64(queueSize) * 8 / float64(currentBitrate) * float64(time.Second))
	logger.Tracef(ctx, "CalculateBitRate: currentBitrate=%d queueSize=%d queueDuration=%s config=%+v", currentBitrate, queueSize, queueDuration, config)
	defer func() {
		logger.Tracef(ctx, "/CalculateBitRate: currentBitrate=%d queueSize=%d queueDuration=%s config=%+v: %v", currentBitrate, queueSize, queueDuration, config, _ret)
	}()

	k := queueDuration.Seconds() / d.QueueOptimal.Seconds()
	instantBitrate := uint64(float64(currentBitrate) * k)
	smoothedBitrate := d.MovingAverage.Update(instantBitrate)
	if !d.MovingAverage.Valid() {
		return currentBitrate
	}
	return smoothedBitrate
}
