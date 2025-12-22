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
	"github.com/xaionaro-go/avpipeline/preset/autoheaders"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
)

type InputID int

type InputNode[K InputKernel, C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Retryable[K]]]

type InputNodes[K InputKernel, C any] []*InputNode[K, C]

func (in InputNodes[K, C]) NonNil() InputNodes[K, C] {
	var r InputNodes[K, C]
	for _, n := range in {
		if n != nil {
			r = append(r, n)
		}
	}
	return r
}

type InputKernel interface {
	kernel.Abstract
	packet.Source
}

var _ InputKernel = (*kernel.Input)(nil)

// retryable:input -> filter (-> autoheaders -> decoder) -> syncBarrier
type InputChain[K InputKernel, DF codec.DecoderFactory, C any] struct {
	ID           InputID
	InputFactory InputFactory[K, DF, C]
	Input        *InputNode[K, C]
	FilterSwitch *barrierstategetter.SwitchOutput
	Filter       *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
	AutoHeaders  *autoheaders.NodeWithCustomData[C]
	Decoder      *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Decoder[DF]]]
	SyncSwitch   *barrierstategetter.SwitchOutput
	SyncBarrier  *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
	isServing    atomic.Bool
}

func newInputChain[K InputKernel, DF codec.DecoderFactory, C any](
	ctx context.Context,
	inputID InputID,
	inputFactory InputFactory[K, DF, C],
	filterSwitch *barrierstategetter.SwitchOutput,
	syncSwitch *barrierstategetter.SwitchOutput,
	onKernelOpen func(context.Context, *InputChain[K, DF, C]),
	onError func(context.Context, *InputChain[K, DF, C], error) error,
) (*InputChain[K, DF, C], error) {
	r := &InputChain[K, DF, C]{
		ID:           inputID,
		InputFactory: inputFactory,
		FilterSwitch: filterSwitch,
		Filter:       node.NewWithCustomDataFromKernel[C](ctx, kernel.NewBarrier(ctx, filterSwitch)),
		SyncSwitch:   syncSwitch,
		SyncBarrier:  node.NewWithCustomDataFromKernel[C](ctx, kernel.NewBarrier(ctx, syncSwitch)),
	}

	decoderFactory, err := inputFactory.NewDecoderFactory(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("unable to create decoder factory for input %v: %w", inputID, err)
	}
	if any(decoderFactory) != codec.DecoderFactory(nil) {
		r.Decoder = node.NewWithCustomDataFromKernel[C](
			ctx,
			kernel.NewDecoder(ctx, decoderFactory),
			processor.DefaultOptionsTranscoder()...,
		)
	}

	inputKernel := kernel.NewRetryable(ctx,
		func(ctx context.Context) (K, error) {
			return inputFactory.NewInput(ctx, r)
		},
		func(ctx context.Context, k K, err error) error {
			logger.Errorf(ctx, "input %v error: %v", inputID, err)
			if onError != nil {
				if err := onError(ctx, r, err); err != nil {
					return err
				}
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
	r.Input = node.NewWithCustomDataFromKernel[C](
		ctx,
		inputKernel,
		processor.DefaultOptionsInput()...,
	)
	r.Input.AddPushPacketsTo(ctx, r.Filter)
	r.Input.AddPushFramesTo(ctx, r.Filter)
	if r.Decoder == nil {
		r.Filter.AddPushPacketsTo(ctx, r.SyncBarrier)
		r.Filter.AddPushFramesTo(ctx, r.SyncBarrier)
	} else {
		r.AutoHeaders = autoheaders.NewNodeWithCustomData[C](ctx, r.Decoder.Processor.Kernel)
		r.Filter.AddPushPacketsTo(ctx, r.AutoHeaders)
		r.Filter.AddPushFramesTo(ctx, r.AutoHeaders)
		r.AutoHeaders.AddPushPacketsTo(ctx, r.Decoder)
		r.AutoHeaders.AddPushFramesTo(ctx, r.Decoder)
		r.Decoder.AddPushPacketsTo(ctx, r.SyncBarrier)
		r.Decoder.AddPushFramesTo(ctx, r.SyncBarrier)
	}

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

	if i.AutoHeaders != nil {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			defer logger.Debugf(ctx, "InputChain[%d].Serve: autoheaders node serving ended", i.ID)
			logger.Debugf(ctx, "InputChain[%d].Serve: autoheaders node serving started", i.ID)
			i.AutoHeaders.Serve(ctx, cfg, errCh)
		})
	}

	if i.Decoder != nil {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			defer logger.Debugf(ctx, "InputChain[%d].Serve: decoder node serving ended", i.ID)
			logger.Debugf(ctx, "InputChain[%d].Serve: decoder node serving started", i.ID)
			i.Decoder.Serve(ctx, cfg, errCh)
		})
	}

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
	if err := i.AutoHeaders.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close autoheaders node: %w", err))
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
