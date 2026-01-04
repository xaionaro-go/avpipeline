// passthrough.go implements a kernel that passes all inputs to the output without modification.

package kernel

import (
	"context"

	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Passthrough struct{}

var _ Abstract = (*Passthrough)(nil)

func (Passthrough) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "SendInput")
	defer func() { logger.Tracef(ctx, "/SendInput: %v", _err) }()

	output := input.CloneAsReferencedOutput()
	if output.Get() == nil {
		return kerneltypes.ErrUnexpectedInputType{}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case outputCh <- output:
	}
	return nil
}

func (p *Passthrough) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(p)
}

func (Passthrough) String() string {
	return "Passthrough"
}

func (Passthrough) Close(context.Context) error {
	return nil
}

func (Passthrough) CloseChan() <-chan struct{} {
	return nil
}

func (Passthrough) Generate(
	ctx context.Context,
	_ chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "Generate")
	defer func() { logger.Tracef(ctx, "/Generate: %v", _err) }()
	return nil
}
