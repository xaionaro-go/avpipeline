package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/condition"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

type Switch[T Abstract] struct {
	*closeChan

	Kernels []T

	KernelIndex     atomic.Uint32
	NextKernelIndex atomic.Int32

	KeepUntil *condition.Condition
}

var _ Abstract = (*Switch[Abstract])(nil)

func NewSwitch[T Abstract](
	kernels ...T,
) *Switch[T] {
	sw := &Switch[T]{
		closeChan: newCloseChan(),
		Kernels:   kernels,
	}
	sw.NextKernelIndex.Store(-1)
	return sw
}

func (sw *Switch[T]) GetKeepUntil() condition.Condition {
	ptr := xatomic.LoadPointer(&sw.KeepUntil)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (sw *Switch[T]) SetKeepUntil(cond condition.Condition) {
	xatomic.StorePointer(&sw.KeepUntil, ptr(cond))
}

func (sw *Switch[T]) GetKernelIndex(
	ctx context.Context,
) uint {
	return uint(sw.KernelIndex.Load())
}

func (sw *Switch[T]) SetKernelIndex(
	ctx context.Context,
	idx uint,
) error {
	if idx >= uint(len(sw.Kernels)) {
		return fmt.Errorf("requested processor #%d, while I have only %d processor", idx+1, len(sw.Kernels))
	}
	if sw.GetKeepUntil() == nil {
		sw.KernelIndex.Store(uint32(idx))
		return nil
	}
	sw.NextKernelIndex.Store(int32(idx))
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
	nextKernelIndex := sw.NextKernelIndex.Load()
	if nextKernelIndex == -1 {
		return sw.Kernels[sw.KernelIndex.Load()].SendInput(ctx, input, outputCh)
	}

	keepUntil := sw.GetKeepUntil()
	if keepUntil == nil || keepUntil.Match(ctx, input) {
		sw.KernelIndex.Store(uint32(nextKernelIndex))
		sw.NextKernelIndex.Store(-1)
	}

	return sw.Kernels[sw.KernelIndex.Load()].SendInput(ctx, input, outputCh)
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
