// send_inputer.go defines the SendInputer interface.

package types

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type SendInputer interface {
	SendInput(
		ctx context.Context,
		input packetorframe.InputUnion,
		outputCh chan<- packetorframe.OutputUnion,
	) error
}
