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
) (_ret BitRateChangeRequest) {
	logger.Tracef(ctx, "CalculateBitRate: %d %d %d %d %v", currentBitrateSetting, inputBitrate, actualOutputBitrate, queueSize, config)
	defer func() {
		logger.Tracef(ctx, "/CalculateBitRate: %d %d %d %d %v: %v", currentBitrateSetting, inputBitrate, actualOutputBitrate, queueSize, config, _ret)
	}()

	return BitRateChangeRequest{BitRate: uint64(d), IsCritical: true}
}
