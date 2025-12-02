package avpipeline

import (
	smtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipelinenolibav"
)

func AutoBitRateResolutionAndBitRateConfigFromProto(
	rc *avpipelinegrpc.AutoBitRateResolutionAndBitRateConfig,
) *smtypes.AutoBitRateResolutionAndBitRateConfig {
	return avpipelinenolibav.AutoBitRateResolutionAndBitRateConfigFromProto(rc)
}

func AutoBitRateResolutionAndBitRateConfigToProto(
	rc *smtypes.AutoBitRateResolutionAndBitRateConfig,
) *avpipelinegrpc.AutoBitRateResolutionAndBitRateConfig {
	return avpipelinenolibav.AutoBitRateResolutionAndBitRateConfigToProto(rc)
}

func AutoBitRateResolutionAndBitRateConfigsFromProto(
	in *avpipelinegrpc.AutoBitRateResolutionAndBitRateConfigs,
) smtypes.AutoBitRateResolutionAndBitRateConfigs {
	return avpipelinenolibav.AutoBitRateResolutionAndBitRateConfigsFromProto(in)
}

func AutoBitRateResolutionAndBitRateConfigsToProto(
	in smtypes.AutoBitRateResolutionAndBitRateConfigs,
) *avpipelinegrpc.AutoBitRateResolutionAndBitRateConfigs {
	return avpipelinenolibav.AutoBitRateResolutionAndBitRateConfigsToProto(in)
}

func AutoBitRateVideoConfigFromProto(
	cfg *avpipelinegrpc.AutoBitRateVideoConfig,
) (*smtypes.AutoBitRateVideoConfig, error) {
	return avpipelinenolibav.AutoBitRateVideoConfigFromProto(cfg)
}

func AutoBitRateVideoConfigToProto(
	cfg *smtypes.AutoBitRateVideoConfig,
) (*avpipelinegrpc.AutoBitRateVideoConfig, error) {
	return avpipelinenolibav.AutoBitRateVideoConfigToProto(cfg)
}
