package screencapturer

import (
	"context"
	"errors"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
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

func (a *ScreenCapturer[C]) InputPacketChan() chan<- packet.Input {
	return a.Input().GetProcessor().InputPacketChan()
}

func (a *ScreenCapturer[C]) OutputPacketChan() <-chan packet.Output {
	return a.Output().GetProcessor().OutputPacketChan()
}

func (a *ScreenCapturer[C]) InputFrameChan() chan<- frame.Input {
	return a.Input().GetProcessor().InputFrameChan()
}

func (a *ScreenCapturer[C]) OutputFrameChan() <-chan frame.Output {
	return a.Output().GetProcessor().OutputFrameChan()
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
