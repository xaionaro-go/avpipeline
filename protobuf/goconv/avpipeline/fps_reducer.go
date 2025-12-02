package avpipeline

import (
	smtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipelinenolibav"
)

func FPSReductionRangeFromProto(r *avpipelinegrpc.FPSReductionRange) *smtypes.FPSReductionRange {
	return avpipelinenolibav.FPSReductionRangeFromProto(r)
}

func FPSReductionRangeToProto(r *smtypes.FPSReductionRange) *avpipelinegrpc.FPSReductionRange {
	return avpipelinenolibav.FPSReductionRangeToProto(r)
}

func FPSReducerConfigFromProto(in *avpipelinegrpc.FPSReducerConfig) smtypes.FPSReducerConfig {
	return avpipelinenolibav.FPSReducerConfigFromProto(in)
}

func FPSReducerConfigToProto(in smtypes.FPSReducerConfig) *avpipelinegrpc.FPSReducerConfig {
	return avpipelinenolibav.FPSReducerConfigToProto(in)
}
