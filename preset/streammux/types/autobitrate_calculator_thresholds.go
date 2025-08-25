package types

import (
	"context"
	"time"

	"github.com/xaionaro-go/avpipeline/logger"
)

type AutoBitrateCalculatorThresholds struct {
	OutputExtremelyHighQueueSizeDuration time.Duration
	OutputVeryHighQueueSizeDuration      time.Duration
	OutputHighQueueSizeDuration          time.Duration
	OutputLowQueueSizeDuration           time.Duration
	OutputVeryLowQueueSizeDuration       time.Duration
	IncreaseK                            float64
	DecreaseK                            float64
	QuickIncreaseK                       float64
	QuickDecreaseK                       float64
	ExtremeDecreaseK                     float64
}

var _ AutoBitRateCalculator = (*AutoBitrateCalculatorThresholds)(nil)

func DefaultAutoBitrateCalculatorThresholds() *AutoBitrateCalculatorThresholds {
	return &AutoBitrateCalculatorThresholds{
		OutputExtremelyHighQueueSizeDuration: time.Second * 30,
		OutputVeryHighQueueSizeDuration:      time.Second * 5,
		OutputHighQueueSizeDuration:          time.Second * 2,
		OutputLowQueueSizeDuration:           time.Second,
		OutputVeryLowQueueSizeDuration:       time.Second / 2,
		IncreaseK:                            1.01,
		DecreaseK:                            0.95,
		QuickIncreaseK:                       1.2,
		QuickDecreaseK:                       0.5,
		ExtremeDecreaseK:                     0.1,
	}
}

func (d *AutoBitrateCalculatorThresholds) decideFloat(
	ctx context.Context,
	queueDuration time.Duration,
) (_ret float64) {
	switch {
	case queueDuration >= d.OutputExtremelyHighQueueSizeDuration:
		return d.ExtremeDecreaseK
	case queueDuration >= d.OutputVeryHighQueueSizeDuration:
		return d.QuickDecreaseK
	case queueDuration <= d.OutputVeryLowQueueSizeDuration:
		return d.QuickIncreaseK
	case queueDuration >= d.OutputHighQueueSizeDuration:
		return d.DecreaseK
	case queueDuration <= d.OutputLowQueueSizeDuration:
		return d.IncreaseK
	}
	return 1
}

func (d *AutoBitrateCalculatorThresholds) CalculateBitRate(
	ctx context.Context,
	currentBitrateSetting uint64,
	inputBitrate uint64,
	actualOutputBitrate uint64,
	queueSize uint64,
	config *AutoBitRateConfig,
) (_ret uint64) {
	queueDuration := time.Duration(float64(queueSize) * 8 / float64(currentBitrateSetting) * float64(time.Second))
	logger.Tracef(ctx, "CalculateBitRate: %d %d %d %d %d %v", currentBitrateSetting, inputBitrate, actualOutputBitrate, queueSize, d, config)
	defer func() {
		logger.Tracef(ctx, "/CalculateBitRate: %d %d %d %d %d %v: %v", currentBitrateSetting, inputBitrate, actualOutputBitrate, queueSize, d, config, _ret)
	}()

	k := d.decideFloat(ctx, queueDuration)
	if k == 1 {
		return currentBitrateSetting
	}
	return uint64(float64(currentBitrateSetting) * k)
}
