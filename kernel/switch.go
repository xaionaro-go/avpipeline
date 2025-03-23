package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/facebookincubator/go-belt/tool/logger"
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

	KeepUnlessCond         *condition.Condition
	VerifySwitchOutputCond *condition.Condition
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

func (sw *Switch[T]) GetKeepUnless() condition.Condition {
	ptr := xatomic.LoadPointer(&sw.KeepUnlessCond)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (sw *Switch[T]) SetKeepUnless(cond condition.Condition) {
	xatomic.StorePointer(&sw.KeepUnlessCond, ptr(cond))
}

func (sw *Switch[T]) GetVerifySwitchOutput() condition.Condition {
	ptr := xatomic.LoadPointer(&sw.VerifySwitchOutputCond)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (sw *Switch[T]) SetVerifySwitchOutput(cond condition.Condition) {
	xatomic.StorePointer(&sw.VerifySwitchOutputCond, ptr(cond))
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
	if sw.GetKeepUnless() == nil && sw.GetVerifySwitchOutput() == nil {
		logger.Debugf(ctx, "switched to the next kernel: %d -> %d", sw.KernelIndex.Load(), idx)
		sw.KernelIndex.Store(uint32(idx))
		return nil
	}
	logger.Debugf(ctx, "setting the next kernel: %d -> %d", sw.KernelIndex.Load(), idx)
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
	kernelIndex := sw.KernelIndex.Load()
	nextKernelIndex := sw.NextKernelIndex.Load()
	if nextKernelIndex == -1 {
		return sw.sendViaCurrentKernel(ctx, kernelIndex, input, outputCh)
	}

	commitToNextKernel := func() {
		logger.Debugf(ctx, "found a good entrance and switched to the next kernel: %d -> %d", kernelIndex, nextKernelIndex)
		sw.KernelIndex.Store(uint32(nextKernelIndex))
		sw.NextKernelIndex.CompareAndSwap(nextKernelIndex, -1)
	}

	if uint32(nextKernelIndex) == kernelIndex {
		commitToNextKernel()
		return sw.sendViaCurrentKernel(ctx, kernelIndex, input, outputCh)
	}

	keepUnless := sw.GetKeepUnless()
	if keepUnless != nil && !keepUnless.Match(ctx, input) {
		return sw.sendViaCurrentKernel(ctx, kernelIndex, input, outputCh)
	}

	verifySwitchOutputCond := sw.GetVerifySwitchOutput()
	if verifySwitchOutputCond == nil {
		commitToNextKernel()
		return sw.sendViaCurrentKernel(ctx, kernelIndex, input, outputCh)
	}

	ok, err := sw.doVerifySwitchOutput(ctx, input, outputCh, nextKernelIndex, verifySwitchOutputCond)
	if err != nil {
		return err
	}
	if ok {
		commitToNextKernel()
		return nil // we've already sent the data during doVerifySwitchOutput
	}

	return sw.sendViaCurrentKernel(ctx, kernelIndex, input, outputCh)
}

func (sw *Switch[T]) sendViaCurrentKernel(
	ctx context.Context,
	kernelIndex uint32,
	input types.InputPacket,
	outputCh chan<- types.OutputPacket,
) error {
	return sw.Kernels[kernelIndex].SendInput(ctx, input, outputCh)
}

func (sw *Switch[T]) doVerifySwitchOutput(
	ctx context.Context,
	input types.InputPacket,
	outputCh chan<- types.OutputPacket,
	nextKernelIndex int32,
	verifySwitchOutput condition.Condition,
) (_ret bool, _err error) {
	logger.Tracef(ctx, "doVerifySwitchOutput()")
	defer func() { logger.Tracef(ctx, "/doVerifySwitchOutput(): %v %v", _ret, _err) }()

	var wg sync.WaitGroup
	defer wg.Wait()

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	fakeOutputCh := make(chan types.OutputPacket, 2)

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		defer logger.Tracef(ctx, "the reader loop closed")
		for {
			output, ok := <-fakeOutputCh
			if !ok {
				return
			}
			if _ret {
				outputCh <- output
				continue
			}
			if !verifySwitchOutput.Match(
				ctx,
				types.BuildInputPacket(output.Packet, output.FormatContext, output.Stream()),
			) {
				continue
			}

			// found a match, feeding all we have left to the output
			_ret = true
			logger.Debugf(ctx, "the next kernel passed the verification")
			outputCh <- output
		}
	})

	err := sw.Kernels[nextKernelIndex].SendInput(ctx, input, fakeOutputCh)
	close(fakeOutputCh)
	if err != nil {
		if _ret {
			return true, fmt.Errorf("got an error from the next kernel: %w", err)
		}
		logger.Errorf(ctx, "got an error from the next kernel: %w", err)
		return false, nil
	}

	wg.Wait()
	return
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
