package kernel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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

// Note: Chain is a very hacky thing, try to never use it. Pipelining
// should be handled by pipeline, not by a Kernel.
type Chain[T Abstract] struct {
	*closuresignaler.ClosureSignaler
	Locker  xsync.Mutex
	Kernels []T
}

var _ Abstract = (*Chain[Abstract])(nil)
var _ packet.Source = (*Chain[Abstract])(nil)
var _ packet.Sink = (*Chain[Abstract])(nil)

func NewChain[T Abstract](kernels ...T) *Chain[T] {
	return &Chain[T]{
		ClosureSignaler: closuresignaler.New(),
		Kernels:         kernels,
	}
}

func (c *Chain[T]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA4R1(ctx, &c.Locker, c.sendInputPacket, ctx, input, outputPacketsCh, outputFramesCh)
}

func (c *Chain[T]) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return c.sendInput(ctx, ptr(input), nil, outputPacketsCh, outputFramesCh)
}

func (c *Chain[T]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA4R1(ctx, &c.Locker, c.sendInputFrame, ctx, input, outputPacketsCh, outputFramesCh)
}

func (c *Chain[T]) sendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return c.sendInput(ctx, nil, ptr(input), outputPacketsCh, outputFramesCh)
}

func (c *Chain[T]) sendInput(
	ctx context.Context,
	inputPacket *packet.Input,
	inputFrame *frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	if len(c.Kernels) == 0 {
		return nil
	}
	if inputPacket != nil && inputFrame != nil {
		return fmt.Errorf("internal error: inputPacket != nil && inputFrame != nil")
	}
	var (
		firstOutPacketCh chan<- packet.Output
		firstOutFrameCh  chan<- frame.Output
		nextInPacketCh   chan packet.Output
		nextInFrameCh    chan frame.Output
	)
	if len(c.Kernels) == 1 {
		firstOutPacketCh = outputPacketsCh
		firstOutFrameCh = outputFramesCh
	} else {
		packetCh, frameCh := make(chan packet.Output), make(chan frame.Output)
		firstOutPacketCh, nextInPacketCh = packetCh, packetCh
		firstOutFrameCh, nextInFrameCh = frameCh, frameCh
	}

	errCh := make(chan error, 10)
	var wg sync.WaitGroup
	for idx, k := range c.Kernels[1:] {
		{
			k := k
			curInPacketCh, curInFrameCh := nextInPacketCh, nextInFrameCh
			nextInPacketCh, nextInFrameCh = make(chan packet.Output), make(chan frame.Output)
			var (
				curOutPacketCh chan<- packet.Output
				curOutFrameCh  chan<- frame.Output
			)
			if idx == len(c.Kernels[1:])-1 {
				curOutPacketCh, curOutFrameCh = outputPacketsCh, outputFramesCh
			} else {
				curOutPacketCh, curOutFrameCh = nextInPacketCh, nextInFrameCh
			}
			var kernelWG sync.WaitGroup
			wg.Add(1)
			kernelWG.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
				defer wg.Done()
				defer kernelWG.Done()
				for pkt := range curInPacketCh {
					err := k.SendInputPacket(ctx, packet.Input(pkt), curOutPacketCh, curOutFrameCh)
					if err != nil {
						select {
						case errCh <- err:
						case <-ctx.Done():
							return
						default:
						}
					}
				}
			})
			wg.Add(1)
			kernelWG.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
				defer wg.Done()
				defer kernelWG.Done()
				for pkt := range curInFrameCh {
					err := k.SendInputFrame(ctx, frame.Input(pkt), curOutPacketCh, curOutFrameCh)
					if err != nil {
						select {
						case errCh <- err:
						case <-ctx.Done():
							return
						default:
						}
					}
				}
			})
			if idx != len(c.Kernels[1:])-1 {
				observability.Go(ctx, func(ctx context.Context) {
					kernelWG.Wait()
					close(curOutPacketCh)
					close(curOutFrameCh)
				})
			}
		}
	}
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})

	var err error
	if inputPacket != nil {
		err = c.Kernels[0].SendInputPacket(ctx, *inputPacket, firstOutPacketCh, firstOutFrameCh)
	}
	if inputFrame != nil {
		err = c.Kernels[0].SendInputFrame(ctx, *inputFrame, firstOutPacketCh, firstOutFrameCh)
	}
	if err != nil {
		return fmt.Errorf("unable to send to the first kernel: %w", err)
	}
	close(firstOutPacketCh)
	close(firstOutFrameCh)

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

func (c *Chain[T]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(c)
}

func (c *Chain[T]) String() string {
	var result []string
	for _, node := range c.Kernels {
		result = append(result, node.String())
	}
	var sample T
	return fmt.Sprintf("Chain[%T](\n\t%s,\n)", sample, strings.Join(result, ",\n\t"))
}

func (c *Chain[T]) Close(ctx context.Context) error {
	c.ClosureSignaler.Close(ctx)
	var result []error
	for idx, node := range c.Kernels {
		err := node.Close(ctx)
		if err != nil {
			result = append(result, fmt.Errorf("unable to close node#%d:%T: %w", idx, node, err))
		}
	}
	return errors.Join(result...)
}

func (c *Chain[T]) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	if len(c.Kernels) == 0 {
		return nil
	}
	if len(c.Kernels) == 1 {
		return c.Kernels[0].Generate(ctx, outputPacketsCh, outputFramesCh)
	}

	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		logger.Debugf(ctx, "cancelling context...")
		cancelFn()
	}()

	errCh := make(chan error, len(c.Kernels)*4)
	observability.Go(ctx, func(ctx context.Context) {
		<-ctx.Done()
	})

	var wg sync.WaitGroup

	// Per-kernel output stages. stagePktCh[i]/stageFrameCh[i] is where kernel i writes its outputs.
	stagePktCh := make([]chan packet.Output, len(c.Kernels)-1)
	stageFrameCh := make([]chan frame.Output, len(c.Kernels)-1)
	for i := 0; i < len(c.Kernels)-1; i++ {
		stagePktCh[i] = make(chan packet.Output)
		stageFrameCh[i] = make(chan frame.Output)
	}

	// kernel[0].Generate -> stage0
	{
		k := c.Kernels[0]
		outPktCh := stagePktCh[0]
		outFrameCh := stageFrameCh[0]
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			if err := k.Generate(ctx, outPktCh, outFrameCh); err != nil {
				errCh <- fmt.Errorf("unable to generate from kernel #0:%s (%v): %w", k, k.GetObjectID(), err)
				cancelFn()
			}
			close(outPktCh)
			close(outFrameCh)
		})
	}

	// For each intermediate kernel i (1..last-1):
	// - forward stage(i-1) into kernel i via SendInput* producing into stage i
	// - also run kernel i.Generate producing into stage i
	// - close stage i when both are done
	for i := 1; i < len(c.Kernels)-1; i++ {
		i, k := i, c.Kernels[i]
		inPktCh := stagePktCh[i-1]
		inFrameCh := stageFrameCh[i-1]
		outPktCh := stagePktCh[i]
		outFrameCh := stageFrameCh[i]

		var stageWG sync.WaitGroup
		stageWG.Add(2)

		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			defer stageWG.Done()
			for pkt := range inPktCh {
				err := k.SendInputPacket(ctx, packet.Input(pkt), outPktCh, outFrameCh)
				if err != nil {
					errCh <- fmt.Errorf("unable to send packet to kernel #%d:%s (%v): %w", i, k, k.GetObjectID(), err)
					cancelFn()
					return
				}
			}
		})
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			defer stageWG.Done()
			for f := range inFrameCh {
				err := k.SendInputFrame(ctx, frame.Input(f), outPktCh, outFrameCh)
				if err != nil {
					errCh <- fmt.Errorf("unable to send frame to kernel #%d:%s (%v): %w", i, k, k.GetObjectID(), err)
					cancelFn()
					return
				}
			}
		})

		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			defer stageWG.Done()
			if err := k.Generate(ctx, outPktCh, outFrameCh); err != nil {
				errCh <- fmt.Errorf("unable to generate from kernel #%d:%s (%v): %w", i, k, k.GetObjectID(), err)
				cancelFn()
			}
		})

		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			stageWG.Wait()
			close(outPktCh)
			close(outFrameCh)
		})
	}

	// Last kernel: consumes stage(last-1) and may also Generate to final outputs.
	lastIdx := len(c.Kernels) - 1
	lastKernel := c.Kernels[lastIdx]
	lastInPktCh := stagePktCh[lastIdx-1]
	lastInFrameCh := stageFrameCh[lastIdx-1]
	var lastWG sync.WaitGroup
	lastWG.Add(1)

	observability.Go(ctx, func(ctx context.Context) {
		defer lastWG.Done()
		for pkt := range lastInPktCh {
			err := lastKernel.SendInputPacket(ctx, packet.Input(pkt), outputPacketsCh, outputFramesCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send packet to last kernel:%s (%v): %w", lastKernel, lastKernel.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})
	lastWG.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer lastWG.Done()
		for f := range lastInFrameCh {
			err := lastKernel.SendInputFrame(ctx, frame.Input(f), outputPacketsCh, outputFramesCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send frame to last kernel:%s (%v): %w", lastKernel, lastKernel.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})
	lastWG.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer lastWG.Done()
		if err := lastKernel.Generate(ctx, outputPacketsCh, outputFramesCh); err != nil {
			errCh <- fmt.Errorf("unable to generate from last kernel:%s (%v): %w", lastKernel, lastKernel.GetObjectID(), err)
			cancelFn()
		}
	})

	// Close errors after everything is done.
	observability.Go(ctx, func(ctx context.Context) {
		lastWG.Wait()
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
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func (c *Chain[T]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	for _, k := range c.Kernels {
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

func (c *Chain[T]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	for _, k := range slices.Backward(c.Kernels) {
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

func (c *Chain[T]) NotifyAboutPacketSource(
	ctx context.Context,
	prevSource packet.Source,
) error {
	var errs []error
	for idx, k := range c.Kernels {
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
