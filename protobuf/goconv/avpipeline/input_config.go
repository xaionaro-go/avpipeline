package avpipeline

import (
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipelinenolibav"
)

func InputConfigFromProto(
	in *avpipelinegrpc.InputConfig,
) kerneltypes.InputConfig {
	return avpipelinenolibav.InputConfigFromProto(in)
}

func InputConfigToProto(
	in kerneltypes.InputConfig,
) *avpipelinegrpc.InputConfig {
	return avpipelinenolibav.InputConfigToProto(in)
}
