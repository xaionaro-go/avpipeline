package router

import (
	"context"
	"errors"
	"fmt"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	transcodertypes "github.com/xaionaro-go/avpipeline/chain/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/xsync"
)

type forwardOutputFactoryLocalPath struct {
	Router    *Router
	RoutePath RoutePath
}

var _ ForwardOutputFactory = (*forwardOutputFactoryLocalPath)(nil)

func newForwardOutputFactoryLocalPath(
	Router *Router,
	RoutePath RoutePath,
) *forwardOutputFactoryLocalPath {
	return &forwardOutputFactoryLocalPath{
		Router:    Router,
		RoutePath: RoutePath,
	}
}

func (r *Router) AddRouteForwardingLocal(
	ctx context.Context,
	srcPath RoutePath,
	dstPath RoutePath,
	recoderConfig *transcodertypes.RecoderConfig,
) (_ret *RouteForwarding, _err error) {
	logger.Debugf(ctx, "RouteForwarding(ctx, '%s', '%s', %#+v)", srcPath, dstPath, recoderConfig)
	defer func() {
		logger.Debugf(ctx, "/RouteForwarding(ctx, '%s', '%s', %#+v): %p %v", srcPath, dstPath, recoderConfig, _ret, _err)
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
		newForwardOutputFactoryLocalPath(r, dstPath),
		recoderConfig,
	)
}

func (f *forwardOutputFactoryLocalPath) NewOutput(
	ctx context.Context,
	fwd *RouteForwarding,
) (_ret NodeForwardingOutput, _err error) {
	outputRoute, err := f.Router.GetRoute(ctx, f.RoutePath, GetRouteModeCreateIfNotFound)
	if err != nil {
		return nil, fmt.Errorf("unable to get the source route by path '%s': %w", f.RoutePath, err)
	}
	if outputRoute == nil {
		return nil, fmt.Errorf("there is no active route by path '%s' (source)", f.RoutePath)
	}
	if err := outputRoute.AddPublisher(ctx, fwd); err != nil {
		return nil, fmt.Errorf("unable to add the forwarder as a publisher to '%s': %w", outputRoute.Path, err)
	}
	return &forwardOutputNodeLocalPath{
		NodeRouting:     outputRoute.Node,
		RouteForwarding: fwd,
	}, nil
}

type forwardOutputNodeLocalPath struct {
	*NodeRouting
	RouteForwarding *RouteForwarding
}

func (n *forwardOutputNodeLocalPath) String() string {
	return n.NodeRouting.CustomData.String()
}

func (n *forwardOutputNodeLocalPath) Close(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	var errs []error
	dstRoute := n.NodeRouting.CustomData
	dstRoute.Node.Locker.Do(ctx, func() {
		if _, err := dstRoute.removePublisherLocked(ctx, n.RouteForwarding); err != nil {
			errs = append(errs, fmt.Errorf("dstRoute.removePublisher: %w", err))
		}
	})
	return errors.Join(errs...)
}

func (n *forwardOutputNodeLocalPath) GetOutputRoute(
	ctx context.Context,
) *Route {
	return n.NodeRouting.CustomData
}

var _ Publisher = (*RouteForwarding)(nil)

func (fwd *RouteForwarding) GetInputNode(
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

func (fwd *RouteForwarding) GetOutputRoute(
	ctx context.Context,
) *Route {
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.getOutputRouteLocked, ctx)
}

func (fwd *RouteForwarding) getOutputRouteLocked(
	ctx context.Context,
) *Route {
	if fwd.Output == nil {
		return nil
	}
	return fwd.Output.GetOutputRoute(ctx)
}
