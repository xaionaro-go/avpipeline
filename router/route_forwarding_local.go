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
	"github.com/xaionaro-go/xsync"
)

type forwardOutputFactoryLocalPath[T any] struct {
	Router    *Router[T]
	RoutePath RoutePath
}

var _ ForwardOutputFactory[any] = (*forwardOutputFactoryLocalPath[any])(nil)

func newForwardOutputFactoryLocalPath[T any](
	Router *Router[T],
	RoutePath RoutePath,
) *forwardOutputFactoryLocalPath[T] {
	return &forwardOutputFactoryLocalPath[T]{
		Router:    Router,
		RoutePath: RoutePath,
	}
}

func (ff *forwardOutputFactoryLocalPath[T]) String() string {
	return string(ff.RoutePath)
}

func (r *Router[T]) AddRouteForwardingLocal(
	ctx context.Context,
	srcPath RoutePath,
	dstPath RoutePath,
	publishMode PublishMode,
	recoderConfig *transcodertypes.RecoderConfig,
) (_ret *RouteForwarding[T], _err error) {
	logger.Debugf(ctx, "AddRouteForwardingLocal(ctx, '%s', '%s', %s, %#+v)", srcPath, dstPath, publishMode, recoderConfig)
	defer func() {
		logger.Debugf(ctx, "/AddRouteForwardingLocal(ctx, '%s', '%s', %s, %#+v): %p %v", srcPath, dstPath, publishMode, recoderConfig, _ret, _err)
	}()
	ctx = belt.WithField(ctx, "src_path", srcPath)
	ctx = belt.WithField(ctx, "dst_path", dstPath)

	// Overall what will happen is:
	//
	// In case of recoderConfig != nil:
	// * `RouteForwarding` will wait for a publisher on the source route;
	// * and it will get the source and destination routes.
	// * `RouteForwarding` will add itself as the publisher in the destination route.
	// * `RouteForwarding` will create and start a `StreamForwarderRecoding`.
	// * `StreamForwarderRecoding` will create a `TranscoderWithPassthrough`.
	// * `StreamForwarderRecoding` will subscribe `TranscoderWithPassthrough`'s input to source route node traffic.
	// * `StreamForwarderRecoding` will use the `TranscoderWithPassthrough` to push it to the Output.
	return r.AddRouteForwarding(
		ctx,
		srcPath,
		GetRouteModeCreateTemporaryIfNotFound,
		newForwardOutputFactoryLocalPath(r, dstPath),
		publishMode,
		recoderConfig,
	)
}

func (f *forwardOutputFactoryLocalPath[T]) NewOutput(
	ctx context.Context,
	fwd *RouteForwarding[T],
) (_ret NodeForwardingOutput[T], _err error) {
	logger.Tracef(ctx, "NewOutput(ctx, %s)", fwd)
	defer func() { logger.Tracef(ctx, "/NewOutput(ctx, %s): %v %v", fwd, _ret, _err) }()
	outputRoute, err := f.Router.GetRoute(ctx, f.RoutePath, GetRouteModeCreateTemporaryIfNotFound)
	if err != nil {
		return nil, fmt.Errorf("unable to get the source route by path '%s': %w", f.RoutePath, err)
	}
	if outputRoute == nil {
		return nil, fmt.Errorf("there is no active route by path '%s' (source)", f.RoutePath)
	}
	logger.Tracef(ctx, "AddPublisher")
	if _, err := outputRoute.AddPublisher(ctx, fwd); err != nil {
		return nil, fmt.Errorf("unable to add the forwarder as a publisher to '%s': %w", outputRoute.Path, err)
	}
	return &forwardOutputNodeLocalPath[T]{
		NodeRouting:     outputRoute.Node,
		RouteForwarding: fwd,
	}, nil
}

type forwardOutputNodeLocalPath[T any] struct {
	*NodeRouting[T]
	RouteForwarding *RouteForwarding[T]
}

func (n *forwardOutputNodeLocalPath[T]) String() string {
	return n.NodeRouting.CustomData.(*Route[T]).String()
}

func (n *forwardOutputNodeLocalPath[T]) Close(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	var errs []error
	var wg sync.WaitGroup
	defer wg.Wait()
	dstRoute := n.NodeRouting.CustomData
	dstRoute.(*Route[T]).Node.Locker.Do(ctx, func() {
		if _, err := dstRoute.(*Route[T]).RemovePublisherLocked(ctx, n.RouteForwarding, &wg); err != nil {
			errs = append(errs, fmt.Errorf("dstRoute.removePublisher: %w", err))
		}
	})
	return errors.Join(errs...)
}

func (n *forwardOutputNodeLocalPath[T]) GetOutputRoute(
	ctx context.Context,
) *Route[T] {
	return n.NodeRouting.CustomData.(*Route[T])
}

var _ Publisher[any] = (*RouteForwarding[any])(nil)

func (fwd *RouteForwarding[T]) GetInputNode(
	ctx context.Context,
) node.Abstract {
	return xsync.DoR1(ctx, &fwd.Input.Node.Locker, func() node.Abstract {
		if len(fwd.Input.Publishers) == 0 {
			return nil
		}
		if len(fwd.Input.Publishers) > 1 {
			logger.Errorf(ctx, "source '%s' has more than one publisher; which is not well supported by a local forwarder, yet", fwd.Input.Path)
		}
		publisher := fwd.Input.Publishers[0]
		return publisher.GetInputNode(ctx)
	})
}

func (fwd *RouteForwarding[T]) GetOutputRoute(
	ctx context.Context,
) *Route[T] {
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.getOutputRouteLocked, ctx)
}

func (fwd *RouteForwarding[T]) getOutputRouteLocked(
	ctx context.Context,
) *Route[T] {
	if fwd.Output == nil {
		return nil
	}
	return fwd.Output.GetOutputRoute(ctx)
}
