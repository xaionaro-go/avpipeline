package router

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/node"
	transcodertypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/xsync"
)

type RouteSource[C any, P processor.Abstract] struct {
	Router        *Router
	DstPath       RoutePath
	RecoderConfig *transcodertypes.RecoderConfig
	Locker        xsync.Mutex
	CancelFunc    context.CancelFunc
	Input         *node.NodeWithCustomData[C, P]
	Output        *Route
	WaitGroup     sync.WaitGroup
	StreamForwarder[C, P]
}

func AddRouteSource[C any, P processor.Abstract](
	ctx context.Context,
	r *Router,
	srcNode *node.NodeWithCustomData[C, P],
	dstPath RoutePath,
	recoderConfig *transcodertypes.RecoderConfig,
) (_ret *RouteSource[C, P], _err error) {
	logger.Debugf(ctx, "AddRouteSource(ctx, %#+v, '%s')", srcNode, dstPath)
	defer func() {
		logger.Debugf(ctx, "/AddRouteSource(ctx, %#+v, '%s'): %p %v", srcNode, dstPath, _ret, _err)
	}()
	ctx = belt.WithField(ctx, "dst_path", dstPath)

	fwd := &RouteSource[C, P]{
		Router:        r,
		Input:         srcNode,
		DstPath:       dstPath,
		RecoderConfig: recoderConfig,
	}
	if err := fwd.open(ctx); err != nil {
		return nil, fmt.Errorf("unable to initialize: %w", err)
	}

	return fwd, nil
}

func (fwd *RouteSource[C, P]) open(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "open")
	defer func() { logger.Debugf(ctx, "/open: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.openLocked, ctx)
}

func (fwd *RouteSource[C, P]) openLocked(ctx context.Context) (_err error) {
	if fwd.CancelFunc != nil {
		return fmt.Errorf("internal error: already started")
	}

	ctx, cancelFn := context.WithCancel(ctx)
	fwd.CancelFunc = cancelFn

	return fwd.startLocked(ctx)
}

func (fwd *RouteSource[C, P]) Start(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Start")
	defer func() { logger.Debugf(ctx, "/Start: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.startLocked, ctx)
}

func (fwd *RouteSource[C, P]) startLocked(ctx context.Context) (_err error) {
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

	dst, err := fwd.Router.GetRoute(ctx, fwd.DstPath, GetRouteModeCreateIfNotFound)
	if err != nil {
		return fmt.Errorf("unable to get the destination route by path '%s': %w", fwd.DstPath, err)
	}

	logger.Debugf(ctx, "AddPublisher")
	if err := dst.AddPublisher(ctx, fwd); err != nil {
		return fmt.Errorf("unable to add the forwarder as a publisher to '%s': %w", dst.Path, err)
	}

	f, err := NewStreamForwarder(ctx, fwd.Input, dst.Node, fwd.RecoderConfig)
	if err != nil {
		return fmt.Errorf("unable to initialize a forwarder from %T to '%s' (%#+v): %w", fwd.Input, dst.Path, fwd.RecoderConfig, err)
	}
	fwd.StreamForwarder = f

	if err := fwd.StreamForwarder.Start(ctx); err != nil {
		return fmt.Errorf("unable to start stream forwarding: %w", err)
	}

	return nil
}

func (fwd *RouteSource[C, P]) Stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Stop")
	defer func() { logger.Debugf(ctx, "/Stop: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.stopLocked, ctx)
}

func (fwd *RouteSource[C, P]) stopLocked(
	ctx context.Context,
) (_err error) {
	fwd.WaitGroup.Add(1)
	defer fwd.WaitGroup.Done()
	var errs []error
	if fwd.StreamForwarder != nil {
		if err := fwd.StreamForwarder.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("fwd.StreamForwarder.Stop: %w", err))
		}
		fwd.StreamForwarder = nil
	}

	if fwd.Output != nil {
		if _, err := fwd.Output.RemovePublisher(ctx, fwd); err != nil {
			errs = append(errs, fmt.Errorf("fwd.Output.RemovePublisher: %w", err))
		}
		fwd.Output = nil
	}

	return errors.Join(errs...)
}

func (fwd *RouteSource[C, P]) Close(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.closeLocked, ctx)
}

func (fwd *RouteSource[C, P]) closeLocked(
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

func (fwd *RouteSource[C, P]) String() string {
	return fmt.Sprintf("fwd('%s'->'%s')", fwd.Input, fwd.Output)
}

var _ Publisher = (*RouteForwarding)(nil)

func (fwd *RouteSource[C, P]) GetInputNode(
	ctx context.Context,
) node.Abstract {
	return fwd.Input
}

func (fwd *RouteSource[C, P]) GetOutputRoute(
	ctx context.Context,
) *Route {
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.getOutputRouteLocked, ctx)
}

func (fwd *RouteSource[C, P]) getOutputRouteLocked(
	ctx context.Context,
) *Route {
	return fwd.Output
}
