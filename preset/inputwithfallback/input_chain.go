package inputwithfallback

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/preset/autofix"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
)

type InputID int

type InputNode[K InputKernel, C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Retryable[K]]]

type InputKernel interface {
	kernel.Abstract
	packet.Source
}

var _ InputKernel = (*kernel.Input)(nil)

// retryable:input -> filter (-> autofix -> decoder) -> syncBarrier
type InputChain[K InputKernel, DF codec.DecoderFactory, C any] struct {
	ID           InputID
	InputFactory InputFactory[K, DF]
	Input        *InputNode[K, C]
	FilterSwitch *barrierstategetter.SwitchOutput
	Filter       *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
	Autofix      *autofix.AutoFixerWithCustomData[C]
	Decoder      *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Decoder[DF]]]
	SyncSwitch   *barrierstategetter.SwitchOutput
	SyncBarrier  *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
	isServing    atomic.Bool
}

func newInputChain[K InputKernel, DF codec.DecoderFactory, C any](
	ctx context.Context,
	inputID InputID,
	inputFactory InputFactory[K, DF],
	filterSwitch *barrierstategetter.SwitchOutput,
	syncSwitch *barrierstategetter.SwitchOutput,
	onKernelOpen func(context.Context, *InputChain[K, DF, C]),
	onError func(context.Context, *InputChain[K, DF, C], error),
) (*InputChain[K, DF, C], error) {
	r := &InputChain[K, DF, C]{
		ID:           inputID,
		InputFactory: inputFactory,
		FilterSwitch: filterSwitch,
		Filter:       node.NewWithCustomDataFromKernel[C, *kernel.Barrier](ctx, kernel.NewBarrier(ctx, filterSwitch)),
		SyncSwitch:   syncSwitch,
		SyncBarrier:  node.NewWithCustomDataFromKernel[C, *kernel.Barrier](ctx, kernel.NewBarrier(ctx, syncSwitch)),
	}

	decoder, err := inputFactory.NewDecoderFactory(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to create decoder factory for input %v: %w", inputID, err)
	}
	r.Decoder = node.NewWithCustomDataFromKernel[C, *kernel.Decoder[DF]](
		ctx,
		kernel.NewDecoder(ctx, decoder),
		processor.DefaultOptionsRecoder()...,
	)

	inputKernel := kernel.NewRetryable[K](ctx,
		func(ctx context.Context) (K, error) {
			return inputFactory.NewInput(ctx)
		},
		func(ctx context.Context, k K, err error) error {
			logger.Errorf(ctx, "input %v error: %v", inputID, err)
			if onError != nil {
				onError(ctx, r, err)
			}
			return kernel.ErrRetry{}
		},
		kernel.RetryableOptionStartOnInit[K](false),
		kernel.RetryableOptionOnKernelOpen[K](func(
			ctx context.Context,
			k K,
		) (_err error) {
			logger.Debugf(ctx, "RetryableOptionOnKernelOpen: %d", inputID)
			defer func() { logger.Debugf(ctx, "/RetryableOptionOnKernelOpen: %d: %v", inputID, _err) }()
			if onKernelOpen != nil {
				onKernelOpen(ctx, r)
			}
			return nil
		}),
	)
	r.Input = node.NewWithCustomDataFromKernel[C, *kernel.Retryable[K]](
		ctx,
		inputKernel,
		processor.DefaultOptionsInput()...,
	)
	r.Input.AddPushPacketsTo(ctx, r.Filter)
	r.Input.AddPushFramesTo(ctx, r.Filter)
	var zeroCustomData C
	r.Autofix = autofix.NewWithCustomData[C](ctx, r.Input.Processor.Kernel, r.Decoder.Processor.Kernel, zeroCustomData)
	r.Filter.AddPushPacketsTo(ctx, r.Autofix)
	r.Filter.AddPushFramesTo(ctx, r.Autofix)
	r.Autofix.AddPushPacketsTo(ctx, r.Decoder)
	r.Autofix.AddPushFramesTo(ctx, r.Decoder)
	r.Decoder.AddPushPacketsTo(ctx, r.SyncBarrier)
	r.Decoder.AddPushFramesTo(ctx, r.SyncBarrier)

	return r, nil
}

func (i *InputChain[K, DF, C]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	if !i.isServing.CompareAndSwap(false, true) {
		panic("InputChain.Serve: already started")
	}
	defer i.isServing.Store(false)

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	logger.Debugf(ctx, "InputChain[%d].Serve: started", i.ID)
	defer logger.Debugf(ctx, "InputChain[%d].Serve: ended", i.ID)

	var wg sync.WaitGroup

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Debugf(ctx, "InputChain[%d].Serve: input node serving ended", i.ID)
		logger.Debugf(ctx, "InputChain[%d].Serve: input node serving started", i.ID)
		i.Input.Serve(ctx, cfg, errCh)
	})

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Debugf(ctx, "InputChain[%d].Serve: filter node serving ended", i.ID)
		logger.Debugf(ctx, "InputChain[%d].Serve: filter node serving started", i.ID)
		i.Filter.Serve(ctx, cfg, errCh)
	})

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Debugf(ctx, "InputChain[%d].Serve: decoder node serving ended", i.ID)
		logger.Debugf(ctx, "InputChain[%d].Serve: decoder node serving started", i.ID)
		i.Decoder.Serve(ctx, cfg, errCh)
	})

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Debugf(ctx, "InputChain[%d].Serve: sync barrier node serving ended", i.ID)
		logger.Debugf(ctx, "InputChain[%d].Serve: sync barrier node serving started", i.ID)
		i.SyncBarrier.Serve(ctx, cfg, errCh)
	})

	wg.Wait()
}

func (i *InputChain[K, DF, C]) String() string {
	ctx := context.TODO()
	if !i.Input.Processor.Kernel.KernelLocker.ManualTryLock(ctx) {
		return fmt.Sprintf("InputChain(<unable to lock>; factory:%s)", i.InputFactory)
	}
	kernel := func() K {
		defer i.Input.Processor.Kernel.KernelLocker.ManualUnlock()
		return i.Input.Processor.Kernel.Kernel
	}()
	if i.Input.Processor.Kernel.KernelIsSet {
		return fmt.Sprintf("InputChain(%v:active)", kernel)
	}
	return fmt.Sprintf("InputChain(%s:inactive)", i.InputFactory)
}

func (i *InputChain[K, DF, C]) Unpause(
	ctx context.Context,
) error {
	return i.Input.Processor.Kernel.Unpause(ctx)
}

func (i *InputChain[K, DF, C]) Pause(
	ctx context.Context,
) error {
	return i.Input.Processor.Kernel.Pause(ctx)
}

func (i *InputChain[K, DF, C]) GetInput() node.Abstract {
	return i.Input
}

func (i *InputChain[K, DF, C]) GetOutput() node.Abstract {
	return i.SyncBarrier
}

func (i *InputChain[K, DF, C]) Close(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Close()")
	defer func() { logger.Debugf(ctx, "/Close(): %v", _err) }()

	var errs []error
	if err := i.Input.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close input node: %w", err))
	}
	if err := i.Filter.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close filter node: %w", err))
	}
	if err := i.Autofix.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close autofix node: %w", err))
	}
	if err := i.Decoder.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close decoder node: %w", err))
	}
	if err := i.SyncBarrier.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close sync barrier node: %w", err))
	}
	return errors.Join(errs...)
}

func (i *InputChain[K, DF, C]) IsPaused(
	ctx context.Context,
) bool {
	return i.Input.Processor.Kernel.IsPaused(ctx)
}
