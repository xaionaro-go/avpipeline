package avpipeline

import (
	streammuxtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	avpipeline_grpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipelinenolibav"
	"golang.org/x/exp/constraints"
)

func MovingAverageToGRPC[T constraints.Float | constraints.Integer](
	in streammuxtypes.MovingAverage[T],
) *avpipeline_grpc.MovingAverageConfig {
	return avpipelinenolibav.MovingAverageToGRPC(in)
}

func MovingAverageFromGRPC[T constraints.Integer | constraints.Float](
	in *avpipeline_grpc.MovingAverageConfig,
) streammuxtypes.MovingAverage[T] {
	return avpipelinenolibav.MovingAverageFromGRPC[T](in)
}
