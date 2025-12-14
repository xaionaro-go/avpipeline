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
) (_err error) {
	logger.Tracef(ctx, "sendInputPacket: stream:%d pkt:%p pos:%d pts:%d dts:%d dur:%d",
		input.StreamIndex(), input.Packet, input.Packet.Pos(), input.Packet.Pts(), input.Packet.Dts(), input.Packet.Duration(),
	)
	defer func() {
		logger.Tracef(ctx, "/sendInputPacket: stream:%d pkt:%p pos:%d pts:%d dts:%d dur:%d: %v",
			input.StreamIndex(), input.Packet, input.Packet.Pos(), input.Packet.Pts(), input.Packet.Dts(), input.Packet.Duration(), _err)
	}()
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
) (_err error) {
	logger.Tracef(ctx, "sendInputFrame: stream:%d, frame:%p pts:%d", input.StreamIndex, input.Frame, input.Frame.Pts())
	defer func() {
		logger.Tracef(ctx, "/sendInputFrame: stream:%d, frame:%p pts:%d: %v", input.StreamIndex, input.Frame, input.Frame.Pts(), _err)
	}()
	return c.sendInput(ctx, nil, ptr(input), outputPacketsCh, outputFramesCh)
}

func (c *ChainOfTwo[A, B]) sendInput(
	ctx context.Context,
	inputPacket *packet.Input,
	inputFrame *frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "sendInput")
	defer func() { logger.Tracef(ctx, "/sendInput: %v", _err) }()

	if inputPacket != nil && inputFrame != nil {
		return fmt.Errorf("internal error: inputPacket != nil && inputFrame != nil")
	}
	ctx, cancelFn := context.WithCancel(ctx)

	kernel1InPacketCh, kernel1InFrameCh := make(chan packet.Output), make(chan frame.Output)
	errCh := make(chan error, 100)
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Tracef(ctx, "detaching from kernel1 inputs: packets")
		for pkt := range kernel1InPacketCh {
			logger.Tracef(ctx, "sending packet to kernel1: stream:%d pkt:%p pos:%d pts:%d dts:%d dur:%d",
				pkt.StreamIndex(), pkt.Packet, pkt.Packet.Pos(), pkt.Packet.Pts(), pkt.Packet.Dts(), pkt.Packet.Duration(),
			)
			err := c.Kernel1.SendInputPacket(ctx, packet.Input(pkt), outputPacketsCh, outputFramesCh)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("unable to send packet to kernel1:%s: %w", c.Kernel1, err):
				case <-ctx.Done():
					return
				default:
				}
			}
		}
	})
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Tracef(ctx, "detaching from kernel1 inputs: frames")
		for f := range kernel1InFrameCh {
			logger.Tracef(ctx, "sending frame to kernel1: stream:%d frame:%p pts:%d", f.StreamIndex, f.Frame, f.Frame.Pts())
			err := c.Kernel1.SendInputFrame(ctx, frame.Input(f), outputPacketsCh, outputFramesCh)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("unable to send frame to kernel1:%s: %w", c.Kernel1, err):
				case <-ctx.Done():
					return
				default:
				}
			}
		}
	})
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		logger.Tracef(ctx, "closing kernel1 outputs")
		cancelFn()
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
	logger.Tracef(ctx, "closing kernel0 outputs")
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
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		logger.Debugf(ctx, "cancelling context...")
		cancelFn()
	}()

	kernel0OutPacketCh := make(chan packet.Output)
	kernel0OutFrameCh := make(chan frame.Output)
	errCh := make(chan error, 8)

	var wg sync.WaitGroup

	// Forward kernel0 outputs into kernel1.
	var fwdWG sync.WaitGroup
	fwdWG.Add(2)
	observability.Go(ctx, func(ctx context.Context) {
		defer fwdWG.Done()
		for pkt := range kernel0OutPacketCh {
			err := c.Kernel1.SendInputPacket(ctx, packet.Input(pkt), outputPacketsCh, outputFramesCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send packet to kernel1:%s (%v): %w", c.Kernel1, c.Kernel1.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})
	observability.Go(ctx, func(ctx context.Context) {
		defer fwdWG.Done()
		for f := range kernel0OutFrameCh {
			err := c.Kernel1.SendInputFrame(ctx, frame.Input(f), outputPacketsCh, outputFramesCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send frame to kernel1:%s (%v): %w", c.Kernel1, c.Kernel1.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})

	// kernel1 may generate by itself.
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		err := c.Kernel1.Generate(ctx, outputPacketsCh, outputFramesCh)
		if err != nil {
			errCh <- fmt.Errorf("unable to generate from kernel1:%s (%v): %w", c.Kernel1, c.Kernel1.GetObjectID(), err)
			cancelFn()
		}
	})

	// kernel0 generator feeds kernel1.
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		err := c.Kernel0.Generate(ctx, kernel0OutPacketCh, kernel0OutFrameCh)
		if err != nil {
			errCh <- fmt.Errorf("unable to generate from kernel0:%s (%v): %w", c.Kernel0, c.Kernel0.GetObjectID(), err)
			cancelFn()
		}
		close(kernel0OutPacketCh)
		close(kernel0OutFrameCh)
	})

	observability.Go(ctx, func(ctx context.Context) {
		fwdWG.Wait()
		wg.Wait()
		close(errCh)
	})

	var errs []error
	for err := range errCh {
		if err == nil {
			continue
		}
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
