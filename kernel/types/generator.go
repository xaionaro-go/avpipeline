// generator.go defines the Generator interface.

package types

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type Generator interface {
	Generate(
		ctx context.Context,
		outputCh chan<- packetorframe.OutputUnion,
	) error
}
