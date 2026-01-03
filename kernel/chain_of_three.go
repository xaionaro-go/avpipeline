package kernel

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

// ChainOfThree is a kernel that chains three kernels together.
//
// Note: ChainOfThree is a very hacky thing, try to never use it. Pipelining
// should be handled by pipeline, not by a Kernel.
type ChainOfThree[A, B, C Abstract] struct {
	*closuresignaler.ClosureSignaler
	Locker  xsync.Mutex
	Kernel0 A
	Kernel1 B
	Kernel2 C
}

var (
	_ Abstract                    = (*ChainOfThree[Abstract, Abstract, Abstract])(nil)
	_ packet.Source               = (*ChainOfThree[Abstract, Abstract, Abstract])(nil)
	_ packet.Sink                 = (*ChainOfThree[Abstract, Abstract, Abstract])(nil)
	_ types.OriginalPacketSourcer = (*ChainOfThree[Abstract, Abstract, Abstract])(nil)
)

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

func (c *ChainOfThree[A, B, C]) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return xsync.DoA3R1(ctx, &c.Locker, c.sendInput, ctx, input, outputCh)
}

func (c *ChainOfThree[A, B, C]) sendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "sendInput: %#+v", input)
	defer func() {
		logger.Tracef(ctx, "/sendInput: %#+v: %v", input, _err)
	}()
	logger.Tracef(ctx, "sendInput")
	defer func() { logger.Tracef(ctx, "/sendInput: %v", _err) }()

	ctx, cancelFn := context.WithCancel(ctx)

	kernel1InCh := make(chan packetorframe.OutputUnion, 100)
	kernel2InCh := make(chan packetorframe.OutputUnion, 100)
	errCh := make(chan error, 100)
	var wg sync.WaitGroup

	// Forward kernel1 outputs to kernel2
	var kernel2WG sync.WaitGroup
	kernel2WG.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer kernel2WG.Done()
		defer logger.Tracef(ctx, "detaching from kernel2 inputs")
		for input := range kernel2InCh {
			err := c.Kernel2.SendInput(ctx, input.ToInput(), outputCh)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("unable to send to kernel2:%s: %w", c.Kernel2, err):
				case <-ctx.Done():
					return
				default:
				}
			}
		}
	})

	// Forward kernel0 outputs to kernel1
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Tracef(ctx, "detaching from kernel1 inputs")
		for input := range kernel1InCh {
			err := c.Kernel1.SendInput(ctx, input.ToInput(), kernel2InCh)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("unable to send to kernel1:%s: %w", c.Kernel1, err):
				case <-ctx.Done():
					return
				default:
				}
			}
		}
		close(kernel2InCh)
	})

	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		kernel2WG.Wait()
		logger.Tracef(ctx, "closing errCh")
		cancelFn()
		close(errCh)
	})

	err := c.Kernel0.SendInput(ctx, input, kernel1InCh)
	if err != nil {
		return fmt.Errorf("unable to send to the first kernel: %w", err)
	}
	logger.Tracef(ctx, "closing kernel0 outputs")
	close(kernel1InCh)

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
	outputCh chan<- packetorframe.OutputUnion,
) error {
	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		logger.Debugf(ctx, "cancelling context...")
		cancelFn()
	}()

	kernel0OutCh := make(chan packetorframe.OutputUnion, 100)
	kernel1OutCh := make(chan packetorframe.OutputUnion, 100)
	errCh := make(chan error, 12)

	var wg sync.WaitGroup

	// Forward kernel1 outputs into kernel2
	var fwd1WG sync.WaitGroup
	fwd1WG.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer fwd1WG.Done()
		for out := range kernel1OutCh {
			err := c.Kernel2.SendInput(ctx, out.ToInput(), outputCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send to kernel2:%s (%v): %w", c.Kernel2, c.Kernel2.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})

	// Forward kernel0 outputs into kernel1
	var fwd0WG sync.WaitGroup
	fwd0WG.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer fwd0WG.Done()
		for out := range kernel0OutCh {
			err := c.Kernel1.SendInput(ctx, out.ToInput(), kernel1OutCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send to kernel1:%s (%v): %w", c.Kernel1, c.Kernel1.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})

	// kernel2 may generate by itself
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		err := c.Kernel2.Generate(ctx, outputCh)
		if err != nil {
			errCh <- fmt.Errorf("unable to generate from kernel2:%s (%v): %w", c.Kernel2, c.Kernel2.GetObjectID(), err)
			cancelFn()
		}
	})

	// kernel1 may generate by itself (into kernel2)
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		err := c.Kernel1.Generate(ctx, kernel1OutCh)
		if err != nil {
			errCh <- fmt.Errorf("unable to generate from kernel1:%s (%v): %w", c.Kernel1, c.Kernel1.GetObjectID(), err)
			cancelFn()
		}
	})

	// kernel0 generator feeds kernel1
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		err := c.Kernel0.Generate(ctx, kernel0OutCh)
		if err != nil {
			errCh <- fmt.Errorf("unable to generate from kernel0:%s (%v): %w", c.Kernel0, c.Kernel0.GetObjectID(), err)
			cancelFn()
		}
		close(kernel0OutCh)
	})

	observability.Go(ctx, func(ctx context.Context) {
		fwd0WG.Wait()
		close(kernel1OutCh)
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
