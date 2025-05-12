package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type Router struct {
	OnRouteCreated func(context.Context, *Route)
	OnRouteRemoved func(context.Context, *Route)

	WaitGroup sync.WaitGroup

	Locker xsync.Mutex

	// access only when Locker is locked:
	RoutesByPath map[RoutePath]*Route
	CloseChan    chan struct{}
	AddedChan    chan struct{}
	ErrorChan    chan node.Error
}

func New(ctx context.Context) *Router {
	r := &Router{
		RoutesByPath: map[RoutePath]*Route{},
		CloseChan:    make(chan struct{}),
		AddedChan:    make(chan struct{}),
		ErrorChan:    make(chan node.Error, 100),
	}
	r.init(ctx)
	return r
}

func (r *Router) Close(
	ctx context.Context,
) error {
	r.Locker.Do(ctx, func() { // to make sure we don't have anybody adding more processes
		close(r.CloseChan)
		r.WaitGroup.Wait()
		close(r.ErrorChan)
		close(r.AddedChan)
	})
	return nil
}

func (r *Router) init(
	ctx context.Context,
) {
	observability.Go(ctx, func() {
		for err := range r.ErrorChan {
			route := err.Node.(*NodeRouting).CustomData
			if route == nil {
				logger.Errorf(ctx, "got an error on node %p: %v", err.Node, err.Err)
				continue
			}
			belt.WithField(ctx, "route_path", route.Path)
			if errors.Is(err.Err, context.Canceled) {
				logger.Debugf(ctx, "Cancelled: %v", err)
				continue
			}
			if errors.Is(err.Err, io.EOF) {
				logger.Debugf(ctx, "EOF: %v", err)
				continue
			}
			logger.Errorf(ctx, "got an error on node %p (path: '%s'): %v", err.Node, route.Path, err)
		}
	})
}

func (r *Router) onRouteCreated(
	ctx context.Context,
	route *Route,
) {
	logger.Debugf(ctx, "onRouteCreated: %s", route)
	defer func() { logger.Debugf(ctx, "/onRouteCreated: %s", route) }()
	r.WaitGroup.Add(1)
	if r.OnRouteCreated != nil {
		r.OnRouteCreated(ctx, route)
	}
}

func (r *Router) onRouteRemoved(
	ctx context.Context,
	route *Route,
) {
	logger.Debugf(ctx, "onRouteRemoved: %s", route)
	defer func() { logger.Debugf(ctx, "/onRouteRemoved: %s", route) }()
	r.WaitGroup.Done()
	if r.OnRouteRemoved != nil {
		r.OnRouteRemoved(ctx, route)
	}
}

type GetRouteMode int

const (
	GetRouteModeFailIfNotFound = GetRouteMode(iota)
	GetRouteModeWaitUntilCreated
	GetRouteModeWaitForPublisher
	GetRouteModeCreateIfNotFound
	GetRouteModeCreate
)

func (m GetRouteMode) String() string {
	switch m {
	case GetRouteModeFailIfNotFound:
		return "fail-if-no-found"
	case GetRouteModeWaitUntilCreated:
		return "wait-until-created"
	case GetRouteModeWaitForPublisher:
		return "wait-for-publisher"
	case GetRouteModeCreateIfNotFound:
		return "create-if-not-found"
	case GetRouteModeCreate:
		return "create"
	default:
		return fmt.Sprintf("unknown-mode-%d", int(m))
	}
}

func (r *Router) GetRoute(
	ctx context.Context,
	path RoutePath,
	mode GetRouteMode,
) (_ret *Route, _err error) {
	logger.Debugf(ctx, "GetRoute(ctx, '%s', '%s')", path, mode)
	defer func() { logger.Debugf(ctx, "/GetRoute(ctx, '%s', '%s'): %v %v", path, mode, _ret, _err) }()
	return xsync.DoR2(ctx, &r.Locker, func() (*Route, error) {
		return r.getRouteLocked(ctx, path, mode)
	})
}

func (r *Router) getRouteLocked(
	ctx context.Context,
	path RoutePath,
	mode GetRouteMode,
) (*Route, error) {
	curRoute := r.RoutesByPath[path]
	if curRoute != nil {
		switch mode {
		case GetRouteModeFailIfNotFound:
			return curRoute, nil
		case GetRouteModeWaitUntilCreated:
			return curRoute, nil
		case GetRouteModeWaitForPublisher:
			var err error
			r.Locker.UDo(ctx, func() {
				_, err = curRoute.WaitForPublisher(ctx)
			})
			if !errors.Is(err, io.ErrClosedPipe) {
				if err != nil {
					return nil, fmt.Errorf("unable to wait for a publisher: %w", err)
				}
				return curRoute, nil
			}
		case GetRouteModeCreateIfNotFound:
			return curRoute, nil
		case GetRouteModeCreate:
			return nil, fmt.Errorf("the route '%s' already exists", path)
		}
		return nil, fmt.Errorf("unknown mode: %d (%s)", int(mode), mode)
	}

	switch mode {
	case GetRouteModeFailIfNotFound:
		return nil, fmt.Errorf("route '%s' found", path)
	case GetRouteModeWaitUntilCreated:
		var route *Route
		var err error
		r.Locker.UDo(ctx, func() {
			route, err = r.WaitForRoute(ctx, path)
		})
		if err != nil {
			return nil, fmt.Errorf("unable to wait for route '%s': %w", path, err)
		}
		return route, nil
	case GetRouteModeWaitForPublisher:
		var route *Route
		for {
			var err error
			r.Locker.UDo(ctx, func() {
				route, err = r.WaitForRoute(ctx, path)
			})
			if err != nil {
				return nil, fmt.Errorf("unable to wait for route '%s': %w", path, err)
			}
			r.Locker.UDo(ctx, func() {
				_, err = route.WaitForPublisher(ctx)
			})
			if err != nil {
				return nil, fmt.Errorf("unable to wait for a publisher: %w", err)
			}
			if errors.Is(err, io.ErrClosedPipe) {
				continue
			}
			break
		}
		return route, nil
	case GetRouteModeCreateIfNotFound:
		return r.createRoute(ctx, path), nil
	case GetRouteModeCreate:
		return r.createRoute(ctx, path), nil
	}
	return nil, fmt.Errorf("unknown mode: %d (%s)", int(mode), mode)
}

func (r *Router) createRoute(
	ctx context.Context,
	path RoutePath,
) (_ret *Route) {
	logger.Debugf(ctx, "createRoute(ctx, '%s')", path)
	defer func() { logger.Debugf(ctx, "/createRoute(ctx, '%s'): %v", path, _ret) }()

	route := r.RoutesByPath[path]
	if route != nil {
		return route
	}

	select {
	case <-r.CloseChan:
		return nil
	default:
	}
	route = newRoute(
		ctx,
		path,
		r.ErrorChan,
		r.onRouteCreated,
		r.onRouteRemoved,
	)
	r.RoutesByPath[path] = route
	var addCh chan<- struct{}
	addCh, r.AddedChan = r.AddedChan, make(chan struct{})
	close(addCh)

	return route
}

func (r *Router) GetAddedChan(
	ctx context.Context,
) chan struct{} {
	return xsync.DoR1(ctx, &r.Locker, func() chan struct{} {
		return r.AddedChan
	})
}

func (r *Router) RemoveRoute(
	ctx context.Context,
	path RoutePath,
) *Route {
	return xsync.DoA2R1(ctx, &r.Locker, r.removeRouteNoLock, ctx, path)
}

func (r *Router) removeRouteNoLock(
	ctx context.Context,
	path RoutePath,
) *Route {
	route := r.RoutesByPath[path]
	delete(r.RoutesByPath, path)
	return route
}

func (r *Router) WaitForRoute(
	ctx context.Context,
	path RoutePath,
) (*Route, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.GetAddedChan(ctx):
			if route := xsync.DoR1(ctx, &r.Locker, func() *Route {
				return r.RoutesByPath[path]
			}); route != nil {
				return route, nil
			}
		}
	}
}

func (r *Router) Wait(ctx context.Context) error {
	logger.Debugf(ctx, "Wait")
	defer func() { logger.Debugf(ctx, "/Wait") }()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.CloseChan:
	}

	endCh := make(chan struct{})
	observability.Go(ctx, func() { // TODO: fix this leak
		r.WaitGroup.Wait()
		close(endCh)
	})

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-endCh:
		return nil
	}
}
