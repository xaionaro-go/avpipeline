package autofix

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/processor"
)

var _ processor.Abstract = (*AutoFixerWithCustomData[struct{}])(nil)
var _ packet.Source = (*AutoFixerWithCustomData[struct{}])(nil)
var _ packet.Sink = (*AutoFixerWithCustomData[struct{}])(nil)
var _ types.OriginalPacketSourcer = (*AutoFixerWithCustomData[struct{}])(nil)

// OriginalPacketSource returns the underlying MapStreamIndices kernel as the original packet source.
func (a *AutoFixerWithCustomData[T]) OriginalPacketSource() packet.Source {
	return types.GetOriginalPacketSource(a.MapStreamIndicesNode.Processor.Kernel)
}

func (a *AutoFixerWithCustomData[T]) Close(ctx context.Context) error {
	if a == nil {
		return nil
	}
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

func (a *AutoFixerWithCustomData[T]) InputChan() chan<- packetorframe.InputUnion {
	return a.Input().GetProcessor().InputChan()
}

func (a *AutoFixerWithCustomData[T]) OutputChan() <-chan packetorframe.OutputUnion {
	return a.Output().GetProcessor().OutputChan()
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

func (a *AutoFixerWithCustomData[T]) Flush(ctx context.Context) error {
	return nil
}

func (a *AutoFixerWithCustomData[T]) CountersPtr() *processor.Counters {
	return a.Output().GetProcessor().CountersPtr()
}
