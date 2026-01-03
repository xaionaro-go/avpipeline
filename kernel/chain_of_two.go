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

// ChainOfTwo is a kernel that chains two kernels together.
//
// Note: ChainOfTwo is a very hacky thing, try to never use it. Pipelining
// should be handled by pipeline, not by a Kernel.
type ChainOfTwo[A, B Abstract] struct {
	*closuresignaler.ClosureSignaler
	Locker  xsync.Mutex
	Kernel0 A
	Kernel1 B
}

var (
	_ Abstract                    = (*ChainOfTwo[Abstract, Abstract])(nil)
	_ packet.Source               = (*ChainOfTwo[Abstract, Abstract])(nil)
	_ packet.Sink                 = (*ChainOfTwo[Abstract, Abstract])(nil)
	_ types.OriginalPacketSourcer = (*ChainOfTwo[Abstract, Abstract])(nil)
)

// OriginalPacketSource returns the latest kernel in the chain that is a packet source.
// It checks Kernel1 first, then Kernel0.
func (c *ChainOfTwo[A, B]) OriginalPacketSource() packet.Source {
	for _, k := range []Abstract{c.Kernel1, c.Kernel0} {
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

func NewChainOfTwo[A, B Abstract](kernel0 A, kernel1 B) *ChainOfTwo[A, B] {
	return &ChainOfTwo[A, B]{
		ClosureSignaler: closuresignaler.New(),
		Kernel0:         kernel0,
		Kernel1:         kernel1,
	}
}

func (c *ChainOfTwo[A, B]) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return xsync.DoA3R1(ctx, &c.Locker, c.sendInput, ctx, input, outputCh)
}

func (c *ChainOfTwo[A, B]) sendInput(
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
	errCh := make(chan error, 100)
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Tracef(ctx, "detaching from kernel1 inputs")
		for out := range kernel1InCh {
			err := c.Kernel1.SendInput(ctx, out.ToInput(), outputCh)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("unable to send to kernel1:%s: %w", c.Kernel1, err):
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
	outputCh chan<- packetorframe.OutputUnion,
) error {
	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		logger.Debugf(ctx, "cancelling context...")
		cancelFn()
	}()

	kernel0OutCh := make(chan packetorframe.OutputUnion, 100)
	errCh := make(chan error, 8)

	var wg sync.WaitGroup

	// Forward kernel0 outputs into kernel1.
	var fwdWG sync.WaitGroup
	fwdWG.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer fwdWG.Done()
		for out := range kernel0OutCh {
			err := c.Kernel1.SendInput(ctx, out.ToInput(), outputCh)
			if err != nil {
				errCh <- fmt.Errorf("unable to send to kernel1:%s (%v): %w", c.Kernel1, c.Kernel1.GetObjectID(), err)
				cancelFn()
				return
			}
		}
	})

	// kernel1 may generate by itself.
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		err := c.Kernel1.Generate(ctx, outputCh)
		if err != nil {
			errCh <- fmt.Errorf("unable to generate from kernel1:%s (%v): %w", c.Kernel1, c.Kernel1.GetObjectID(), err)
			cancelFn()
		}
	})

	// kernel0 generator feeds kernel1.
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
