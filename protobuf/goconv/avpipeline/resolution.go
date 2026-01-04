// resolution.go provides conversion functions for resolution between Protobuf and Go.

package avpipeline

import (
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipelinenolibav"
)

func ResolutionFromProto(r *avpipelinegrpc.Resolution) *codectypes.Resolution {
	return avpipelinenolibav.ResolutionFromProto(r)
}

func ResolutionToProto(r codectypes.Resolution) *avpipelinegrpc.Resolution {
	return avpipelinenolibav.ResolutionToProto(r)
}
