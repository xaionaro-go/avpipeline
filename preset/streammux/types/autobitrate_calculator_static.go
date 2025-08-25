package types

import (
	"context"
	"time"

	"github.com/xaionaro-go/avpipeline/logger"
)

// AutoBitrateCalculatorStatic just always returns the same bitrate.
type AutoBitrateCalculatorStatic uint64

var _ AutoBitRateCalculator = (*AutoBitrateCalculatorStatic)(nil)

func (d AutoBitrateCalculatorStatic) CalculateBitRate(
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

	return uint64(d)
}
