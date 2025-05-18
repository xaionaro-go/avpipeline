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

var _ processor.Abstract = (*AutoFixer[struct{}])(nil)
var _ packet.Source = (*AutoFixer[struct{}])(nil)
var _ packet.Sink = (*AutoFixer[struct{}])(nil)

func (a *AutoFixer[T]) Close(ctx context.Context) error {
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

func (a *AutoFixer[T]) SendInputPacketChan() chan<- packet.Input {
	return a.Input().GetProcessor().SendInputPacketChan()
}

func (a *AutoFixer[T]) OutputPacketChan() <-chan packet.Output {
	return a.Output().GetProcessor().OutputPacketChan()
}

func (a *AutoFixer[T]) SendInputFrameChan() chan<- frame.Input {
	return a.Input().GetProcessor().SendInputFrameChan()
}

func (a *AutoFixer[T]) OutputFrameChan() <-chan frame.Output {
	return a.Output().GetProcessor().OutputFrameChan()
}

func (a *AutoFixer[T]) ErrorChan() <-chan error {
	panic("not supported")
}

func (a *AutoFixer[T]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	a.MapStreamIndicesNode.Processor.Kernel.WithOutputFormatContext(ctx, callback)
}

func (a *AutoFixer[T]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	a.MapStreamIndicesNode.Processor.Kernel.WithInputFormatContext(ctx, callback)
}

func (a *AutoFixer[T]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	return a.MapStreamIndicesNode.Processor.Kernel.NotifyAboutPacketSource(ctx, source)
}
