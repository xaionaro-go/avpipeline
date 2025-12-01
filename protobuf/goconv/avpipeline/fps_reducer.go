package avpipeline

import (
	smtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
)

func FPSReductionRangeFromProto(r *avpipelinegrpc.FPSReductionRange) *smtypes.FPSReductionRange {
	if r == nil {
		return nil
	}
	return &smtypes.FPSReductionRange{
		BitrateMin:  smtypes.Ubps(r.GetBitrateMinBps()),
		BitrateMax:  smtypes.Ubps(r.GetBitrateMaxBps()),
		FractionNum: r.GetFractionNum(),
		FractionDen: r.GetFractionDen(),
	}
}

func FPSReductionRangeToProto(r *smtypes.FPSReductionRange) *avpipelinegrpc.FPSReductionRange {
	if r == nil {
		return nil
	}
	return &avpipelinegrpc.FPSReductionRange{
		BitrateMinBps: uint64(r.BitrateMin),
		BitrateMaxBps: uint64(r.BitrateMax),
		FractionNum:   r.FractionNum,
		FractionDen:   r.FractionDen,
	}
}

func FPSReducerConfigFromProto(in *avpipelinegrpc.FPSReducerConfig) smtypes.FPSReducerConfig {
	if in == nil || len(in.GetRanges()) == 0 {
		return nil
	}
	out := make([]smtypes.FPSReductionRange, 0, len(in.GetRanges()))
	for _, r := range in.GetRanges() {
		if rr := FPSReductionRangeFromProto(r); rr != nil {
			out = append(out, *rr)
		}
	}
	return out
}

func FPSReducerConfigToProto(in smtypes.FPSReducerConfig) *avpipelinegrpc.FPSReducerConfig {
	if len(in) == 0 {
		return nil
	}
	ranges := make([]*avpipelinegrpc.FPSReductionRange, 0, len(in))
	for _, r := range in {
		ranges = append(ranges, FPSReductionRangeToProto(&r))
	}
	return &avpipelinegrpc.FPSReducerConfig{Ranges: ranges}
}
