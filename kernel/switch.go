package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/observability"
)

// Note: Switch is a very hacky thing, try to never use it. Pipelining
// should be handled by pipeline, not by a Kernel. You may use
// condition.Switch if you need similar functionality.
type Switch[T Abstract] struct {
	*closeChan

	Kernels []T

	KernelIndex     atomic.Uint32
	NextKernelIndex atomic.Int32

	KeepUnlessPacketCond         *packetcondition.Condition
	VerifySwitchOutputPacketCond *packetcondition.Condition
}

var _ Abstract = (*Switch[Abstract])(nil)
var _ packet.Source = (*Switch[Abstract])(nil)
var _ packet.Sink = (*Switch[Abstract])(nil)

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

func (sw *Switch[T]) GetKeepUnless() packetcondition.Condition {
	ptr := xatomic.LoadPointer(&sw.KeepUnlessPacketCond)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (sw *Switch[T]) SetKeepUnless(cond packetcondition.Condition) {
	xatomic.StorePointer(&sw.KeepUnlessPacketCond, ptr(cond))
}

func (sw *Switch[T]) GetVerifySwitchOutput() packetcondition.Condition {
	ptr := xatomic.LoadPointer(&sw.VerifySwitchOutputPacketCond)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (sw *Switch[T]) SetVerifySwitchOutput(cond packetcondition.Condition) {
	xatomic.StorePointer(&sw.VerifySwitchOutputPacketCond, ptr(cond))
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

func (sw *Switch[T]) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	errCh := make(chan error, len(sw.Kernels))

	var wg sync.WaitGroup
	for _, kernel := range sw.Kernels {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			errCh <- kernel.Generate(ctx, outputPacketsCh, outputFramesCh)
		})
	}
	observability.Go(ctx, func(ctx context.Context) {
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

func (sw *Switch[T]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	kernelIndex := sw.KernelIndex.Load()
	nextKernelIndex := sw.NextKernelIndex.Load()
	if nextKernelIndex == -1 {
		return sw.sendViaKernel(ctx, kernelIndex, input, outputPacketsCh, outputFramesCh)
	}

	commitToNextKernel := func() {
		logger.Debugf(ctx, "found a good entrance and switched to the next kernel: %d -> %d", kernelIndex, nextKernelIndex)
		sw.KernelIndex.Store(uint32(nextKernelIndex))
		sw.NextKernelIndex.CompareAndSwap(nextKernelIndex, -1)
	}

	if uint32(nextKernelIndex) == kernelIndex {
		commitToNextKernel()
		return sw.sendViaKernel(ctx, kernelIndex, input, outputPacketsCh, outputFramesCh)
	}

	keepUnless := sw.GetKeepUnless()
	if keepUnless != nil && !keepUnless.Match(ctx, input) {
		return sw.sendViaKernel(ctx, kernelIndex, input, outputPacketsCh, outputFramesCh)
	}

	verifySwitchOutputCond := sw.GetVerifySwitchOutput()
	if verifySwitchOutputCond == nil {
		commitToNextKernel()
		return sw.sendViaKernel(ctx, kernelIndex, input, outputPacketsCh, outputFramesCh)
	}

	ok, err := sw.doVerifySwitchOutput(ctx, input, outputPacketsCh, outputFramesCh, nextKernelIndex, verifySwitchOutputCond)
	if err != nil {
		return err
	}
	if ok {
		commitToNextKernel()
		return nil // we've already sent the data during doVerifySwitchOutput
	}

	return sw.sendViaKernel(ctx, kernelIndex, input, outputPacketsCh, outputFramesCh)
}

func (sw *Switch[T]) sendViaKernel(
	ctx context.Context,
	kernelIndex uint32,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return sw.Kernels[kernelIndex].SendInputPacket(ctx, input, outputPacketsCh, outputFramesCh)
}

func (sw *Switch[T]) doVerifySwitchOutput(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
	nextKernelIndex int32,
	verifySwitchOutput packetcondition.Condition,
) (_ret bool, _err error) {
	logger.Tracef(ctx, "doVerifySwitchOutput()")
	defer func() { logger.Tracef(ctx, "/doVerifySwitchOutput(): %v %v", _ret, _err) }()

	var wg sync.WaitGroup
	defer wg.Wait()

	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		logger.Debugf(ctx, "cancelling context...")
		cancelFn()
	}()

	fakeOutputPacketsCh := make(chan packet.Output, 2)
	fakeOutputFramesCh := make(chan frame.Output, 2)

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Tracef(ctx, "the reader loop closed")
		for {
			select {
			case output, ok := <-fakeOutputPacketsCh:
				if !ok {
					if fakeOutputFramesCh == nil {
						return
					}
					fakeOutputPacketsCh = nil
					continue
				}
				if _ret {
					outputPacketsCh <- output
					continue
				}
				if !verifySwitchOutput.Match(
					ctx,
					packet.Input(output),
				) {
					continue
				}

				// found a match, feeding all we have left to the output
				_ret = true
				logger.Debugf(ctx, "the next kernel passed the verification")
				outputPacketsCh <- output
			case output, ok := <-fakeOutputFramesCh:
				if !ok {
					if fakeOutputPacketsCh == nil {
						return
					}
					fakeOutputFramesCh = nil
					continue
				}
				if _ret {
					outputFramesCh <- output
					continue
				}
			}
		}
	})

	err := sw.Kernels[nextKernelIndex].SendInputPacket(ctx, input, fakeOutputPacketsCh, fakeOutputFramesCh)
	close(fakeOutputPacketsCh)
	close(fakeOutputFramesCh)
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

func (sw *Switch[T]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return fmt.Errorf("not implemented, yet")
}

func (sw *Switch[T]) Close(
	ctx context.Context,
) error {
	sw.closeChan.Close(ctx)
	var result []error
	for idx, node := range sw.Kernels {
		err := node.Close(ctx)
		if err != nil {
			result = append(result, fmt.Errorf("unable to close node#%d:%T: %w", idx, node, err))
		}
	}
	return errors.Join(result...)
}

func (sw *Switch[T]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	hasFormatContextCount := 0
	for _, k := range sw.Kernels {
		source, ok := any(k).(packet.Source)
		if !ok {
			continue
		}
		source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			callback(fmtCtx)
			hasFormatContextCount++
		})
	}
	if hasFormatContextCount != 0 && hasFormatContextCount != len(sw.Kernels) {
		logger.Errorf(ctx, "a Switch should container either all kernels that are packet.Source-s or all kernels that are not packet.Source-s, but not a mix")
	}
}

func (sw *Switch[T]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	hasFormatContextCount := 0
	for _, k := range sw.Kernels {
		sink, ok := any(k).(packet.Sink)
		if !ok {
			continue
		}
		sink.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			callback(fmtCtx)
			hasFormatContextCount++
		})
	}
	if hasFormatContextCount != 0 && hasFormatContextCount != len(sw.Kernels) {
		logger.Errorf(ctx, "a Switch should container either all kernels that are packet.Sink-s or all kernels that are not packet.Sink-s, but not a mix")
	}
}

func (sw *Switch[T]) NotifyAboutPacketSource(
	ctx context.Context,
	prevSource packet.Source,
) error {
	var errs []error
	for idx, k := range sw.Kernels {
		sink, ok := any(k).(packet.Sink)
		if !ok {
			continue
		}
		err := sink.NotifyAboutPacketSource(ctx, prevSource)
		if err != nil {
			errs = append(errs, fmt.Errorf("got an error from #%d:%s: %w", idx, k, err))
		}
	}
	if len(errs) != 0 {
		return errors.Join(errs...)
	}
	return nil
}
