package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/types"
)

type Generator interface {
	Generate(ctx context.Context, outputCh chan<- types.OutputPacket) error
}
