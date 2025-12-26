package kernel

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

// Note: ChainOfThree is a very hacky thing, try to never use it. Pipelining
// should be handled by pipeline, not by a Kernel.
type ChainOfThree[A, B, C Abstract] struct {
	*closuresignaler.ClosureSignaler
	Locker  xsync.Mutex
	Kernel0 A
	Kernel1 B
	Kernel2 C
}

var _ Abstract = (*ChainOfThree[Abstract, Abstract, Abstract])(nil)
var _ packet.Source = (*ChainOfThree[Abstract, Abstract, Abstract])(nil)
var _ packet.Sink = (*ChainOfThree[Abstract, Abstract, Abstract])(nil)
var _ types.OriginalPacketSourcer = (*ChainOfThree[Abstract, Abstract, Abstract])(nil)

// OriginalPacketSource returns the latest kernel in the chain that is a packet source.
// It checks Kernel2 first, then Kernel1, then Kernel0.
func (c *ChainOfThree[A, B, C]) OriginalPacketSource() packet.Source {
	for _, k := range []Abstract{c.Kernel2, c.Kernel1, c.Kernel0} {
		src, ok := any(k).(packet.Source)
		if !ok {
			continue
		}
		if result := types.GetOriginalPacketSource(src); result != nil {
			return result
		}
	}
	return nil
}

func NewChainOfThree[A, B, C Abstract](kernel0 A, kernel1 B, kernel2 C) *ChainOfThree[A, B, C] {
	return &ChainOfThree[A, B, C]{
		ClosureSignaler: closuresignaler.New(),
		Kernel0:         kernel0,
		Kernel1:         kernel1,
		Kernel2:         kernel2,
	}
}

func (c *ChainOfThree[A, B, C]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA4R1(ctx, &c.Locker, c.sendInputPacket, ctx, input, outputPacketsCh, outputFramesCh)
}

func (c *ChainOfThree[A, B, C]) sendInputPacket(
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

func (c *ChainOfThree[A, B, C]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA4R1(ctx, &c.Locker, c.sendInputFrame, ctx, input, outputPacketsCh, outputFramesCh)
}

func (c *ChainOfThree[A, B, C]) sendInputFrame(
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

func (c *ChainOfThree[A, B, C]) sendInput(
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
	kernel2InPacketCh, kernel2InFrameCh := make(chan packet.Output), make(chan frame.Output)
	errCh := make(chan error, 100)
	var wg sync.WaitGroup

	// Forward kernel1 outputs to kernel2
	var kernel1WG sync.WaitGroup
	kernel1WG.Add(2)
	observability.Go(ctx, func(ctx context.Context) {
		defer kernel1WG.Done()
		defer logger.Tracef(ctx, "detaching from kernel2 inputs: packets")
		for pkt := range kernel2InPacketCh {
			logger.Tracef(ctx, "sending packet to kernel2: stream:%d pkt:%p pos:%d pts:%d dts:%d dur:%d",
				pkt.StreamIndex(), pkt.Packet, pkt.Packet.Pos(), pkt.Packet.Pts(), pkt.Packet.Dts(), pkt.Packet.Duration(),
			)
			err := c.Kernel2.SendInputPacket(ctx, packet.Input(pkt), outputPacketsCh, outputFramesCh)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("unable to send packet to kernel2:%s: %w", c.Kernel2, err):
				case <-ctx.Done():
					return
				default:
				}
			}
		}
	})
	observability.Go(ctx, func(ctx context.Context) {
		defer kernel1WG.Done()
		defer logger.Tracef(ctx, "detaching from kernel2 inputs: frames")
		for f := range kernel2InFrameCh {
			logger.Tracef(ctx, "sending frame to kernel2: stream:%d frame:%p pts:%d", f.StreamIndex, f.Frame, f.Frame.Pts())
			err := c.Kernel2.SendInputFrame(ctx, frame.Input(f), outputPacketsCh, outputFramesCh)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("unable to send frame to kernel2:%s: %w", c.Kernel2, err):
				case <-ctx.Done():
					return
				default:
				}
			}
		}
	})

	// Forward kernel0 outputs to kernel1
	wg.Add(2)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Tracef(ctx, "detaching from kernel1 inputs: packets")
		for pkt := range kernel1InPacketCh {
			logger.Tracef(ctx, "sending packet to kernel1: stream:%d pkt:%p pos:%d pts:%d dts:%d dur:%d",
				pkt.StreamIndex(), pkt.Packet, pkt.Packet.Pos(), pkt.Packet.Pts(), pkt.Packet.Dts(), pkt.Packet.Duration(),
			)
			err := c.Kernel1.SendInputPacket(ctx, packet.Input(pkt), kernel2InPacketCh, kernel2InFrameCh)
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
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Tracef(ctx, "detaching from kernel1 inputs: frames")
		for f := range kernel1InFrameCh {
			logger.Tracef(ctx, "sending frame to kernel1: stream:%d frame:%p pts:%d", f.StreamIndex, f.Frame, f.Frame.Pts())
			err := c.Kernel1.SendInputFrame(ctx, frame.Input(f), kernel2InPacketCh, kernel2InFrameCh)
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
		logger.Tracef(ctx, "closing kernel2 inputs")
		close(kernel2InPacketCh)
		close(kernel2InFrameCh)
	})
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		kernel1WG.Wait()
		logger.Tracef(ctx, "closing errCh")
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

func (c *ChainOfThree[A, B, C]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(c)
}

func (c *ChainOfThree[A, B, C]) String() string {
	return fmt.Sprintf("ChainOfThree(%s -> %s -> %s)", c.Kernel0, c.Kernel1, c.Kernel2)
}

func (c *ChainOfThree[A, B, C]) Close(ctx context.Context) error {
	c.ClosureSignaler.Close(ctx)
	var result []error
	for idx, node := range []Abstract{c.Kernel0, c.Kernel1, c.Kernel2} {
		err := node.Close(ctx)
		if err != nil {
			result = append(result, fmt.Errorf("unable to close node#%d:%T: %w", idx, node, err))
		}
	}
	return errors.Join(result...)
}

func (c *ChainOfThree[A, B, C]) Generate(
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
	kernel1OutPacketCh := make(chan packet.Output)
	kernel1OutFrameCh := make(chan frame.Output)
	errCh := make(chan error, 12)

	var wg sync.WaitGroup

	// Forward kernel1 outputs into kernel2
	var fwd1WG sync.WaitGroup
	fwd1WG.Add(2)
	observability.Go(ctx, func(ctx context.Context) {
		defer fwd1WG.Done()
		for pkt := range kernel1OutPacketCh {
			err := c.Kernel2.SendInputPacket(ctx, packet.Input(pkt), outputPacketsCh, outputFramesCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send packet to kernel2:%s (%v): %w", c.Kernel2, c.Kernel2.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})
	observability.Go(ctx, func(ctx context.Context) {
		defer fwd1WG.Done()
		for f := range kernel1OutFrameCh {
			err := c.Kernel2.SendInputFrame(ctx, frame.Input(f), outputPacketsCh, outputFramesCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send frame to kernel2:%s (%v): %w", c.Kernel2, c.Kernel2.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})

	// Forward kernel0 outputs into kernel1
	var fwd0WG sync.WaitGroup
	fwd0WG.Add(2)
	observability.Go(ctx, func(ctx context.Context) {
		defer fwd0WG.Done()
		for pkt := range kernel0OutPacketCh {
			err := c.Kernel1.SendInputPacket(ctx, packet.Input(pkt), kernel1OutPacketCh, kernel1OutFrameCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send packet to kernel1:%s (%v): %w", c.Kernel1, c.Kernel1.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})
	observability.Go(ctx, func(ctx context.Context) {
		defer fwd0WG.Done()
		for f := range kernel0OutFrameCh {
			err := c.Kernel1.SendInputFrame(ctx, frame.Input(f), kernel1OutPacketCh, kernel1OutFrameCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send frame to kernel1:%s (%v): %w", c.Kernel1, c.Kernel1.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})

	// kernel2 may generate by itself
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		err := c.Kernel2.Generate(ctx, outputPacketsCh, outputFramesCh)
		if err != nil {
			errCh <- fmt.Errorf("unable to generate from kernel2:%s (%v): %w", c.Kernel2, c.Kernel2.GetObjectID(), err)
			cancelFn()
		}
	})

	// kernel1 may generate by itself (into kernel2)
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		err := c.Kernel1.Generate(ctx, kernel1OutPacketCh, kernel1OutFrameCh)
		if err != nil {
			errCh <- fmt.Errorf("unable to generate from kernel1:%s (%v): %w", c.Kernel1, c.Kernel1.GetObjectID(), err)
			cancelFn()
		}
	})

	// kernel0 generator feeds kernel1
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
		fwd0WG.Wait()
		close(kernel1OutPacketCh)
		close(kernel1OutFrameCh)
	})

	observability.Go(ctx, func(ctx context.Context) {
		fwd1WG.Wait()
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

func (c *ChainOfThree[A, B, C]) GetKernels() []Abstract {
	return []Abstract{c.Kernel0, c.Kernel1, c.Kernel2}
}

func (c *ChainOfThree[A, B, C]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	for _, k := range []Abstract{c.Kernel0, c.Kernel1, c.Kernel2} {
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

func (c *ChainOfThree[A, B, C]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	for _, k := range []Abstract{c.Kernel2, c.Kernel1, c.Kernel0} {
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

func (c *ChainOfThree[A, B, C]) NotifyAboutPacketSource(
	ctx context.Context,
	prevSource packet.Source,
) error {
	var errs []error
	for idx, k := range []Abstract{c.Kernel0, c.Kernel1, c.Kernel2} {
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
