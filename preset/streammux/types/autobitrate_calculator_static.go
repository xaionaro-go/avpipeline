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
	req CalculateBitRateRequest,
) (_ret BitRateChangeRequest) {
	logger.Tracef(ctx, "CalculateBitRate: %#+v", req)
	defer func() {
		logger.Tracef(ctx, "/CalculateBitRate: %#+v: %v", req, _ret)
	}()

	return BitRateChangeRequest{BitRate: Ubps(d), IsCritical: true}
}
