package inputwithfallback

import (
	"context"
	"errors"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/packet"
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
			}
		}
	})

	i.serveWaitGroup.Wait()
}

func (i *InputWithFallback[K, DF, C]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(i)
}

func (i *InputWithFallback[K, DF, C]) IsServing() bool {
	return i.isServing.Load()
}

func (i *InputWithFallback[K, DF, C]) GetPushPacketsTos(
	ctx context.Context,
) node.PushPacketsTos {
	return i.Output.GetPushPacketsTos(ctx)
}

func (i *InputWithFallback[K, DF, C]) AddPushPacketsTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetfiltercondition.Condition,
) {
	i.Output.AddPushPacketsTo(ctx, dst, conds...)
}

func (i *InputWithFallback[K, DF, C]) SetPushPacketsTos(
	ctx context.Context,
	pushTos node.PushPacketsTos,
) {
	i.Output.SetPushPacketsTos(ctx, pushTos)
}

func (i *InputWithFallback[K, DF, C]) WithPushPacketsTos(
	ctx context.Context,
	callback func(context.Context, *node.PushPacketsTos),
) {
	i.Output.WithPushPacketsTos(ctx, callback)
}

func (i *InputWithFallback[K, DF, C]) RemovePushPacketsTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return i.Output.RemovePushPacketsTo(ctx, dst)
}

func (i *InputWithFallback[K, DF, C]) GetPushFramesTos(
	ctx context.Context,
) node.PushFramesTos {
	return i.Output.GetPushFramesTos(ctx)
}

func (i *InputWithFallback[K, DF, C]) AddPushFramesTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...framefiltercondition.Condition,
) {
	i.Output.AddPushFramesTo(ctx, dst, conds...)
}

func (i *InputWithFallback[K, DF, C]) SetPushFramesTos(
	ctx context.Context,
	pushTos node.PushFramesTos,
) {
	i.Output.SetPushFramesTos(ctx, pushTos)
}

func (i *InputWithFallback[K, DF, C]) WithPushFramesTos(
	ctx context.Context,
	callback func(context.Context, *node.PushFramesTos),
) {
	i.Output.WithPushFramesTos(ctx, callback)
}

func (i *InputWithFallback[K, DF, C]) RemovePushFramesTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return i.Output.RemovePushFramesTo(ctx, dst)
}

func (i *InputWithFallback[K, DF, C]) GetCountersPtr() *nodetypes.Counters {
	return i.Output.GetCountersPtr()
}

func (i *InputWithFallback[K, DF, C]) GetProcessor() processor.Abstract {
	return i
}

func (i *InputWithFallback[K, DF, C]) GetInputPacketFilter(
	ctx context.Context,
) packetfiltercondition.Condition {
	return i.InputPacketFilter.Load()
}

func (i *InputWithFallback[K, DF, C]) SetInputPacketFilter(
	ctx context.Context,
	cond packetfiltercondition.Condition,
) {
	i.InputPacketFilter.Store(cond)
}

func (i *InputWithFallback[K, DF, C]) GetInputFrameFilter(
	ctx context.Context,
) framefiltercondition.Condition {
	return i.InputFrameFilter.Load()
}

func (i *InputWithFallback[K, DF, C]) SetInputFrameFilter(
	ctx context.Context,
	cond framefiltercondition.Condition,
) {
	i.InputFrameFilter.Store(cond)
}

func (i *InputWithFallback[K, DF, C]) GetChangeChanIsServing() <-chan struct{} {
	return i.Output.GetChangeChanIsServing()
}

func (i *InputWithFallback[K, DF, C]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return i.Output.GetChangeChanPushPacketsTo()
}

func (i *InputWithFallback[K, DF, C]) GetChangeChanPushFramesTo() <-chan struct{} {
	return i.Output.GetChangeChanPushFramesTo()
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
	i.InputChainsLocker.Do(ctx, func() {
		for _, inputChain := range i.InputChains {
			if err := inputChain.Close(ctx); err != nil {
				errs = append(errs, fmt.Errorf("unable to close input chain %v: %w", inputChain, err))
			}
		}
		i.InputChains = nil
	})
	if err := i.PreOutput.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close pre-output processor: %w", err))
	}
	if err := i.Output.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output processor: %w", err))
	}
	return errors.Join(errs...)
}

func (i *InputWithFallback[K, DF, C]) InputPacketChan() chan<- packet.Input {
	return nil
}

func (i *InputWithFallback[K, DF, C]) OutputPacketChan() <-chan packet.Output {
	return i.Output.Processor.OutputPacketChan()
}

func (i *InputWithFallback[K, DF, C]) InputFrameChan() chan<- frame.Input {
	return nil
}

func (i *InputWithFallback[K, DF, C]) OutputFrameChan() <-chan frame.Output {
	return i.Output.Processor.OutputFrameChan()
}

func (i *InputWithFallback[K, DF, C]) ErrorChan() <-chan error {
	panic("not implemented, yet")
}

func (i *InputWithFallback[K, DF, C]) CountersPtr() *processor.Counters {
	return i.Output.Processor.CountersPtr()
}
