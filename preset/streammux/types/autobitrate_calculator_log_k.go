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
// by looking at the gap between optimal queue size and actual.
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
	req CalculateBitRateRequest,
) (_ret BitRateChangeRequest) {
	queueDuration := time.Duration(float64(req.QueueSize) * 8 / float64(req.CurrentBitrateSetting) * float64(time.Second))
	logger.Tracef(ctx, "CalculateBitRate: %#+v", req)
	defer func() { logger.Tracef(ctx, "/CalculateBitRate: %#+v: %v", req, _ret) }()

	k := (d.QueueOptimal + queueDurationError).Seconds() / (queueDuration + queueDurationError).Seconds()
	kSmoothed := d.MovingAverage.Update(k)
	if !d.MovingAverage.Valid() {
		return BitRateChangeRequest{BitRate: req.CurrentBitrateSetting, IsCritical: false}
	}
	if math.IsNaN(kSmoothed) {
		logger.Errorf(ctx, "CalculateBitRate: kSmoothed is NaN, returning currentBitrate=%d", req.CurrentBitrateSetting)
		return BitRateChangeRequest{BitRate: req.CurrentBitrateSetting, IsCritical: false}
	}
	diff := float64(req.CurrentBitrateSetting) * math.Log(kSmoothed) * (1.0 - d.Inertia)
	newBitRate := max(int64(float64(req.CurrentBitrateSetting)+diff), 1)
	logger.Tracef(ctx, "CalculateBitRate: k=%f kSmoothed=%f diff=%f newBitRate=%d", k, kSmoothed, diff, newBitRate)
	return BitRateChangeRequest{
		BitRate:    Ubps(newBitRate),
		IsCritical: newBitRate < int64(req.ActualOutputBitrate)/2 || newBitRate < int64(req.InputBitrate)/2,
	}
}
