package types

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
)

// AutoBitrateCalculatorStatic just always returns the same bitrate.
type AutoBitrateCalculatorStatic uint64

var _ AutoBitRateCalculator = (*AutoBitrateCalculatorStatic)(nil)

func (d AutoBitrateCalculatorStatic) CalculateBitRate(
	ctx context.Context,
	currentBitrateSetting uint64,
	inputBitrate uint64,
	actualOutputBitrate uint64,
	queueSize uint64,
	config *AutoBitRateConfig,
) (_ret uint64) {
	logger.Tracef(ctx, "CalculateBitRate: %d %d %d %d %d %v", currentBitrateSetting, inputBitrate, actualOutputBitrate, queueSize, d, config)
	defer func() {
		logger.Tracef(ctx, "/CalculateBitRate: %d %d %d %d %d %v: %v", currentBitrateSetting, inputBitrate, actualOutputBitrate, queueSize, d, config, _ret)
	}()

	return uint64(d)
}
