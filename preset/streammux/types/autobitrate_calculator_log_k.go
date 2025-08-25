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

// AutoBitrateCalculatorLogK tries to keep the queue size around QueueOptimal
// and to smooth the bitrate changes.
type AutoBitrateCalculatorLogK struct {
	QueueOptimal  time.Duration
	Inertia       float64
	MovingAverage MovingAverage[float64]
}

var _ AutoBitRateCalculator = (*AutoBitrateCalculatorLogK)(nil)

func DefaultAutoBitrateCalculatorLogK() *AutoBitrateCalculatorLogK {
	return &AutoBitrateCalculatorLogK{
		QueueOptimal:  time.Second,
		Inertia:       0.7,
		MovingAverage: indicator.NewMAMA[float64](10, 0.3, 0.05),
	}
}

func (d *AutoBitrateCalculatorLogK) CalculateBitRate(
	ctx context.Context,
	currentBitrateSetting uint64,
	inputBitrate uint64,
	actualOutputBitrate uint64,
	queueSize uint64,
	config *AutoBitRateConfig,
) (_ret BitRateChangeRequest) {
	queueDuration := time.Duration(float64(queueSize) * 8 / float64(currentBitrateSetting) * float64(time.Second))
	logger.Tracef(ctx, "CalculateBitRate: %d %d %d %d %v", currentBitrateSetting, inputBitrate, actualOutputBitrate, queueSize, config)
	defer func() {
		logger.Tracef(ctx, "/CalculateBitRate: %d %d %d %d %v: %v", currentBitrateSetting, inputBitrate, actualOutputBitrate, queueSize, config, _ret)
	}()

	k := (d.QueueOptimal + queueDurationError).Seconds() / (queueDuration + queueDurationError).Seconds()
	kSmoothed := d.MovingAverage.Update(k)
	if !d.MovingAverage.Valid() {
		return BitRateChangeRequest{BitRate: currentBitrateSetting, IsCritical: false}
	}
	if math.IsNaN(kSmoothed) {
		logger.Errorf(ctx, "CalculateBitRate: kSmoothed is NaN, returning currentBitrate=%d", currentBitrateSetting)
		return BitRateChangeRequest{BitRate: currentBitrateSetting, IsCritical: false}
	}
	diff := float64(currentBitrateSetting) * math.Log(kSmoothed) * (1.0 - d.Inertia)
	newBitRate := max(int64(float64(currentBitrateSetting)+diff), 1)
	logger.Tracef(ctx, "CalculateBitRate: k=%f kSmoothed=%f diff=%f newBitRate=%d", k, kSmoothed, diff, newBitRate)
	return BitRateChangeRequest{
		BitRate:    uint64(newBitRate),
		IsCritical: newBitRate < int64(actualOutputBitrate)/2 || newBitRate < int64(inputBitrate)/2,
	}
}
