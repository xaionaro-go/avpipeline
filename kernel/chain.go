// chain.go implements the Chain kernel for chaining multiple kernels together.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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

// Chain is a kernel that chains multiple kernels together.
//
// Note: Chain is a very hacky thing, try to never use it. Pipelining
// should be handled by pipeline, not by a Kernel.
type Chain[T Abstract] struct {
	*closuresignaler.ClosureSignaler
	Locker  xsync.Mutex
	Kernels []T
}

var (
	_ Abstract                    = (*Chain[Abstract])(nil)
	_ packet.Source               = (*Chain[Abstract])(nil)
	_ packet.Sink                 = (*Chain[Abstract])(nil)
	_ types.OriginalPacketSourcer = (*Chain[Abstract])(nil)
)

// OriginalPacketSource returns the latest kernel in the chain that is a packet source.
// It iterates backwards through the chain to find the last actual source.
func (c *Chain[T]) OriginalPacketSource() packet.Source {
	for i := len(c.Kernels) - 1; i >= 0; i-- {
		k := c.Kernels[i]
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

func NewChain[T Abstract](kernels ...T) *Chain[T] {
	return &Chain[T]{
		ClosureSignaler: closuresignaler.New(),
		Kernels:         kernels,
	}
}

func (c *Chain[T]) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return xsync.DoA3R1(ctx, &c.Locker, c.sendInput, ctx, input, outputCh)
}

func (c *Chain[T]) sendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	if len(c.Kernels) == 0 {
		return nil
	}

	if len(c.Kernels) == 1 {
		return c.Kernels[0].SendInput(ctx, input, outputCh)
	}

	errCh := make(chan error, 10)
	var wg sync.WaitGroup

	firstNextInCh := make(chan packetorframe.OutputUnion, 100)
	curInCh := firstNextInCh

	for idx := 1; idx < len(c.Kernels); idx++ {
		k := c.Kernels[idx]
		var curOutCh chan<- packetorframe.OutputUnion
		var nextCh chan packetorframe.OutputUnion
		if idx == len(c.Kernels)-1 {
			curOutCh = outputCh
		} else {
			nextCh = make(chan packetorframe.OutputUnion, 100)
			curOutCh = nextCh
		}

		wg.Add(1)
		func(curInCh <-chan packetorframe.OutputUnion, curOutCh chan<- packetorframe.OutputUnion, nextCh chan packetorframe.OutputUnion) {
			observability.Go(ctx, func(ctx context.Context) {
				defer wg.Done()
				for output := range curInCh {
					err := k.SendInput(ctx, output.ToInput(), curOutCh)
					if err != nil {
						select {
						case errCh <- err:
						case <-ctx.Done():
							return
						default:
						}
					}
				}
				if nextCh != nil {
					close(nextCh)
				}
			})
		}(curInCh, curOutCh, nextCh)

		if nextCh != nil {
			curInCh = nextCh
		}
	}

	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})

	err := c.Kernels[0].SendInput(ctx, input, firstNextInCh)
	if err != nil {
		return fmt.Errorf("unable to send to the first kernel: %w", err)
	}
	close(firstNextInCh)

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
	outputCh chan<- packetorframe.OutputUnion,
) error {
	if len(c.Kernels) == 0 {
		return nil
	}
	if len(c.Kernels) == 1 {
		return c.Kernels[0].Generate(ctx, outputCh)
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

	// Per-kernel output stages. stageCh[i] is where kernel i writes its outputs.
	stageCh := make([]chan packetorframe.OutputUnion, len(c.Kernels)-1)
	for i := 0; i < len(c.Kernels)-1; i++ {
		stageCh[i] = make(chan packetorframe.OutputUnion, 100)
	}

	for i, k := range c.Kernels {
		i, k := i, k
		var inCh <-chan packetorframe.OutputUnion
		if i > 0 {
			inCh = stageCh[i-1]
		}
		var outCh chan<- packetorframe.OutputUnion
		if i < len(c.Kernels)-1 {
			outCh = stageCh[i]
		} else {
			outCh = outputCh
		}

		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()

			var stageWG sync.WaitGroup
			if inCh != nil {
				stageWG.Add(1)
				observability.Go(ctx, func(ctx context.Context) {
					defer stageWG.Done()
					for out := range inCh {
						err := k.SendInput(ctx, out.ToInput(), outCh)
						if err != nil {
							errCh <- fmt.Errorf("unable to send to kernel #%d:%s (%v): %w", i, k, k.GetObjectID(), err)
							cancelFn()
							return
						}
					}
				})
			}

			stageWG.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
				defer stageWG.Done()
				if err := k.Generate(ctx, outCh); err != nil {
					errCh <- fmt.Errorf("unable to generate from kernel #%d:%s (%v): %w", i, k, k.GetObjectID(), err)
					cancelFn()
				}
			})

			stageWG.Wait()
			if i < len(c.Kernels)-1 {
				close(stageCh[i])
			}
		})
	}

	// Close errors after everything is done.
	observability.Go(ctx, func(ctx context.Context) {
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
