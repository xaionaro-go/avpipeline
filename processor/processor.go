package processor

import (
	"context"
	"fmt"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/observability"
)

type Abstract interface {
	fmt.Stringer
	Closer

	GetOutputFormatContext(ctx context.Context) *astiav.FormatContext
	SendInputChan() chan<- InputPacket
	OutputPacketsChan() <-chan OutputPacket
	ErrorChan() <-chan error
}

type FromKernel struct {
	*ChanStruct
	Kernel kernel.Abstract

	closeOnce sync.Once
	closer    *astikit.Closer
}

var _ Abstract = (*FromKernel)(nil)

func NewFromKernel(
	ctx context.Context,
	kernel kernel.Abstract,
	opts ...Option,
) *FromKernel {
	opts = append([]Option{
		OptionQueueSizeInput(1),
		OptionQueueSizeOutput(1),
		OptionQueueSizeError(1),
	}, opts...)
	cfg := Options(opts).config()
	p := &FromKernel{
		ChanStruct: NewChanStruct(cfg.InputQueue, cfg.OutputQueue, cfg.ErrorQueue),
		Kernel:     kernel,
		closer:     astikit.NewCloser(),
	}
	p.startProcessing(ctx)
	return p
}

func (p *FromKernel) startProcessing(ctx context.Context) {
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
			err := p.Kernel.Generate(ctx, p.OutputCh)
			logger.Tracef(ctx, "p.Kernel.Generate: %v", err)
			if err != nil {
				p.ErrorCh <- fmt.Errorf(
					"kernel %T unable to generate traffic: %w",
					p.Kernel, err,
				)
			}
		})

		logger.Tracef(ctx, "ReaderLoop[%s]", p)
		err := ReaderLoop(ctx, p.InputCh, p.Kernel, p.OutputCh)
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

func (p *FromKernel) Close(ctx context.Context) error {
	var err error
	p.closeOnce.Do(func() {
		err = p.closer.Close()
	})
	return err
}

func (p *FromKernel) addToCloser(callback func()) {
	p.closer.Add(callback)
}

func (p *FromKernel) finalize(ctx context.Context) error {
	logger.Debugf(ctx, "closing %T", p.Kernel)
	defer func() {
		close(p.OutputCh)
	}()
	return p.Kernel.Close(ctx)
}

func (p *FromKernel) SendOutput(
	ctx context.Context,
	outputPacket OutputPacket,
) {
	logger.Tracef(ctx, "SendOutput[%T]", p.Kernel)
	defer func() { logger.Tracef(ctx, "/SendOutput[%T]", p.Kernel) }()
	p.OutputCh <- outputPacket
}

func (p *FromKernel) SendInputChan() chan<- InputPacket {
	return p.InputCh
}

func (p *FromKernel) OutputPacketsChan() <-chan OutputPacket {
	return p.OutputCh
}

func (p *FromKernel) ErrorChan() <-chan error {
	return p.ErrorCh
}

func (p *FromKernel) outChanError() chan<- error {
	return p.ErrorCh
}

func (p *FromKernel) GetOutputFormatContext(
	ctx context.Context,
) *astiav.FormatContext {
	return p.Kernel.GetOutputFormatContext(ctx)
}

func (p *FromKernel) String() string {
	return p.Kernel.String()
}
