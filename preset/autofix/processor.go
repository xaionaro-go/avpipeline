package autofix

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
)

var _ processor.Abstract = (*AutoFixerWithCustomData[struct{}])(nil)
var _ packet.Source = (*AutoFixerWithCustomData[struct{}])(nil)
var _ packet.Sink = (*AutoFixerWithCustomData[struct{}])(nil)

func (a *AutoFixerWithCustomData[T]) Close(ctx context.Context) error {
	var errs []error
	if a.AutoHeadersNode != nil {
		if err := a.AutoHeadersNode.Processor.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close AutoHeadersNode.Processor: %w", err))
		}
	}
	if err := a.MapStreamIndicesNode.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close MapStreamIndicesNode.Processor: %w", err))
	}
	return errors.Join(errs...)
}

func (a *AutoFixerWithCustomData[T]) SendInputPacketChan() chan<- packet.Input {
	return a.Input().GetProcessor().SendInputPacketChan()
}

func (a *AutoFixerWithCustomData[T]) OutputPacketChan() <-chan packet.Output {
	return a.Output().GetProcessor().OutputPacketChan()
}

func (a *AutoFixerWithCustomData[T]) SendInputFrameChan() chan<- frame.Input {
	return a.Input().GetProcessor().SendInputFrameChan()
}

func (a *AutoFixerWithCustomData[T]) OutputFrameChan() <-chan frame.Output {
	return a.Output().GetProcessor().OutputFrameChan()
}

func (a *AutoFixerWithCustomData[T]) ErrorChan() <-chan error {
	panic("not supported")
}

func (a *AutoFixerWithCustomData[T]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	a.MapStreamIndicesNode.Processor.Kernel.WithOutputFormatContext(ctx, callback)
}

func (a *AutoFixerWithCustomData[T]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	a.MapStreamIndicesNode.Processor.Kernel.WithInputFormatContext(ctx, callback)
}

func (a *AutoFixerWithCustomData[T]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	return a.MapStreamIndicesNode.Processor.Kernel.NotifyAboutPacketSource(ctx, source)
}
