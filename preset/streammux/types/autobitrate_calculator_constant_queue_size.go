package types

import (
	"context"
	"math"
	"time"

	"github.com/xaionaro-go/avpipeline/indicator"
	"github.com/xaionaro-go/avpipeline/logger"
	"golang.org/x/exp/constraints"
)

type MovingAverage[T constraints.Integer | constraints.Float] = indicator.MovingAverage[T]

// AutoBitrateCalculatorConstantQueueSize tries to keep the queue size around QueueOptimal
// and to smooth the bitrate changes. To make sure smoothing does not prevent reacting to
// drastic changes, QueueRidiculouslyLow and QueueCriticallyHigh define the thresholds
// for very quick bitrate changes.
type AutoBitrateCalculatorConstantQueueSize struct {
	QueueOptimal  time.Duration
	Inertia       float64
	MovingAverage MovingAverage[float64]
}

var _ AutoBitRateCalculator = (*AutoBitrateCalculatorConstantQueueSize)(nil)

func DefaultAutoBitrateCalculatorConstantQueueSize() *AutoBitrateCalculatorConstantQueueSize {
	return &AutoBitrateCalculatorConstantQueueSize{
		QueueOptimal:  time.Second,
		Inertia:       0.7,
		MovingAverage: indicator.NewMAMA[float64](10, 0.3, 0.05),
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

	k := (d.QueueOptimal + queueDurationError).Seconds() / (queueDuration + queueDurationError).Seconds()
	kSmoothed := d.MovingAverage.Update(k)
	if !d.MovingAverage.Valid() {
		return currentBitrate
	}
	if math.IsNaN(kSmoothed) {
		logger.Errorf(ctx, "CalculateBitRate: kSmoothed is NaN, returning currentBitrate=%d", currentBitrate)
		return currentBitrate
	}
	diff := float64(currentBitrate) * math.Log(kSmoothed) * (1.0 - d.Inertia)
	newBitRate := int64(float64(currentBitrate) + diff)
	logger.Tracef(ctx, "CalculateBitRate: k=%f kSmoothed=%f diff=%f newBitRate=%d", k, kSmoothed, diff, newBitRate)
	return uint64(newBitRate)
}
