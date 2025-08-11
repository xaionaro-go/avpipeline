package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type Retry[T Abstract] struct {
	*closeChan
	Factory      func(context.Context) (T, error)
	OnStart      func(context.Context, T) error
	OnError      func(context.Context, T, error) error
	Kernel       T
	KernelIsSet  bool
	KernelLocker xsync.Mutex
	KernelError  error
}

func NewRetry[T Abstract](
	ctx context.Context,
	factory func(context.Context) (T, error),
	onStart func(context.Context, T) error,
	onError func(context.Context, T, error) error,
) *Retry[T] {
	r := &Retry[T]{
		closeChan: newCloseChan(),
		Factory:   factory,
		OnStart:   onStart,
		OnError:   onError,
	}
	observability.Go(ctx, func(ctx context.Context) {
		r.KernelLocker.Do(xsync.WithEnableDeadlock(ctx, false), func() {
			r.startIfNeeded(ctx)
		})
	})
	return r
}

var _ Abstract = (*Retry[Abstract])(nil)
var _ packet.Source = (*Retry[Abstract])(nil)
var _ packet.Sink = (*Retry[Abstract])(nil)

func (r *Retry[T]) startIfNeeded(
	ctx context.Context,
) {
	if r.KernelIsSet || r.KernelError != nil {
		return
	}
	logger.Debugf(ctx, "start")
	defer logger.Debugf(ctx, "/start")

	for {
		if err := r.checkStatus(ctx); err != nil {
			logger.Errorf(ctx, "unable to start: %v", err)
			if r.KernelError == nil {
				r.KernelError = err
			}
			return
		}

		k, err := r.Factory(ctx)
		logger.Debugf(ctx, "factory results: %p %v", k, err)
		if err == nil {
			if r.OnStart != nil {
				err := r.OnStart(ctx, k)
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

func (r *Retry[T]) checkStatus(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.closeChan.CloseChan():
		return io.EOF
	default:
		return nil
	}
}

func (r *Retry[T]) retry(
	ctx context.Context,
	callback func(T) error,
) error {
	var zeroValue T
	return xsync.DoR1(xsync.WithEnableDeadlock(ctx, false), &r.KernelLocker, func() error {
		for {
			if err := r.checkStatus(ctx); err != nil {
				return err
			}

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

func (r *Retry[T]) getKernel(
	ctx context.Context,
) (T, error) {
	var zeroValue T
	if r.KernelError != nil {
		return zeroValue, r.KernelError
	}

	r.startIfNeeded(ctx)
	return r.Kernel, r.KernelError
}

func (r *Retry[T]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return r.retry(ctx, func(k T) error {
		return k.SendInputPacket(ctx, input, outputPacketsCh, outputFramesCh)
	})
}

func (r *Retry[T]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return r.retry(ctx, func(k T) error {
		return k.SendInputFrame(ctx, input, outputPacketsCh, outputFramesCh)
	})
}

func (r *Retry[T]) String() string {
	return fmt.Sprintf("Retry(%T:%s)", r.Kernel, r.Kernel)
}

func (r *Retry[T]) Close(ctx context.Context) error {
	r.closeChan.Close(ctx)
	observability.Go(ctx, func(ctx context.Context) {
		r.KernelLocker.Do(ctx, func() {
			if !r.KernelIsSet {
				return
			}
			err := r.Kernel.Close(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to close the kernel: %v", err)
			}
			logger.Debugf(ctx, "unset kernel")
			r.KernelIsSet = false
		})
	})
	return nil
}

func (r *Retry[T]) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return r.retry(ctx, func(k T) error {
		return k.Generate(ctx, outputPacketsCh, outputFramesCh)
	})
}

func (r *Retry[T]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	r.retry(ctx, func(k T) error {
		pktSrc, ok := Abstract(k).(packet.Source)
		if !ok {
			return nil
		}
		pktSrc.WithOutputFormatContext(ctx, callback)
		return nil
	})
}

func (r *Retry[T]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	r.retry(ctx, func(k T) error {
		pktSink, ok := Abstract(k).(packet.Sink)
		if !ok {
			return nil
		}
		pktSink.WithInputFormatContext(ctx, callback)
		return nil
	})
}

func (r *Retry[T]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	return r.retry(ctx, func(k T) error {
		pktSink, ok := Abstract(k).(packet.Sink)
		if !ok {
			return nil
		}
		return pktSink.NotifyAboutPacketSource(ctx, source)
	})
}

type ErrRetry struct {
	Err error
}

func (e ErrRetry) Error() string {
	return fmt.Sprintf("%s [please retry]", e.Err)
}
