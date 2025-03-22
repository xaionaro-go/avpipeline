package kernel

import (
	"context"
)

type SendInputer interface {
	SendInput(
		ctx context.Context,
		input InputPacket,
		outputCh chan<- OutputPacket,
	) error
}
