package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/types"
)

type SendInputer interface {
	SendInput(
		ctx context.Context,
		input types.InputPacket,
		outputCh chan<- types.OutputPacket,
	) error
}
