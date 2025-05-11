package router

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	transcodertypes "github.com/xaionaro-go/avpipeline/chain/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type NodeForwardingOutput interface {
	node.Abstract
	types.Closer
	GetOutputRoute(ctx context.Context) *Route
}

type ForwardOutputFactory interface {
	NewOutput(ctx context.Context, fwd *RouteForwarding) (NodeForwardingOutput, error)
}

type RouteForwarding struct {
	Router        *Router
	SrcPath       RoutePath
	OutputFactory ForwardOutputFactory
	RecoderConfig *transcodertypes.RecoderConfig
	Locker        xsync.Mutex
	CancelFunc    context.CancelFunc
	Input         *Route
	Output        NodeForwardingOutput
	WaitGroup     sync.WaitGroup
	StreamForwarder
}

func (r *Router) AddRouteForwarding(
	ctx context.Context,
	srcPath RoutePath,
	outputFactory ForwardOutputFactory,
	recoderConfig *transcodertypes.RecoderConfig,
) (_ret *RouteForwarding, _err error) {
	logger.Debugf(ctx, "AddRouteForwarding(ctx, '%s', %#+v)", srcPath, outputFactory)
	defer func() {
		logger.Debugf(ctx, "/AddRouteForwarding(ctx, '%s', %#+v): %p %v", srcPath, outputFactory, _ret, _err)
	}()
	ctx = belt.WithField(ctx, "src_path", srcPath)

	fwd := &RouteForwarding{
		Router:        r,
		SrcPath:       srcPath,
		OutputFactory: outputFactory,
		RecoderConfig: recoderConfig,
	}
	if err := fwd.open(ctx); err != nil {
		return nil, fmt.Errorf("unable to initialize: %w", err)
	}

	return fwd, nil
}

func (fwd *RouteForwarding) open(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "open")
	defer func() { logger.Debugf(ctx, "/open: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.openLocked, ctx)
}

func (fwd *RouteForwarding) openLocked(ctx context.Context) (_err error) {
	if fwd.CancelFunc != nil {
		return fmt.Errorf("internal error: already started")
	}

	ctx, cancelFn := context.WithCancel(ctx)
	fwd.CancelFunc = cancelFn

	return fwd.startLocked(ctx)
}

func (fwd *RouteForwarding) start(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "start")
	defer func() { logger.Debugf(ctx, "/start: %v", _err) }()
	return xsync.DoA1R1(xsync.WithEnableDeadlock(ctx, false), &fwd.Locker, fwd.startLocked, ctx)
}

func (fwd *RouteForwarding) startLocked(ctx context.Context) (_err error) {
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

	src, err := fwd.Router.GetRoute(ctx, fwd.SrcPath, GetRouteModeWaitForPublisher)
	if err != nil {
		return fmt.Errorf("unable to get the source route by path '%s': %w", fwd.SrcPath, err)
	}
	if src == nil {
		return fmt.Errorf("there is no active route by path '%s' (source)", fwd.SrcPath)
	}

	fwd.WaitGroup.Add(1)
	observability.Go(ctx, func() {
		defer fwd.WaitGroup.Done()
		logger.Debugf(ctx, "waiter")
		defer logger.Debugf(ctx, "/waiter")
		select {
		case <-ctx.Done():
			if err := fwd.stop(ctx); err != nil {
				logger.Errorf(ctx, "unable to stop: %v", err)
			}
			return
		case <-src.PublishersChangeChan:
			if err := fwd.stop(ctx); err != nil {
				logger.Errorf(ctx, "unable to stop: %v", err)
				return
			}
			if fwd.start(ctx); err != nil {
				logger.Error(ctx, "unable to start: %v", err)
				return
			}
		}
	})

	dstNode, err := fwd.OutputFactory.NewOutput(ctx, fwd)
	if err != nil {
		return fmt.Errorf("unable to open the output: %w", err)
	}
	fwd.Output = dstNode

	f, err := NewStreamForwarder(ctx, src.Node, dstNode, fwd.RecoderConfig)
	if err != nil {
		return fmt.Errorf("unable to initialize a forwarder from '%s' to '%s' (%#+v): %w", src.Path, dstNode, fwd.RecoderConfig, err)
	}
	fwd.StreamForwarder = f

	if err := fwd.StreamForwarder.Start(ctx); err != nil {
		return fmt.Errorf("unable to start stream forwarding: %w", err)
	}

	return nil
}

func (fwd *RouteForwarding) stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "stop")
	defer func() { logger.Debugf(ctx, "/stop: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.stopLocked, ctx)
}

func (fwd *RouteForwarding) stopLocked(
	ctx context.Context,
) (_err error) {
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
		if err := fwd.Output.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("fwd.Output.Close: %w", err))
		}
		fwd.Output = nil
	}
	return errors.Join(errs...)
}

func (fwd *RouteForwarding) Close(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.closeLocked, ctx)
}

func (fwd *RouteForwarding) closeLocked(
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
	fwd.WaitGroup.Wait()
	return nil
}

func (fwd *RouteForwarding) String() string {
	return fmt.Sprintf("fwd('%s'->'%s')", fwd.Input.Path, fwd.Output)
}
