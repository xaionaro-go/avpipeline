// processor.go implements the processor interface for the ScreenCapturer preset.

package screencapturer

import (
	"context"
	"errors"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/processor"
)

var _ processor.Abstract = (*ScreenCapturer[any])(nil)

func (a *ScreenCapturer[C]) Close(ctx context.Context) error {
	var errs []error
	if err := a.InputNode.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close InputNode.Processor: %w", err))
	}
	if err := a.DecoderNode.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close DecoderNode.Processor: %w", err))
	}
	return errors.Join(errs...)
}

func (a *ScreenCapturer[C]) InputChan() chan<- packetorframe.InputUnion {
	return a.Input().GetProcessor().InputChan()
}

func (a *ScreenCapturer[C]) OutputChan() <-chan packetorframe.OutputUnion {
	return a.Output().GetProcessor().OutputChan()
}

func (a *ScreenCapturer[C]) ErrorChan() <-chan error {
	panic("not supported")
}

func (a *ScreenCapturer[C]) Flush(ctx context.Context) error {
	return nil
}

func (a *ScreenCapturer[C]) CountersPtr() *processor.Counters {
	return a.Output().GetProcessor().CountersPtr()
}
