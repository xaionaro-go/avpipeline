package processor

import (
	"context"
	"fmt"
	"sync"

	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/observability"
)

type FromKernel[T kernel.Abstract] struct {
	*ChanStruct
	Kernel T

	closeOnce sync.Once
	closer    *astikit.Closer
}

var _ Abstract = (*FromKernel[kernel.Abstract])(nil)

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
		closer: astikit.NewCloser(),
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
	wg.Add(1)
	observability.Go(ctx, func() {
		defer observability.Go(ctx, func() {
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
		observability.Go(ctx, func() {
			defer swg.Done()
			err := p.Kernel.Generate(ctx, p.OutputPacketCh, p.OutputFrameCh)
			logger.Tracef(ctx, "p.Kernel.Generate: %v", err)
			if err != nil {
				p.ErrorCh <- fmt.Errorf(
					"kernel %T unable to generate traffic: %w",
					p.Kernel, err,
				)
			}
		})

		logger.Tracef(ctx, "ReaderLoop[%s]", p)
		err := ReaderLoop(ctx, p.InputPacketCh, p.InputFrameCh, p.Kernel, p.OutputPacketCh, p.OutputFrameCh)
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
			wg.Wait()
		})
	})
}

func (p *FromKernel[T]) Close(ctx context.Context) error {
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
		close(p.OutputPacketCh)
	}()
	return p.Kernel.Close(ctx)
}

func (p *FromKernel[T]) SendOutput(
	ctx context.Context,
	outputPacket packet.Output,
) {
	logger.Tracef(ctx, "SendOutput[%T]", p.Kernel)
	defer func() { logger.Tracef(ctx, "/SendOutput[%T]", p.Kernel) }()
	p.OutputPacketCh <- outputPacket
}

func (p *FromKernel[T]) SendInputPacketChan() chan<- packet.Input {
	return p.InputPacketCh
}

func (p *FromKernel[T]) OutputPacketChan() <-chan packet.Output {
	return p.OutputPacketCh
}

func (p *FromKernel[T]) SendInputFrameChan() chan<- frame.Input {
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
