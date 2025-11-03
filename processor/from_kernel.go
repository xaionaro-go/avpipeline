package processor

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type FromKernel[T kernel.Abstract] struct {
	*ChanStruct
	Kernel T

	preOutputPacketsCh chan packet.Output
	preOutputFramesCh  chan frame.Output

	closeOnce sync.Once
	closer    *astikit.Closer
	OnClosed  func(context.Context) error

	CountersStorage *Counters
}

type GetPacketSourcer interface {
	GetPacketSource() packet.Source
}

type GetPacketSinker interface {
	GetPacketSink() packet.Sink
}

var _ Abstract = (*FromKernel[kernel.Abstract])(nil)
var _ GetPacketSourcer = (*FromKernel[kernel.Abstract])(nil)
var _ GetPacketSinker = (*FromKernel[kernel.Abstract])(nil)

func NewFromKernel[T kernel.Abstract](
	ctx context.Context,
	kernel T,
	opts ...Option,
) *FromKernel[T] {
	opts = append([]Option{
		OptionQueueSizeInputPacket(1),
		OptionQueueSizeOutputPacket(1),
		OptionQueueSizeInputFrame(1),
		OptionQueueSizeOutputFrame(1),
		OptionQueueSizeError(1),
	}, opts...)
	cfg := Options(opts).config()
	p := &FromKernel[T]{
		ChanStruct: NewChanStruct(
			cfg.InputPacketQueue, cfg.OutputPacketQueue,
			cfg.InputFrameQueue, cfg.OutputFrameQueue,
			cfg.ErrorQueue,
		),
		Kernel: kernel,

		preOutputPacketsCh: make(chan packet.Output, 1),
		preOutputFramesCh:  make(chan frame.Output, 1),

		CountersStorage: processortypes.NewCounters(),
		closer:          astikit.NewCloser(),
	}
	p.startProcessing(ctx)
	return p
}

func (p *FromKernel[T]) startProcessing(ctx context.Context) {
	logger.Tracef(ctx, "startProcessing[%s]", p)
	defer func() { logger.Tracef(ctx, "/startProcessing[%s]", p) }()

	errCh := p.outChanError()

	ctx, cancelFn := context.WithCancel(ctx)
	var wg sync.WaitGroup

	var debugM xsync.Map[string, struct{}]

	debugM.Store("preOutputPacketsCh", struct{}{})
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer debugM.Delete("preOutputPacketsCh")
		defer wg.Done()
		defer close(p.OutputPacketCh)
		for {
			select {
			case <-ctx.Done():
				return
			case outputPacket, ok := <-p.preOutputPacketsCh:
				if !ok {
					return
				}
				mediaType := outputPacket.GetMediaType()
				objSize := uint64(outputPacket.GetSize())
				p.CountersStorage.Generated.Packets.Increment(globaltypes.MediaType(mediaType), objSize)
				select {
				case <-ctx.Done():
					p.CountersStorage.Omitted.Packets.Increment(globaltypes.MediaType(mediaType), objSize)
					return
				case p.OutputPacketCh <- outputPacket:
				}
			}
		}
	})

	debugM.Store("preOutputFramesCh", struct{}{})
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer debugM.Delete("preOutputFramesCh")
		defer wg.Done()
		defer close(p.OutputFrameCh)
		for {
			select {
			case <-ctx.Done():
				return
			case outputFrame, ok := <-p.preOutputFramesCh:
				if !ok {
					return
				}
				mediaType := outputFrame.GetMediaType()
				objSize := uint64(outputFrame.GetSize())
				p.CountersStorage.Generated.Frames.Increment(globaltypes.MediaType(mediaType), objSize)
				select {
				case <-ctx.Done():
					p.CountersStorage.Omitted.Frames.Increment(globaltypes.MediaType(mediaType), objSize)
					return
				case p.OutputFrameCh <- outputFrame:
				}
			}
		}
	})

	debugM.Store("readerLoop", struct{}{})
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer observability.Go(ctx, func(ctx context.Context) {
			defer debugM.Delete("readerLoop")
			defer wg.Done()
			logger.Tracef(ctx, "finalize[%s]", p)
			err := p.finalize(ctx)
			logger.Tracef(ctx, "/finalize[%s]: %v", p, err)
			errmon.ObserveErrorCtx(ctx, err)
			if err != nil {
				errCh <- err
			}
			close(errCh)
		})

		var swg sync.WaitGroup
		defer swg.Wait()
		swg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer swg.Done()
			err := p.Kernel.Generate(ctx, p.preOutputPacketsCh, p.preOutputFramesCh)
			logger.Tracef(ctx, "p.Kernel[%T].Generate: %v", p, err)
			if err != nil {
				p.ErrorCh <- fmt.Errorf(
					"kernel %T unable to generate traffic: %w",
					p.Kernel, err,
				)
			}
		})

		logger.Tracef(ctx, "ReaderLoop[%s]", p)
		err := readerLoop(
			ctx,
			p.InputPacketCh, p.InputFrameCh,
			p.Kernel,
			p.preOutputPacketsCh, p.preOutputFramesCh,
			p.CountersStorage,
		)
		logger.Tracef(ctx, "/ReaderLoop[%s]: %v", p, err)
		if err != nil {
			errCh <- err
		}
	})

	var once sync.Once
	p.addToCloser(func() {
		once.Do(func() {
			logger.Tracef(ctx, "close[%s]", p)
			defer logger.Tracef(ctx, "/close[%s]", p)
			cancelFn()
			runtime.Gosched()
			var leftovers []string
			debugM.Range(func(key string, value struct{}) bool {
				leftovers = append(leftovers, key)
				return true
			})
			logger.Tracef(ctx, "wait[%s] for %s", p, strings.Join(leftovers, ", "))
			wg.Wait()
		})
	})
}

func (p *FromKernel[T]) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close[%T]", p.Kernel)
	defer func() { logger.Debugf(ctx, "/Close[%T]: %v", p.Kernel, _err) }()
	var err error
	p.closeOnce.Do(func() {
		err = p.closer.Close()
	})
	return err
}

func (p *FromKernel[T]) addToCloser(callback func()) {
	p.closer.Add(callback)
}

func (p *FromKernel[T]) finalize(ctx context.Context) error {
	logger.Debugf(ctx, "closing %T", p.Kernel)
	defer func() {
		close(p.preOutputPacketsCh)
		close(p.preOutputFramesCh)
	}()

	var errs []error
	if err := p.Kernel.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close the kernel: %w", err))
	}
	if p.OnClosed != nil {
		if err := p.OnClosed(ctx); err != nil {
			errs = append(errs, fmt.Errorf("OnClosed returned an error: %w", err))
		}
	}

	return errors.Join(errs...)
}

func (p *FromKernel[T]) InputPacketChan() chan<- packet.Input {
	return p.InputPacketCh
}

func (p *FromKernel[T]) OutputPacketChan() <-chan packet.Output {
	return p.OutputPacketCh
}

func (p *FromKernel[T]) InputFrameChan() chan<- frame.Input {
	return p.InputFrameCh
}

func (p *FromKernel[T]) OutputFrameChan() <-chan frame.Output {
	return p.OutputFrameCh
}

func (p *FromKernel[T]) ErrorChan() <-chan error {
	return p.ErrorCh
}

func (p *FromKernel[T]) outChanError() chan<- error {
	return p.ErrorCh
}

func (p *FromKernel[T]) String() string {
	return p.Kernel.String()
}

func (p *FromKernel[T]) GetPacketSource() packet.Source {
	source, ok := any(p.Kernel).(packet.Source)
	if !ok {
		return nil
	}
	return source
}

func (p *FromKernel[T]) GetPacketSink() packet.Sink {
	sink, ok := any(p.Kernel).(packet.Sink)
	if !ok {
		return nil
	}
	return sink
}

type GetInternalQueueSizer = kernel.GetInternalQueueSizer

var _ GetInternalQueueSizer = (*FromKernel[kernel.Abstract])(nil)

func (p *FromKernel[T]) GetInternalQueueSize(
	ctx context.Context,
) map[string]uint64 {
	queuer, ok := any(p.Kernel).(GetInternalQueueSizer)
	if !ok {
		logger.Debugf(ctx, "GetInternalQueueSize: kernel %T does not implement GetInternalQueueSizer", p.Kernel)
		return nil
	}
	return queuer.GetInternalQueueSize(ctx)
}

type GetKerneler = kernel.GetKerneler

var _ GetKerneler = (*FromKernel[kernel.Abstract])(nil)

func (p *FromKernel[T]) GetKernel() kernel.Abstract {
	return p.Kernel
}

func (p *FromKernel[T]) CountersPtr() *Counters {
	return p.CountersStorage
}

var _ Flusher = (*FromKernel[kernel.Abstract])(nil)

func (p *FromKernel[T]) IsDirty(ctx context.Context) bool {
	if isDirtier, ok := any(p.Kernel).(kernel.Flusher); ok {
		return isDirtier.IsDirty(ctx)
	}
	return false
}

func (p *FromKernel[T]) Flush(
	ctx context.Context,
) error {
	flusher, ok := any(p.Kernel).(kernel.Flusher)
	if !ok {
		return nil // no internal buffers to flush
	}

	return flusher.Flush(ctx, p.preOutputPacketsCh, p.preOutputFramesCh)
}
