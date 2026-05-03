// route_forwarding.go defines the RouteForwarding struct for forwarding streams between routes.

package router

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	transcodertypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type NodeForwardingOutput[T any] interface {
	node.Abstract
	types.Closer
	GetOutputRoute(ctx context.Context) *Route[T]
}

type ForwardOutputFactory[T any] interface {
	String() string
	NewOutput(ctx context.Context, fwd *RouteForwarding[T]) (NodeForwardingOutput[T], error)
}

type RouteForwarding[T any] struct {
	Router              *Router[T]
	SrcPath             RoutePath
	GetSrcRouteMode     GetRouteMode
	OutputFactory       ForwardOutputFactory[T]
	PublishMode         PublishMode
	TranscoderConfig    *transcodertypes.TranscoderConfig
	FilterKernelFactory FilterKernelFactory
	Locker              xsync.Mutex
	CancelFunc          context.CancelFunc
	Input               *Route[T]
	Output              NodeForwardingOutput[T]
	WaitGroup           sync.WaitGroup
	StreamForwarder[GoBug63285RouteInterface[T], *ProcessorRouting]
}

func (r *Router[T]) AddRouteForwarding(
	ctx context.Context,
	srcPath RoutePath,
	getSrcRouteMode GetRouteMode,
	outputFactory ForwardOutputFactory[T],
	publishMode PublishMode,
	transcoderConfig *transcodertypes.TranscoderConfig,
	filterKernelFactory FilterKernelFactory,
) (_ret *RouteForwarding[T], _err error) {
	logger.Debugf(ctx, "AddRouteForwarding(ctx, '%s', '%s', %s)", srcPath, outputFactory, publishMode)
	defer func() {
		logger.Debugf(ctx, "/AddRouteForwarding(ctx, '%s', '%s', %s): %p %v", srcPath, outputFactory, publishMode, _ret, _err)
	}()
	ctx = belt.WithField(ctx, "src_path", srcPath)

	fwd := &RouteForwarding[T]{
		Router:              r,
		SrcPath:             srcPath,
		GetSrcRouteMode:     getSrcRouteMode,
		OutputFactory:       outputFactory,
		PublishMode:         publishMode,
		TranscoderConfig:    transcoderConfig,
		FilterKernelFactory: filterKernelFactory,
	}
	if err := fwd.open(ctx); err != nil {
		return nil, fmt.Errorf("unable to initialize: %w", err)
	}

	return fwd, nil
}

func (fwd *RouteForwarding[T]) GetPublishMode(ctx context.Context) PublishMode {
	return fwd.PublishMode
}

func (fwd *RouteForwarding[T]) open(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "open")
	defer func() { logger.Debugf(ctx, "/open: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.openLocked, ctx)
}

// openLocked derives the forwarder's lifetime ctx from the caller's
// ctx and stores the cancel function. The forwarder's watcher
// goroutine in startLocked selects on ctx.Done() and tears down on
// cancellation, so the caller is responsible for passing a ctx whose
// lifetime corresponds to the desired forwarder lifetime.
//
// In particular: a forwarder that needs to outlive any single
// publisher/consumer request handler MUST be created with a ctx
// detached from that handler's ctx — wrap the request-scoped ctx with
// xcontext.DetachDone or context.WithoutCancel (Go 1.21+) so that
// disconnect of the originating request does not cancel the
// forwarder. Without the detach, the watcher takes the <-ctx.Done()
// branch on disconnect, exits, and any sync.Once-guarded re-wire path
// in the caller will short-circuit subsequent activations — leaving
// the route node alive but the forwarder dead.
func (fwd *RouteForwarding[T]) openLocked(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "openLocked")
	defer func() { logger.Debugf(ctx, "/openLocked: %v", _err) }()
	if fwd.CancelFunc != nil {
		return fmt.Errorf("internal error: already started")
	}

	ctx, cancelFn := context.WithCancel(ctx)
	fwd.CancelFunc = cancelFn

	return fwd.startLocked(ctx)
}

func (fwd *RouteForwarding[T]) start(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "start")
	defer func() { logger.Debugf(ctx, "/start: %v", _err) }()
	return xsync.DoA1R1(xsync.WithEnableDeadlock(ctx, false), &fwd.Locker, fwd.startLocked, ctx)
}

func (fwd *RouteForwarding[T]) startLocked(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "startLocked: %p", fwd)
	defer func() { logger.Debugf(ctx, "/startLocked: %p: %v", fwd, _err) }()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fwd.WaitGroup.Add(1)
	defer fwd.WaitGroup.Done()
	defer func() {
		if _err != nil {
			fwd.stopLocked(ctx)
		}
	}()

	src, err := fwd.Router.GetRoute(ctx, fwd.SrcPath, fwd.GetSrcRouteMode)
	if err != nil {
		return fmt.Errorf("internal error: unable to get the source route by path '%s': %w", fwd.SrcPath, err)
	}
	if src == nil {
		return fmt.Errorf("internal error: there is no active route by path '%s' (source)", fwd.SrcPath)
	}
	logger.Debugf(ctx, "route instance: %p", src)

	fwd.WaitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer fwd.WaitGroup.Done()
		logger.Debugf(ctx, "waiter")
		defer logger.Debugf(ctx, "/waiter")
		ch := src.getPublishersChangeChan(ctx)
		for {
			logger.Debugf(ctx, "waiter: waiting")
			select {
			case <-ctx.Done():
				logger.Debugf(ctx, "<-ctx.Done()")
				if err := fwd.stop(ctx); err != nil {
					logger.Errorf(ctx, "unable to stop: %v", err)
				}
				return
			case <-ch:
				ch = src.getPublishersChangeChan(ctx)
				isStillOpen := src.IsOpen(ctx)
				logger.Debugf(ctx, "<-src[%s].PublishersChangeChan: %t", src, isStillOpen)
				if isStillOpen {
					continue
				}
				logger.Debugf(ctx, "the route instance %p is closed, restarting the forwarder to get a new route node (for the same route path)", src)
				if err := fwd.stop(ctx); err != nil {
					logger.Errorf(ctx, "unable to stop: %v", err)
				}
				if err := fwd.start(ctx); err != nil {
					logger.Errorf(ctx, "unable to start: %v", err)
				}
				return
			}
		}
	})

	logger.Tracef(ctx, "fwd.OutputFactory.NewOutput(ctx, %s)", fwd)
	dstNode, err := fwd.OutputFactory.NewOutput(ctx, fwd)
	logger.Tracef(ctx, "/fwd.OutputFactory.NewOutput(ctx, %s): %v %v", fwd, dstNode, err)
	if err != nil {
		return fmt.Errorf("unable to open the output: %w", err)
	}
	fwd.Output = dstNode

	f, err := NewStreamForwarder(ctx, src.Node, dstNode, fwd.TranscoderConfig, fwd.FilterKernelFactory, nil)
	if err != nil {
		return fmt.Errorf("unable to initialize a forwarder from '%s' to '%s' (%#+v): %w", src.Path, dstNode, fwd.TranscoderConfig, err)
	}
	fwd.StreamForwarder = f

	if err := fwd.StreamForwarder.Start(ctx); err != nil {
		return fmt.Errorf("unable to start stream forwarding: %w", err)
	}

	return nil
}

func (fwd *RouteForwarding[T]) stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "stop")
	defer func() { logger.Debugf(ctx, "/stop: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.stopLocked, ctx)
}

func (fwd *RouteForwarding[T]) stopLocked(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "stopLocked")
	defer func() { logger.Debugf(ctx, "/stopLocked: %v", _err) }()

	fwd.WaitGroup.Add(1)
	defer fwd.WaitGroup.Done()
	var errs []error
	if fwd.StreamForwarder != nil {
		if err := fwd.StreamForwarder.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("fwd.Forwarder.Stop: %w", err))
		}
		fwd.StreamForwarder = nil
	}
	if fwd.Output != nil {
		output := fwd.Output
		// Close synchronously so the error becomes part of the returned
		// error set. Previously this ran in a goroutine tracked by the
		// caller's wg, but the caller's wg.Wait is deferred AFTER the
		// return, so any error written there could never reach the
		// caller. The output's Close uses its own locker (distinct from
		// fwd.Locker) so this does not introduce a lock-order cycle.
		if err := output.Close(ctx); err != nil {
			logger.Errorf(ctx, "fwd.Output.Close: %v", err)
			errs = append(errs, fmt.Errorf("fwd.Output.Close: %w", err))
		}
		fwd.Output = nil
	}
	return errors.Join(errs...)
}

func (fwd *RouteForwarding[T]) Close(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	defer fwd.WaitGroup.Wait()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.doCloseLocked, ctx)
}

func (fwd *RouteForwarding[T]) doCloseLocked(
	ctx context.Context,
) (_err error) {
	if fwd.CancelFunc == nil {
		return nil
	}
	fwd.CancelFunc()
	fwd.CancelFunc = nil
	err := fwd.stopLocked(ctx)
	if err != nil {
		return err
	}
	return nil
}

// Activate satisfies the OnDemandActivator contract at the
// RouteForwarding level. It delegates to the inner StreamForwarder if
// that forwarder implements OnDemandActivator, which is the case when a
// TranscoderConfig was provided (StreamForwarderTranscoding). Callers
// use this to bring a stopped on-demand transcoder online without
// needing to hold a direct pointer to the StreamForwarder.
func (fwd *RouteForwarding[T]) Activate(ctx context.Context) error {
	activator, ok := fwd.onDemandActivator(ctx)
	if !ok {
		return fmt.Errorf("inner StreamForwarder does not implement OnDemandActivator")
	}
	return activator.Activate(ctx)
}

// Deactivate satisfies the OnDemandActivator contract at the
// RouteForwarding level. See Activate for details.
func (fwd *RouteForwarding[T]) Deactivate(ctx context.Context) error {
	activator, ok := fwd.onDemandActivator(ctx)
	if !ok {
		return fmt.Errorf("inner StreamForwarder does not implement OnDemandActivator")
	}
	return activator.Deactivate(ctx)
}

func (fwd *RouteForwarding[T]) onDemandActivator(
	ctx context.Context,
) (OnDemandActivator, bool) {
	return xsync.DoR2(ctx, &fwd.Locker, func() (OnDemandActivator, bool) {
		if fwd.StreamForwarder == nil {
			return nil, false
		}
		activator, ok := fwd.StreamForwarder.(OnDemandActivator)
		return activator, ok
	})
}

func (fwd *RouteForwarding[T]) String() string {
	switch {
	case fwd.Input != nil && fwd.Output != nil:
		return fmt.Sprintf("fwd('%s'->'%s')", fwd.Input.Path, fwd.Output)
	case fwd.Input != nil:
		return fmt.Sprintf("fwd('%s'->?)", fwd.Input.Path)
	case fwd.Output != nil:
		return fmt.Sprintf("fwd(?->'%s')", fwd.Output)
	default:
		return "fwd(?->?)"
	}
}
