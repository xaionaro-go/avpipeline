package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type Retryable[K Abstract] struct {
	*closuresignaler.ClosureSignaler
	Factory           func(context.Context) (K, error)
	OnError           RetryableFuncOnError[K]
	Config            RetryableConfig[K]
	Kernel            K
	KernelIsSet       bool
	KernelLocker      xsync.CtxLocker
	KernelError       error
	KernelOpenBarrier xatomic.Pointer[chan struct{}]
}

func NewRetryable[K Abstract](
	ctx context.Context,
	factory func(context.Context) (K, error),
	onErrorFunc RetryableFuncOnError[K],
	opts ...RetryableOption[K],
) *Retryable[K] {
	r := &Retryable[K]{
		ClosureSignaler: closuresignaler.New(),
		Factory:         factory,
		OnError:         onErrorFunc,
		Config:          RetryableOptions[K](opts).Config(),
		KernelLocker:    make(xsync.CtxLocker, 1),
	}
	r.KernelOpenBarrier.Pointer = ptr(make(chan struct{}))
	if r.Config.StartOnInit {
		r.unpauseKernelOpening(ctx)
		r.Config.OnInit(ctx, r)
		observability.Go(ctx, func(ctx context.Context) {
			r.KernelLocker.Do(xsync.WithEnableDeadlock(ctx, false), func() {
				r.openKernelIfNeeded(ctx)
			})
		})
	} else {
		r.pauseKernelOpening(ctx)
		r.Config.OnInit(ctx, r)
	}
	return r
}

var _ Abstract = (*Retryable[Abstract])(nil)
var _ packet.Source = (*Retryable[Abstract])(nil)
var _ packet.Sink = (*Retryable[Abstract])(nil)

func (r *Retryable[K]) unpauseKernelOpening(
	ctx context.Context,
) {
	oldCh := r.KernelOpenBarrier.Load()
	// close the channel if opened:
	select {
	case <-*oldCh:
	default:
		close(*oldCh)
	}
}

func (r *Retryable[K]) pauseKernelOpening(
	ctx context.Context,
) {
	// open the channel if closed:
	newCh := make(chan struct{})
	for {
		oldCh := r.KernelOpenBarrier.Load()
		select {
		case <-*oldCh:
			if r.KernelOpenBarrier.CompareAndSwap(oldCh, &newCh) {
				return
			}
			runtime.Gosched()
		default:
			return
		}
	}
}

func (r *Retryable[K]) openKernelIfNeeded(
	ctx context.Context,
) {
	if r.KernelIsSet || r.KernelError != nil {
		return
	}
	logger.Debugf(ctx, "openKernelIfNeeded")
	defer logger.Debugf(ctx, "/openKernelIfNeeded")

	for {

		err := func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-r.ClosureSignaler.CloseChan():
				return io.EOF
			case <-*r.KernelOpenBarrier.Load():
				return nil
			}
		}()
		if err != nil {
			logger.Errorf(ctx, "unable to open the kernel, because we are finishing: %v", err)
			if r.KernelError == nil {
				r.KernelError = err
			}
			return
		}

		k, err := r.Factory(ctx)
		logger.Debugf(ctx, "factory results: %p %v", k, err)
		if err == nil {
			if r.Config.OnKernelOpen != nil {
				err := r.Config.OnKernelOpen(ctx, k)
				if err != nil {
					r.KernelError = err
					r.Close(ctx)
					return
				}
			}
			logger.Debugf(ctx, "set kernel")
			r.Kernel = k
			r.KernelIsSet = true
			return
		}

		if r.OnError == nil {
			r.KernelError = err
			r.Close(ctx)
			return
		}

		err = r.OnError(ctx, k, err)
		switch {
		case err == nil:
		case errors.As(err, &ErrRetry{}):
		default:
			r.KernelError = err
			r.Close(ctx)
			return
		}
	}
}

func (r *Retryable[K]) retry(
	ctx context.Context,
	callback func(K) error,
) error {
	var zeroValue K
	return xsync.DoR1(xsync.WithEnableDeadlock(ctx, false), &r.KernelLocker, func() error {
		for {
			k, err := r.getKernel(ctx)
			if err != nil {
				logger.Debugf(ctx, "unset kernel: getKernel error %v", err)
				r.Kernel = zeroValue
				r.KernelIsSet = false
				return fmt.Errorf("unable to get kernel: %w", err)
			}

			err = callback(k)
			if err == nil {
				return nil
			}
			if r.OnError != nil {
				err = r.OnError(ctx, k, err)
				switch {
				case err == nil:
					return nil
				case errors.As(err, &ErrRetry{}):
				default:
					logger.Debugf(ctx, "unset kernel: OnError error: %v", err)
					r.Kernel = zeroValue
					r.KernelIsSet = false
					r.KernelError = err
					r.Close(ctx)
					return err
				}
			}

			logger.Debugf(ctx, "unset kernel: callback error: %v", err)
			r.Kernel = zeroValue
			r.KernelIsSet = false
		}
	})
}

func (r *Retryable[K]) getKernel(
	ctx context.Context,
) (K, error) {
	var zeroValue K
	if r.KernelError != nil {
		return zeroValue, r.KernelError
	}

	r.openKernelIfNeeded(ctx)
	return r.Kernel, r.KernelError
}

func (r *Retryable[K]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return r.retry(ctx, func(k K) (_err error) {
		defer func() {
			r := recover()
			if r != nil {
				_err = fmt.Errorf("panic in SendInputPacket: %v:\n%s", r, debug.Stack())
			}
		}()
		return k.SendInputPacket(ctx, input, outputPacketsCh, outputFramesCh)
	})
}

func (r *Retryable[K]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return r.retry(ctx, func(k K) (_err error) {
		defer func() {
			r := recover()
			if r != nil {
				_err = fmt.Errorf("panic in SendInputFrame: %v:\n%s", r, debug.Stack())
			}
		}()
		return k.SendInputFrame(ctx, input, outputPacketsCh, outputFramesCh)
	})
}

func (r *Retryable[K]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(r)
}

func (r *Retryable[K]) String() string {
	return fmt.Sprintf("Retry(%T:%s)", r.Kernel, r.Kernel)
}

func (r *Retryable[K]) Unpause(ctx context.Context) (_err error) {
	r.unpauseKernelOpening(ctx)
	observability.Go(ctx, func(ctx context.Context) {
		r.KernelLocker.Do(xsync.WithEnableDeadlock(ctx, false), func() {
			r.openKernelIfNeeded(ctx)
		})
	})
	return nil
}

func (r *Retryable[K]) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close()")
	defer func() { logger.Debugf(ctx, "/Close(): %v", _err) }()
	r.ClosureSignaler.Close(ctx)
	observability.Go(ctx, func(ctx context.Context) {
		err := r.Pause(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to stop the retry kernel: %v", err)
		}
	})
	return nil
}

func (r *Retryable[K]) Pause(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Stop()")
	defer func() { logger.Debugf(ctx, "/Stop(): %v", _err) }()
	return xsync.DoA1R1(ctx, &r.KernelLocker, r.pauseLocked, ctx)
}

func (r *Retryable[K]) pauseLocked(ctx context.Context) error {
	if !r.KernelIsSet {
		logger.Debugf(ctx, "kernel is not set, nothing to stop")
		return nil
	}
	err := r.Kernel.Close(ctx)
	if err != nil {
		return fmt.Errorf("unable to close kernel: %w", err)
	}
	logger.Debugf(ctx, "unset kernel")
	r.KernelIsSet = false
	r.pauseKernelOpening(ctx)
	return nil
}

func (r *Retryable[K]) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return r.retry(ctx, func(k K) error {
		return k.Generate(ctx, outputPacketsCh, outputFramesCh)
	})
}

func (r *Retryable[K]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	r.retry(ctx, func(k K) error {
		pktSrc, ok := Abstract(k).(packet.Source)
		if !ok {
			return nil
		}
		pktSrc.WithOutputFormatContext(ctx, callback)
		return nil
	})
}

func (r *Retryable[K]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	r.retry(ctx, func(k K) error {
		pktSink, ok := Abstract(k).(packet.Sink)
		if !ok {
			return nil
		}
		pktSink.WithInputFormatContext(ctx, callback)
		return nil
	})
}

func (r *Retryable[K]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	return r.retry(ctx, func(k K) error {
		pktSink, ok := Abstract(k).(packet.Sink)
		if !ok {
			return nil
		}
		return pktSink.NotifyAboutPacketSource(ctx, source)
	})
}

var _ GetInternalQueueSizer = (*Retryable[Abstract])(nil)

func (r *Retryable[K]) GetInternalQueueSize(
	ctx context.Context,
) map[string]uint64 {
	if _, ok := any(r.Kernel).(GetInternalQueueSizer); !ok {
		return nil
	}
	kernel := xsync.DoR1(xsync.WithEnableDeadlock(ctx, false), &r.KernelLocker, func() *K {
		if r.KernelIsSet {
			return &r.Kernel
		}
		return nil
	})
	if kernel == nil {
		logger.Debugf(ctx, "GetInternalQueueSize: kernel is not set")
		return nil
	}
	return any(*kernel).(GetInternalQueueSizer).GetInternalQueueSize(ctx)
}

type ErrRetry struct {
	Err error
}

func (e ErrRetry) Error() string {
	return fmt.Sprintf("%s [please retry]", e.Err)
}

var _ types.UnsafeGetOldestDTSInTheQueuer = (*Retryable[Abstract])(nil)

func (r *Retryable[K]) UnsafeGetOldestDTSInTheQueue(
	ctx context.Context,
) (time.Duration, error) {
	kernel := xsync.DoR1(xsync.WithEnableDeadlock(ctx, false), &r.KernelLocker, func() *K {
		if r.KernelIsSet {
			return &r.Kernel
		}
		return nil
	})
	if kernel == nil {
		return 0, ErrKernelNotSet{}
	}

	queuer, ok := any(*kernel).(types.UnsafeGetOldestDTSInTheQueuer)
	if !ok {
		return 0, ErrNotImplemented{}
	}

	return queuer.UnsafeGetOldestDTSInTheQueue(ctx)
}

type ErrKernelNotSet struct{}

func (e ErrKernelNotSet) Error() string {
	return "kernel is not set"
}
