package avpipeline

import (
	smtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipelinenolibav"
)

func AutoBitRateCalculatorFromProto(
	in *avpipelinegrpc.AutoBitrateCalculator,
) (smtypes.AutoBitRateCalculator, error) {
	return avpipelinenolibav.AutoBitRateCalculatorFromProto(in)
}

func AutoBitRateCalculatorToProto(
	in smtypes.AutoBitRateCalculator,
) (*avpipelinegrpc.AutoBitrateCalculator, error) {
	return avpipelinenolibav.AutoBitRateCalculatorToProto(in)
}
