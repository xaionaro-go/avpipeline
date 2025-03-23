package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/xaionaro-go/avpipeline/condition"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

type Switch[T Abstract] struct {
	*closeChan

	Kernels []T

	KernelIndex     uint
	NextKernelIndex typing.Optional[uint]

	KeepUntil condition.Condition
	Locker    xsync.RWMutex
}

var _ Abstract = (*Switch[Abstract])(nil)

func NewSwitch[T Abstract](
	kernels ...T,
) *Switch[T] {
	return &Switch[T]{
		closeChan: newCloseChan(),
		Kernels:   kernels,
	}
}

func (sw *Switch[T]) GetKernelIndex(
	ctx context.Context,
) uint {
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &sw.Locker, func() uint {
		return sw.KernelIndex
	})
}

func (sw *Switch[T]) SetKernelIndex(
	ctx context.Context,
	idx uint,
) error {
	if idx >= uint(len(sw.Kernels)) {
		return fmt.Errorf("requested processor #%d, while I have only %d processor", idx+1, len(sw.Kernels))
	}
	sw.Locker.Do(ctx, func() {
		if sw.KeepUntil == nil {
			sw.KernelIndex = idx
			return
		}
		sw.NextKernelIndex.Set(idx)
	})
	return nil
}

func (sw *Switch[T]) GetKernel(ctx context.Context) Abstract {
	return sw.Kernels[sw.GetKernelIndex(ctx)]
}

func (sw *Switch[T]) String() string {
	var result []string
	for idx, node := range sw.Kernels {
		var str string
		if uint(idx) == sw.GetKernelIndex(context.Background()) {
			str = fmt.Sprintf("->%s", node.String())
		} else {
			str = node.String()
		}
		result = append(result, str)
	}
	return fmt.Sprintf("Switch(\n\t%s,\n)", strings.Join(result, ",\n\t"))
}

func (sw *Switch[T]) Generate(ctx context.Context, outputCh chan<- types.OutputPacket) error {
	errCh := make(chan error, len(sw.Kernels))

	var wg sync.WaitGroup
	for _, kernel := range sw.Kernels {
		wg.Add(1)
		observability.Go(ctx, func() {
			defer wg.Done()
			errCh <- kernel.Generate(ctx, outputCh)
		})
	}
	observability.Go(ctx, func() {
		wg.Wait()
		close(errCh)
	})

	var result []error
	for err := range errCh {
		if err == nil {
			continue
		}
		result = append(result, err)
	}
	return errors.Join(result...)
}

func (sw *Switch[T]) SendInput(
	ctx context.Context,
	input types.InputPacket,
	outputCh chan<- types.OutputPacket,
) error {
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &sw.Locker, func() error {
		if !sw.NextKernelIndex.IsSet() {
			return sw.Kernels[sw.KernelIndex].SendInput(ctx, input, outputCh)
		}

		if sw.KeepUntil == nil || sw.KeepUntil.Match(ctx, input) {
			sw.KernelIndex = sw.NextKernelIndex.Get()
			sw.NextKernelIndex.Unset()
		}

		return sw.Kernels[sw.KernelIndex].SendInput(ctx, input, outputCh)
	})
}

func (sw *Switch[T]) Close(
	ctx context.Context,
) error {
	sw.closeChan.Close()
	var result []error
	for idx, node := range sw.Kernels {
		err := node.Close(ctx)
		if err != nil {
			result = append(result, fmt.Errorf("unable to close node#%d:%T: %w", idx, node, err))
		}
	}
	return errors.Join(result...)
}
