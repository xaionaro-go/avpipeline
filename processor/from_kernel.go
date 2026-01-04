// from_kernel.go is the primary implementation of Abstract that wraps a kernel and manages asynchronous communication.

package processor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/asticode/go-astikit"
	xruntime "github.com/facebookincubator/go-belt/pkg/runtime"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/xaionaro-go/avpipeline/kernel"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type FromKernel[T kernel.Abstract] struct {
	*ChanStruct
	Kernel T

	preOutputCh chan packetorframe.OutputUnion

	closeOnce sync.Once
	closer    *astikit.Closer
	OnClosed  func(context.Context) error

	CountersStorage *Counters
}

var _ globaltypes.ErrorHandler = (*FromKernel[kernel.Abstract])(nil)

type GetPacketSourcer interface {
	GetPacketSource() packet.Source
}

type GetPacketSinker interface {
	GetPacketSink() packet.Sink
}

var (
	_ Abstract         = (*FromKernel[kernel.Abstract])(nil)
	_ GetPacketSourcer = (*FromKernel[kernel.Abstract])(nil)
	_ GetPacketSinker  = (*FromKernel[kernel.Abstract])(nil)
)

func NewFromKernel[T kernel.Abstract](
	ctx context.Context,
	kernel T,
	opts ...Option,
) *FromKernel[T] {
	opts = append([]Option{
		OptionQueueSizeInput(1),
		OptionQueueSizeOutput(1),
		OptionQueueSizeError(1),
	}, opts...)
	cfg := Options(opts).config()
	p := &FromKernel[T]{
		ChanStruct: NewChanStruct(
			cfg.InputQueue,
			cfg.OutputQueue,
			cfg.ErrorQueue,
		),
		Kernel: kernel,

		preOutputCh: make(chan packetorframe.OutputUnion, 1),

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

	debugM.Store("preOutputCh", struct{}{})
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer debugM.Delete("preOutputCh")
		defer wg.Done()
		defer close(p.OutputCh)
		for {
			select {
			case <-ctx.Done():
				return
			case output, ok := <-p.preOutputCh:
				if !ok {
					return
				}
				if output.Packet != nil {
					outputPacket := *output.Packet
					mediaType := outputPacket.GetMediaType()
					objSize := uint64(outputPacket.GetSize())
					p.CountersStorage.Generated.Packets.Increment(globaltypes.MediaType(mediaType), objSize)
					select {
					case <-ctx.Done():
						p.CountersStorage.Omitted.Packets.Increment(globaltypes.MediaType(mediaType), objSize)
						return
					case p.OutputCh <- output:
					}
				}
				if output.Frame != nil {
					outputFrame := *output.Frame
					mediaType := outputFrame.GetMediaType()
					objSize := uint64(outputFrame.GetSize())
					p.CountersStorage.Generated.Frames.Increment(globaltypes.MediaType(mediaType), objSize)
					select {
					case <-ctx.Done():
						p.CountersStorage.Omitted.Frames.Increment(globaltypes.MediaType(mediaType), objSize)
						return
					case p.OutputCh <- output:
					}
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
			err := p.Kernel.Generate(ctx, p.preOutputCh)
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
			p.InputCh,
			p.Kernel,
			p.preOutputCh,
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

func getCaller() (string, int) {
	cnt := 0
	return xruntime.Caller(func(pc uintptr) bool {
		if cnt >= 3 {
			return true
		}
		cnt++
		return false
	}).FileLine()
}

func (p *FromKernel[T]) Close(ctx context.Context) (_err error) {
	f, l := getCaller()
	logger.Debugf(ctx, "Close[%T]: called from %s:%d", p.Kernel, f, l)
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
		close(p.preOutputCh)
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

func (p *FromKernel[T]) InputChan() chan<- packetorframe.InputUnion {
	return p.InputCh
}

func (p *FromKernel[T]) OutputChan() <-chan packetorframe.OutputUnion {
	return p.OutputCh
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
	// Unwrap wrapper types (like Retryable) to get the actual source kernel.
	// This handles multiple layers of wrapping recursively.
	// If any wrapper's inner kernel is not available, returns nil rather than
	// returning a wrapper, since wrappers should never be used as packet sources.
	return kerneltypes.GetOriginalPacketSource(source)
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

var _ kerneltypes.WithNetworkConner = (*FromKernel[kernel.Abstract])(nil)

func (p *FromKernel[T]) WithNetworkConn(
	ctx context.Context,
	callback func(context.Context, net.Conn) error,
) error {
	getter, ok := any(p.Kernel).(kerneltypes.WithNetworkConner)
	if !ok {
		return ErrNotImplemented{
			Err: fmt.Errorf("kernel %T does not implement GetNetConner", p.Kernel),
		}
	}
	return getter.WithNetworkConn(ctx, callback)
}

var _ kerneltypes.WithRawNetworkConner = (*FromKernel[kernel.Abstract])(nil)

func (p *FromKernel[T]) WithRawNetworkConn(
	ctx context.Context,
	callback func(context.Context, syscall.RawConn, string) error,
) error {
	getter, ok := any(p.Kernel).(kerneltypes.WithRawNetworkConner)
	if !ok {
		return ErrNotImplemented{
			Err: fmt.Errorf("kernel %T does not implement GetSyscallRawConner", p.Kernel),
		}
	}
	return getter.WithRawNetworkConn(ctx, callback)
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

	return flusher.Flush(ctx, p.preOutputCh)
}

func (p *FromKernel[T]) HandleError(
	ctx context.Context,
	err error,
) error {
	if h, ok := any(p.Kernel).(globaltypes.ErrorHandler); ok {
		return h.HandleError(ctx, err)
	}
	return err
}

var _ processortypes.UnsafeGetOldestDTSInTheQueuer = (*FromKernel[kernel.Abstract])(nil)

func (p *FromKernel[T]) GetOldestDTSInTheQueue(
	ctx context.Context,
) (_ret time.Duration, _err error) {
	queuer, ok := any(p.Kernel).(kerneltypes.GetOldestDTSInTheQueuer)
	if !ok {
		return 0, ErrNotImplemented{Err: kerneltypes.ErrNotImplemented{}}
	}
	return queuer.GetOldestDTSInTheQueue(ctx)
}
