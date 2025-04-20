package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type Retry[T Abstract] struct {
	*closeChan
	Factory      func(context.Context) (T, error)
	OnStart      func(context.Context, T) error
	OnEnd        func(context.Context, T, error) error
	Kernel       T
	KernelIsSet  bool
	KernelLocker xsync.Mutex
	KernelError  error
}

func NewRetry[T Abstract](
	ctx context.Context,
	factory func(context.Context) (T, error),
	onStart func(context.Context, T) error,
	onEnd func(context.Context, T, error) error,
) *Retry[T] {
	r := &Retry[T]{
		closeChan: newCloseChan(),
		Factory:   factory,
		OnStart:   onStart,
		OnEnd:     onEnd,
	}
	observability.Go(ctx, func() {
		r.KernelLocker.Do(xsync.WithEnableDeadlock(ctx, false), func() {
			r.start(ctx)
		})
	})
	return r
}

var _ Abstract = (*Retry[Abstract])(nil)
var _ packet.Source = (*Retry[Abstract])(nil)

func (r *Retry[T]) start(
	ctx context.Context,
) {
	if r.KernelIsSet || r.KernelError != nil {
		return
	}

	for {
		if err := r.checkStatus(ctx); err != nil {
			logger.Errorf(ctx, "unable to start: %v", err)
			if r.KernelError == nil {
				r.KernelError = err
			}
			return
		}

		k, err := r.Factory(ctx)
		if err == nil {
			if r.OnStart != nil {
				err := r.OnStart(ctx, k)
				if err != nil {
					r.KernelError = err
					r.Close(ctx)
					return
				}
			}
			r.Kernel = k
			r.KernelIsSet = true
			return
		}

		if r.OnEnd == nil {
			r.KernelError = err
			r.Close(ctx)
			return
		}

		err = r.OnEnd(ctx, k, err)
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
				r.Kernel = zeroValue
				r.KernelIsSet = false
				return fmt.Errorf("unable to get kernel: %w", err)
			}

			err = callback(k)
			if r.OnEnd != nil {
				err = r.OnEnd(ctx, k, err)
				switch {
				case err == nil:
				case errors.As(err, &ErrRetry{}):
				default:
					r.KernelError = err
					r.Close(ctx)
					return err
				}
			}
			if err == nil {
				return nil
			}

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

	r.start(ctx)
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
	observability.Go(ctx, func() {
		r.KernelLocker.Do(ctx, func() {
			if !r.KernelIsSet {
				return
			}
			err := r.Kernel.Close(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to close the kernel: %v", err)
			}
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

func (r *Retry[T]) WithFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	r.retry(ctx, func(k T) error {
		pktSrc, ok := Abstract(k).(packet.Source)
		if !ok {
			return nil
		}
		pktSrc.WithFormatContext(ctx, callback)
		return nil
	})
}

func (r *Retry[T]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) error {
	return r.retry(ctx, func(k T) error {
		pktSrc, ok := Abstract(k).(packet.Source)
		if !ok {
			return nil
		}
		return pktSrc.NotifyAboutPacketSource(ctx, source)
	})
}

type ErrRetry struct {
	Err error
}

func (e ErrRetry) Error() string {
	return fmt.Sprintf("%s [please retry]", e.Err)
}
