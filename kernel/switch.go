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
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

// Note: Switch is a very hacky thing, try to never use it. Pipelining
// should be handled by pipeline, not by a Kernel. You may use
// condition.Switch if you need similar functionality.
type Switch[T Abstract] struct {
	*closuresignaler.ClosureSignaler

	Kernels []T

	KernelIndex     atomic.Uint32
	NextKernelIndex atomic.Int32

	KeepUnlessCond         *packetorframecondition.Condition
	VerifySwitchOutputCond *packetorframecondition.Condition
}

var _ Abstract = (*Switch[Abstract])(nil)
var _ packet.Source = (*Switch[Abstract])(nil)
var _ packet.Sink = (*Switch[Abstract])(nil)
var _ types.OriginalPacketSourcer = (*Switch[Abstract])(nil)

// OriginalPacketSource returns the currently active kernel's original packet source.
func (sw *Switch[T]) OriginalPacketSource() packet.Source {
	idx := sw.KernelIndex.Load()
	if int(idx) >= len(sw.Kernels) {
		return nil
	}
	if src, ok := any(sw.Kernels[idx]).(packet.Source); ok {
		return types.GetOriginalPacketSource(src)
	}
	return nil
}

func NewSwitch[T Abstract](
	kernels ...T,
) *Switch[T] {
	sw := &Switch[T]{
		ClosureSignaler: closuresignaler.New(),
		Kernels:         kernels,
	}
	sw.NextKernelIndex.Store(-1)
	return sw
}

func (sw *Switch[T]) GetKeepUnless() packetorframecondition.Condition {
	ptr := xatomic.LoadPointer(&sw.KeepUnlessCond)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (sw *Switch[T]) SetKeepUnless(cond packetorframecondition.Condition) {
	xatomic.StorePointer(&sw.KeepUnlessCond, ptr(cond))
}

func (sw *Switch[T]) GetVerifySwitchOutput() packetorframecondition.Condition {
	ptr := xatomic.LoadPointer(&sw.VerifySwitchOutputCond)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (sw *Switch[T]) SetVerifySwitchOutput(cond packetorframecondition.Condition) {
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

func (sw *Switch[T]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(sw)
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
	outputCh chan<- packetorframe.OutputUnion,
) error {
	errCh := make(chan error, len(sw.Kernels))

	var wg sync.WaitGroup
	for _, kernel := range sw.Kernels {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			errCh <- kernel.Generate(ctx, outputCh)
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

func (sw *Switch[T]) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	kernelIndex := sw.KernelIndex.Load()
	nextKernelIndex := sw.NextKernelIndex.Load()
	if nextKernelIndex == -1 {
		return sw.sendViaKernel(ctx, kernelIndex, input, outputCh)
	}

	commitToNextKernel := func() {
		logger.Debugf(ctx, "found a good entrance and switched to the next kernel: %d -> %d", kernelIndex, nextKernelIndex)
		sw.KernelIndex.Store(uint32(nextKernelIndex))
		sw.NextKernelIndex.CompareAndSwap(nextKernelIndex, -1)
	}

	if uint32(nextKernelIndex) == kernelIndex {
		commitToNextKernel()
		return sw.sendViaKernel(ctx, kernelIndex, input, outputCh)
	}

	keepUnless := sw.GetKeepUnless()
	if keepUnless != nil && !keepUnless.Match(ctx, input) {
		return sw.sendViaKernel(ctx, kernelIndex, input, outputCh)
	}

	verifySwitchOutputCond := sw.GetVerifySwitchOutput()
	if verifySwitchOutputCond == nil {
		commitToNextKernel()
		return sw.sendViaKernel(ctx, kernelIndex, input, outputCh)
	}

	ok, err := sw.doVerifySwitchOutput(ctx, input, outputCh, nextKernelIndex, verifySwitchOutputCond)
	if err != nil {
		return err
	}
	if ok {
		commitToNextKernel()
		return nil // we've already sent the data during doVerifySwitchOutput
	}

	return sw.sendViaKernel(ctx, kernelIndex, input, outputCh)
}

func (sw *Switch[T]) sendViaKernel(
	ctx context.Context,
	kernelIndex uint32,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return sw.Kernels[kernelIndex].SendInput(ctx, input, outputCh)
}

func (sw *Switch[T]) doVerifySwitchOutput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
	nextKernelIndex int32,
	verifySwitchOutput packetorframecondition.Condition,
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

	fakeOutputCh := make(chan packetorframe.OutputUnion, 2)

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Tracef(ctx, "the reader loop closed")
		for {
			select {
			case output, ok := <-fakeOutputCh:
				if !ok {
					return
				}
				if _ret {
					outputCh <- output
					continue
				}
				if !verifySwitchOutput.Match(
					ctx,
					output.ToInput(),
				) {
					continue
				}

				// found a match, feeding all we have left to the output
				_ret = true
				logger.Debugf(ctx, "the next kernel passed the verification")
				outputCh <- output
			case <-ctx.Done():
				return
			}
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
	sw.ClosureSignaler.Close(ctx)
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
