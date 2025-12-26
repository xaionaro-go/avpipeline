package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type HasPipelineSideDataCond struct {
	Value any
}

var _ Condition = (*HasPipelineSideDataCond)(nil)

func HasPipelineSideData(value any) HasPipelineSideDataCond {
	return HasPipelineSideDataCond{Value: value}
}

func (v HasPipelineSideDataCond) String() string {
	return fmt.Sprintf("HasPipelineSideData(%#+v)", v.Value)
}

func (v HasPipelineSideDataCond) Match(
	ctx context.Context,
	input packetorframe.InputUnion,
) (_ret bool) {
	sideData := input.GetPipelineSideData()
	logger.Tracef(ctx, "sideData: %#+v", sideData)
	return sideData.Contains(v.Value)
}
