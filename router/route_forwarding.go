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
	Router           *Router[T]
	SrcPath          RoutePath
	GetSrcRouteMode  GetRouteMode
	OutputFactory    ForwardOutputFactory[T]
	PublishMode      PublishMode
	TranscoderConfig *transcodertypes.TranscoderConfig
	Locker           xsync.Mutex
	CancelFunc       context.CancelFunc
	Input            *Route[T]
	Output           NodeForwardingOutput[T]
	WaitGroup        sync.WaitGroup
	StreamForwarder[GoBug63285RouteInterface[T], *ProcessorRouting]
}

func (r *Router[T]) AddRouteForwarding(
	ctx context.Context,
	srcPath RoutePath,
	getSrcRouteMode GetRouteMode,
	outputFactory ForwardOutputFactory[T],
	publishMode PublishMode,
	transcoderConfig *transcodertypes.TranscoderConfig,
) (_ret *RouteForwarding[T], _err error) {
	logger.Debugf(ctx, "AddRouteForwarding(ctx, '%s', '%s', %s)", srcPath, outputFactory, publishMode)
	defer func() {
		logger.Debugf(ctx, "/AddRouteForwarding(ctx, '%s', '%s', %s): %p %v", srcPath, outputFactory, publishMode, _ret, _err)
	}()
	ctx = belt.WithField(ctx, "src_path", srcPath)

	fwd := &RouteForwarding[T]{
		Router:           r,
		SrcPath:          srcPath,
		GetSrcRouteMode:  getSrcRouteMode,
		OutputFactory:    outputFactory,
		PublishMode:      publishMode,
		TranscoderConfig: transcoderConfig,
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
			var wg sync.WaitGroup
			fwd.stopLocked(ctx, &wg)
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
		for {
			logger.Debugf(ctx, "waiter: waiting")
			select {
			case <-ctx.Done():
				logger.Debugf(ctx, "<-ctx.Done()")
				if err := fwd.stop(ctx); err != nil {
					logger.Errorf(ctx, "unable to stop: %v", err)
				}
				return
			case <-src.PublishersChangeChan:
				isStillOpen := src.IsOpen(ctx)
				logger.Debugf(ctx, "<-src[%s].PublishersChangeChan: %t", src, isStillOpen)
				if isStillOpen {
					continue
				}
				logger.Debugf(ctx, "the route instance %p is closed, restarting the forwarder to get a new route node (for the same route path)", src)
				if err := fwd.stop(ctx); err != nil {
					logger.Errorf(ctx, "unable to stop: %v", err)
				}
				if fwd.start(ctx); err != nil {
					logger.Error(ctx, "unable to start: %v", err)
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

	f, err := NewStreamForwarder(ctx, src.Node, dstNode, fwd.TranscoderConfig)
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
	var wg sync.WaitGroup
	defer wg.Wait()
	return xsync.DoA2R1(ctx, &fwd.Locker, fwd.stopLocked, ctx, &wg)
}

func (fwd *RouteForwarding[T]) stopLocked(
	ctx context.Context,
	wg *sync.WaitGroup,
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
		wg.Add(1)
		output := fwd.Output
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			if err := output.Close(ctx); err != nil {
				logger.Errorf(ctx, "fwd.Output.Close: %v", err)
			}
		})
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
	var wg sync.WaitGroup
	defer wg.Wait()
	return xsync.DoA2R1(ctx, &fwd.Locker, fwd.doCloseLocked, ctx, &wg)
}

func (fwd *RouteForwarding[T]) doCloseLocked(
	ctx context.Context,
	wg *sync.WaitGroup,
) (_err error) {
	if fwd.CancelFunc == nil {
		return nil
	}
	fwd.CancelFunc()
	fwd.CancelFunc = nil
	err := fwd.stopLocked(ctx, wg)
	if err != nil {
		return err
	}
	return nil
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
