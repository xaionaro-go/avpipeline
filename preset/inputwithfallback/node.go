// node.go implements the node interface for the InputWithFallback preset.

package inputwithfallback

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/node"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

var _ node.Abstract = (*InputWithFallback[InputKernel, codec.DecoderFactory, any])(nil)

func (i *InputWithFallback[K, DF, C]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	if !i.isServing.CompareAndSwap(false, true) {
		errCh <- node.Error{
			Node: i,
			Err:  node.ErrAlreadyStarted{},
		}
		return
	}
	defer i.isServing.Store(false)

	logger.Debugf(ctx, "inputwithfallback.Serve: started")
	defer logger.Debugf(ctx, "inputwithfallback.Serve: ended")

	if debugConsistencyCheckLoop {
		observability.Go(ctx, func(ctx context.Context) {
			t := time.NewTicker(10 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
				i.InputChainsLocker.Do(ctx, func() {
					for inputID := InputID(0); inputID < InputID(len(i.InputChains)); inputID++ {
						inputChain := i.InputChains[inputID]
						isPaused := inputChain.IsPaused(ctx)
						expectedIsPaused := (inputID > InputID(i.InputSwitch.CurrentValue.Load()))
						if isPaused != expectedIsPaused {
							logger.Errorf(ctx, "inputwithfallback.Serve: inconsistency detected: input chain %d paused=%v but should be %v", inputID, isPaused, expectedIsPaused)
						}
					}
				})
			}
		})
	}

	i.serveWaitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer i.serveWaitGroup.Done()
		logger.Debugf(ctx, "inputwithfallback.Serve: pre-output node serving started")
		i.PreOutput.Serve(ctx, cfg, errCh)
	})

	i.serveWaitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer i.serveWaitGroup.Done()
		logger.Debugf(ctx, "inputwithfallback.Serve: output node serving started")
		i.Output.Serve(ctx, cfg, errCh)
	})

	i.serveWaitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer i.serveWaitGroup.Done()
		if err := i.inputBitRateMeasurerLoop(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Errorf(ctx, "inputBitRateMeasurerLoop failed: %v", err)
		}
	})

	i.serveWaitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer i.serveWaitGroup.Done()
		defer logger.Debugf(ctx, "inputwithfallback.Serve: inputChain receiver loop ended")
		for {
			select {
			case <-ctx.Done():
				return
			case inputChain := <-i.newInputChainChan:
				i.serveWaitGroup.Add(1)
				observability.Go(ctx, func(ctx context.Context) {
					defer i.serveWaitGroup.Done()
					inputChain.Serve(ctx, cfg, errCh)
				})
				// Auto-unpause every chain whose ID is at-or-above the
				// active priority (numerically: ID <= CurrentValue) —
				// this matches the consistency-check invariant
				// `paused = (ID > CurrentValue)` enforced by the loop
				// above.
				current := i.InputSwitch.CurrentValue.Load()
				if int32(inputChain.ID) <= current {
					logger.Debugf(ctx, "inputwithfallback.Serve: input chain %d arrived at-or-above active priority (current=%d), unpausing", inputChain.ID, current)
					if err := inputChain.Unpause(ctx); err != nil {
						errCh <- node.Error{
							Node: i,
							Err:  fmt.Errorf("unable to unpause input chain %d on arrival: %w", inputChain.ID, err),
						}
					}
				}
			}
		}
	})

	i.serveWaitGroup.Wait()
}

func (i *InputWithFallback[K, DF, C]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(i)
}

func (i *InputWithFallback[K, DF, C]) IsServing(ctx context.Context) bool {
	return i.isServing.Load()
}

func (i *InputWithFallback[K, DF, C]) GetPushTos(
	ctx context.Context,
) node.PushTos {
	return i.Output.GetPushTos(ctx)
}

func (i *InputWithFallback[K, DF, C]) AddPushTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetorframefiltercondition.Condition,
) {
	i.Output.AddPushTo(ctx, dst, conds...)
}

func (i *InputWithFallback[K, DF, C]) SetPushTos(
	ctx context.Context,
	pushTos node.PushTos,
) {
	i.Output.SetPushTos(ctx, pushTos)
}

func (i *InputWithFallback[K, DF, C]) WithPushTos(
	ctx context.Context,
	callback func(context.Context, *node.PushTos),
) {
	i.Output.WithPushTos(ctx, callback)
}

func (i *InputWithFallback[K, DF, C]) RemovePushTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return i.Output.RemovePushTo(ctx, dst)
}

func (i *InputWithFallback[K, DF, C]) GetCountersPtr() *nodetypes.Counters {
	return i.Output.GetCountersPtr()
}

func (i *InputWithFallback[K, DF, C]) GetProcessor() processor.Abstract {
	return i
}

func (i *InputWithFallback[K, DF, C]) GetInputFilter(
	ctx context.Context,
) packetorframefiltercondition.Condition {
	return i.InputFilter.Load()
}

func (i *InputWithFallback[K, DF, C]) SetInputFilter(
	ctx context.Context,
	cond packetorframefiltercondition.Condition,
) {
	i.InputFilter.Store(cond)
}

func (i *InputWithFallback[K, DF, C]) GetChangeChanIsServing() <-chan struct{} {
	return i.Output.GetChangeChanIsServing()
}

func (i *InputWithFallback[K, DF, C]) GetChangeChanPushTo() <-chan struct{} {
	return i.Output.GetChangeChanPushTo()
}

func (i *InputWithFallback[K, DF, C]) GetChangeChanDrained() <-chan struct{} {
	return i.Output.GetChangeChanDrained()
}

func (i *InputWithFallback[K, DF, C]) IsDrained(
	ctx context.Context,
) bool {
	panic("not implemented, yet")
}

func (i *InputWithFallback[K, DF, C]) Flush(
	ctx context.Context,
) error {
	panic("not implemented, yet")
}

func (i *InputWithFallback[K, DF, C]) Close(ctx context.Context) error {
	var errs []error
	// Snapshot the input chains under the lock, then clear the slice
	// and release the lock before calling Close on each chain.
	// Holding InputChainsLocker while closing chains causes a deadlock:
	// closing a chain cancels its processor ctx, which trips
	// Retryable.retry's OnError path; that OnError calls
	// onInputChainError, which tries to take InputChainsLocker —
	// blocking forever because Close is holding it.
	var chains []*InputChain[K, DF, C]
	i.InputChainsLocker.Do(ctx, func() {
		chains = i.InputChains
		i.InputChains = nil
	})
	for _, inputChain := range chains {
		if err := inputChain.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close input chain %v: %w", inputChain, err))
		}
	}
	if err := i.PreOutput.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close pre-output processor: %w", err))
	}
	if err := i.Output.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output processor: %w", err))
	}
	return errors.Join(errs...)
}

func (i *InputWithFallback[K, DF, C]) InputChan() chan<- packetorframe.InputUnion {
	return nil
}

func (i *InputWithFallback[K, DF, C]) OutputChan() <-chan packetorframe.OutputUnion {
	return i.Output.Processor.OutputChan()
}

func (i *InputWithFallback[K, DF, C]) ErrorChan() <-chan error {
	panic("not implemented, yet")
}

func (i *InputWithFallback[K, DF, C]) CountersPtr() *processor.Counters {
	return i.Output.Processor.CountersPtr()
}
