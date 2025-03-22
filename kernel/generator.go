package kernel

import (
	"context"
)

type Generator interface {
	Generate(ctx context.Context, outputCh chan<- OutputPacket) error
}
