package avpipelinenolibav

import (
	"encoding/json"

	"github.com/xaionaro-go/avpipeline/indicator"
	streammuxtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	avpipeline_grpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	"golang.org/x/exp/constraints"
)

func MovingAverageToGRPC[T constraints.Float | constraints.Integer](
	in streammuxtypes.MovingAverage[T],
) *avpipeline_grpc.MovingAverageConfig {
	if in == nil {
		return nil
	}
	switch in := in.(type) {
	case *indicator.MAMA[T]:
		return &avpipeline_grpc.MovingAverageConfig{
			MovingAverageConfig: &avpipeline_grpc.MovingAverageConfig_Mama{
				Mama: &avpipeline_grpc.MovingAverageConfigMAMA{
					FastLimit: in.FastLimit,
					SlowLimit: in.SlowLimit,
				},
			},
		}
	default:
		b, _ := json.Marshal(in)
		return &avpipeline_grpc.MovingAverageConfig{
			MovingAverageConfig: &avpipeline_grpc.MovingAverageConfig_Other{
				Other: &avpipeline_grpc.MovingAverageConfigOther{
					JsonConfig: string(b),
				},
			},
		}
	}
}

func MovingAverageFromGRPC[T constraints.Integer | constraints.Float](
	in *avpipeline_grpc.MovingAverageConfig,
) streammuxtypes.MovingAverage[T] {
	if in == nil {
		return nil
	}
	switch cfg := in.GetMovingAverageConfig().(type) {
	case *avpipeline_grpc.MovingAverageConfig_Mama:
		return indicator.NewMAMA[T](10, cfg.Mama.GetFastLimit(), cfg.Mama.GetSlowLimit())
	default:
		return nil
	}
}
