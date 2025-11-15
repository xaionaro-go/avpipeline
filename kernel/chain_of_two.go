package kernel

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

// Note: ChainOfTwo is a very hacky thing, try to never use it. Pipelining
// should be handled by pipeline, not by a Kernel.
type ChainOfTwo[A, B Abstract] struct {
	*closuresignaler.ClosureSignaler
	Locker  xsync.Mutex
	Kernel0 A
	Kernel1 B
}

var _ Abstract = (*ChainOfTwo[Abstract, Abstract])(nil)
var _ packet.Source = (*ChainOfTwo[Abstract, Abstract])(nil)
var _ packet.Sink = (*ChainOfTwo[Abstract, Abstract])(nil)

func NewChainOfTwo[A, B Abstract](kernel0 A, kernel1 B) *ChainOfTwo[A, B] {
	return &ChainOfTwo[A, B]{
		ClosureSignaler: closuresignaler.New(),
		Kernel0:         kernel0,
		Kernel1:         kernel1,
	}
}

func (c *ChainOfTwo[A, B]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA4R1(ctx, &c.Locker, c.sendInputPacket, ctx, input, outputPacketsCh, outputFramesCh)
}

func (c *ChainOfTwo[A, B]) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return c.sendInput(ctx, ptr(input), nil, outputPacketsCh, outputFramesCh)
}

func (c *ChainOfTwo[A, B]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA4R1(ctx, &c.Locker, c.sendInputFrame, ctx, input, outputPacketsCh, outputFramesCh)
}

func (c *ChainOfTwo[A, B]) sendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return c.sendInput(ctx, nil, ptr(input), outputPacketsCh, outputFramesCh)
}

func (c *ChainOfTwo[A, B]) sendInput(
	ctx context.Context,
	inputPacket *packet.Input,
	inputFrame *frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	if inputPacket != nil && inputFrame != nil {
		return fmt.Errorf("internal error: inputPacket != nil && inputFrame != nil")
	}

	kernel1InPacketCh, kernel1InFrameCh := make(chan packet.Output), make(chan frame.Output)
	errCh := make(chan error, 10)
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		for pkt := range kernel1InPacketCh {
			errCh <- c.Kernel1.SendInputPacket(ctx, packet.Input(pkt), outputPacketsCh, outputFramesCh)
		}
	})
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		for f := range kernel1InFrameCh {
			errCh <- c.Kernel1.SendInputFrame(ctx, frame.Input(f), outputPacketsCh, outputFramesCh)
		}
	})
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})

	var err error
	if inputPacket != nil {
		err = c.Kernel0.SendInputPacket(ctx, *inputPacket, kernel1InPacketCh, kernel1InFrameCh)
	}
	if inputFrame != nil {
		err = c.Kernel0.SendInputFrame(ctx, *inputFrame, kernel1InPacketCh, kernel1InFrameCh)
	}
	if err != nil {
		return fmt.Errorf("unable to send to the first kernel: %w", err)
	}
	close(kernel1InPacketCh)
	close(kernel1InFrameCh)

	var errs []error
	for err := range errCh {
		if err == nil {
			continue
		}
		errs = append(errs, err)
	}

	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (c *ChainOfTwo[A, B]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(c)
}

func (c *ChainOfTwo[A, B]) String() string {
	return fmt.Sprintf("ChainOfTwo(%s -> %s)", c.Kernel0, c.Kernel1)
}

func (c *ChainOfTwo[A, B]) Close(ctx context.Context) error {
	c.ClosureSignaler.Close(ctx)
	var result []error
	for idx, node := range []Abstract{c.Kernel0, c.Kernel1} {
		err := node.Close(ctx)
		if err != nil {
			result = append(result, fmt.Errorf("unable to close node#%d:%T: %w", idx, node, err))
		}
	}
	return errors.Join(result...)
}

func (c *ChainOfTwo[A, B]) Generate(
	ctx context.Context,
	_ chan<- packet.Output,
	_ chan<- frame.Output,
) error {
	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		logger.Debugf(ctx, "cancelling context...")
		cancelFn()
	}()
	outputPacketsCh := make(chan packet.Output, 1)
	outputFramesCh := make(chan frame.Output, 1)

	errCh := make(chan error, 1)

	var kernelWG sync.WaitGroup
	for _, k := range []Abstract{c.Kernel0, c.Kernel1} {
		kernelWG.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer kernelWG.Done()
			errCh <- k.Generate(ctx, outputPacketsCh, outputFramesCh)
		})
	}
	observability.Go(ctx, func(ctx context.Context) {
		kernelWG.Wait()
		close(outputPacketsCh)
		close(outputFramesCh)
	})

	var readerWG sync.WaitGroup
	readerWG.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer readerWG.Done()
		for range outputPacketsCh {
			errCh <- fmt.Errorf("generators are not supported in ChainOfTwo, yet")
		}
	})
	readerWG.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer readerWG.Done()
		for range outputFramesCh {
			errCh <- fmt.Errorf("generators are not supported in ChainOfTwo, yet")
		}
	})
	observability.Go(ctx, func(ctx context.Context) {
		readerWG.Wait()
		close(errCh)
	})

	var errs []error
	for err := range errCh {
		if err == nil {
			continue
		}
		cancelFn()
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

type GetKernelser interface {
	GetKernels() []Abstract
}

func (c *ChainOfTwo[A, B]) GetKernels() []Abstract {
	return []Abstract{c.Kernel0, c.Kernel1}
}

func (c *ChainOfTwo[A, B]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	for _, k := range []Abstract{c.Kernel0, c.Kernel1} {
		sink, ok := any(k).(packet.Sink)
		if !ok {
			continue
		}

		hasFormatContext := false
		sink.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			callback(fmtCtx)
			hasFormatContext = true
		})
		if hasFormatContext {
			return
		}
	}
}

func (c *ChainOfTwo[A, B]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	for _, k := range []Abstract{c.Kernel1, c.Kernel0} {
		source, ok := any(k).(packet.Source)
		if !ok {
			continue
		}

		hasFormatContext := false
		source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			callback(fmtCtx)
			hasFormatContext = true
		})
		if hasFormatContext {
			return
		}
	}
}

func (c *ChainOfTwo[A, B]) NotifyAboutPacketSource(
	ctx context.Context,
	prevSource packet.Source,
) error {
	var errs []error
	for idx, k := range []Abstract{c.Kernel0, c.Kernel1} {
		sink, ok := any(k).(packet.Sink)
		if ok {
			err := sink.NotifyAboutPacketSource(ctx, prevSource)
			if err != nil {
				errs = append(errs, fmt.Errorf("got an error from #%d:%s: %w", idx, k, err))
			}
		}
		source, ok := any(k).(packet.Source)
		if !ok {
			continue
		}
		hasFormatContext := false
		source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			hasFormatContext = true
		})
		if hasFormatContext {
			prevSource = source
		}
	}
	if len(errs) != 0 {
		return errors.Join(errs...)
	}
	return nil
}
