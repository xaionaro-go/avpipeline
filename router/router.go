package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type Router[T any] struct {
	OnRouteCreated          func(context.Context, *Route[T])
	OnRouteRemoved          func(context.Context, *Route[T])
	OnRoutePublisherAdded   func(context.Context, *Route[T], Publisher[T])
	OnRoutePublisherRemoved func(context.Context, *Route[T], Publisher[T])

	WaitGroup sync.WaitGroup

	Locker xsync.Mutex

	// access only when Locker is locked:
	RoutesByPath      map[RoutePath]*Route[T]
	RouterCloseChan   chan struct{}
	RoutesChangedChan chan struct{}
	ErrorChan         chan node.Error
}

func New[T any](
	ctx context.Context,
) *Router[T] {
	r := &Router[T]{
		RoutesByPath:      map[RoutePath]*Route[T]{},
		RouterCloseChan:   make(chan struct{}),
		RoutesChangedChan: make(chan struct{}),
		ErrorChan:         make(chan node.Error, 100),
	}
	r.init(ctx)
	return r
}

func (r *Router[T]) Close(
	ctx context.Context,
) error {
	r.Locker.Do(ctx, func() { // to make sure we don't have anybody adding more processes
		close(r.RouterCloseChan)
		r.WaitGroup.Wait()
		close(r.ErrorChan)
		close(r.RoutesChangedChan)
	})
	return nil
}

func (r *Router[T]) init(
	ctx context.Context,
) {
	observability.Go(ctx, func(ctx context.Context) {
		for err := range r.ErrorChan {
			route := err.Node.(*NodeRouting[T]).CustomData.(*Route[T])
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

func (r *Router[T]) onRouteCreated(
	ctx context.Context,
	route *Route[T],
) {
	logger.Debugf(ctx, "onRouteCreated: %s", route)
	defer func() { logger.Debugf(ctx, "/onRouteCreated: %s", route) }()
	r.WaitGroup.Add(1)
	if r.OnRouteCreated != nil {
		r.OnRouteCreated(ctx, route)
	}
}

func (r *Router[T]) onRouteClosed(
	ctx context.Context,
	route *Route[T],
) {
	logger.Debugf(ctx, "onRouteClosed: %s", route)
	defer func() { logger.Debugf(ctx, "/onRouteClosed: %s", route) }()
	if err := r.RemoveRoute(ctx, route); err != nil {
		logger.Errorf(ctx, "unable to remove route '%s': %v", route, err)
	}
	// TODO: figure out: should I add r.WaitGroup.Done() here? (see also onRouteCreated)
}

func (r *Router[T]) onRoutePublisherAdded(
	ctx context.Context,
	route *Route[T],
	publisher Publisher[T],
) {
	logger.Debugf(ctx, "onRoutePublisherAdded: %s", route)
	defer func() { logger.Debugf(ctx, "/onRoutePublisherAdded: %s", route) }()

	if r.OnRoutePublisherAdded != nil {
		r.OnRoutePublisherAdded(ctx, route, publisher)
	}
}

func (r *Router[T]) onRoutePublisherRemoved(
	ctx context.Context,
	route *Route[T],
	publisher Publisher[T],
) {
	logger.Debugf(ctx, "onRoutePublisherRemoved: %s", route)
	defer func() { logger.Debugf(ctx, "/onRoutePublisherRemoved: %s", route) }()

	if r.OnRoutePublisherRemoved != nil {
		r.OnRoutePublisherRemoved(ctx, route, publisher)
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

func (r *Router[T]) GetRoute(
	ctx context.Context,
	path RoutePath,
	mode GetRouteMode,

) (_ret *Route[T], _err error) {
	logger.Debugf(ctx, "GetRoute(ctx, '%s', '%s')", path, mode)
	defer func() { logger.Debugf(ctx, "/GetRoute(ctx, '%s', '%s'): %v %v", path, mode, _ret, _err) }()
	return xsync.DoR2(ctx, &r.Locker, func() (*Route[T], error) {
		return r.getRouteLocked(ctx, path, mode)
	})
}

func (r *Router[T]) getRouteLocked(
	ctx context.Context,
	path RoutePath,
	mode GetRouteMode,
) (*Route[T], error) {
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
		var route *Route[T]
		var err error
		r.Locker.UDo(ctx, func() {
			route, err = r.WaitForRoute(ctx, path)
		})
		if err != nil {
			return nil, fmt.Errorf("unable to wait for route '%s': %w", path, err)
		}
		return route, nil
	case GetRouteModeWaitForPublisher:
		var route *Route[T]
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

func (r *Router[T]) createRoute(
	ctx context.Context,
	path RoutePath,
) (_ret *Route[T]) {
	logger.Debugf(ctx, "createRoute(ctx, '%s')", path)
	defer func() { logger.Debugf(ctx, "/createRoute(ctx, '%s'): %v", path, _ret) }()

	route := r.RoutesByPath[path]
	if route != nil {
		return route
	}

	select {
	case <-r.RouterCloseChan:
		return nil
	default:
	}
	route = newRoute(
		ctx,
		path,
		r.ErrorChan,
		r.onRouteCreated,
		r.onRouteClosed,
		r.onRoutePublisherAdded,
		r.onRoutePublisherRemoved,
	)
	r.RoutesByPath[path] = route
	var addCh chan<- struct{}
	addCh, r.RoutesChangedChan = r.RoutesChangedChan, make(chan struct{})
	close(addCh)

	return route
}

func (r *Router[T]) GetRoutesChangedChan(
	ctx context.Context,
) chan struct{} {
	return xsync.DoR1(ctx, &r.Locker, func() chan struct{} {
		return r.RoutesChangedChan
	})
}

func (r *Router[T]) RemoveRouteByPath(
	ctx context.Context,
	path RoutePath,
) *Route[T] {
	var wg sync.WaitGroup
	defer wg.Wait()
	return xsync.DoR1(ctx, &r.Locker, func() *Route[T] {
		route := r.RoutesByPath[path]
		r.removeRouteLocked(ctx, route, &wg)
		return route
	})
}

func (r *Router[T]) RemoveRoute(
	ctx context.Context,
	route *Route[T],
) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	return xsync.DoR1(ctx, &r.Locker, func() error {
		oldRoute := r.RoutesByPath[route.Path]
		if oldRoute != route {
			return fmt.Errorf("route instance is not set in the router")
		}
		r.removeRouteLocked(ctx, route, &wg)
		r.WaitGroup.Done()
		return nil
	})
}

func (r *Router[T]) removeRouteLocked(
	ctx context.Context,
	route *Route[T],
	wg *sync.WaitGroup,
) {
	logger.Debugf(ctx, "removeRouteLocked: %s", route)
	defer func() { logger.Debugf(ctx, "/removeRouteLocked: %s", route) }()
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		route.Close(ctx)
	})
	delete(r.RoutesByPath, route.Path)
	var remCh chan<- struct{}
	remCh, r.RoutesChangedChan = r.RoutesChangedChan, make(chan struct{})
	close(remCh)
	if r.OnRouteRemoved != nil {
		r.Locker.UDo(ctx, func() {
			r.OnRouteRemoved(ctx, route)
		})
	}
}

func (r *Router[T]) WaitForRoute(
	ctx context.Context,
	path RoutePath,
) (_ret *Route[T], _err error) {
	logger.Debugf(ctx, "WaitForRoute: %s", path)
	defer func() { logger.Debugf(ctx, "/WaitForRoute: %s: %v %v", path, _ret, _err) }()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.GetRoutesChangedChan(ctx):
			if route := xsync.DoR1(ctx, &r.Locker, func() *Route[T] {
				return r.RoutesByPath[path]
			}); route != nil {
				return route, nil
			}
		}
	}
}

func (r *Router[T]) Wait(ctx context.Context) error {
	logger.Debugf(ctx, "Wait")
	defer func() { logger.Debugf(ctx, "/Wait") }()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.RouterCloseChan:
	}

	endCh := make(chan struct{})
	observability.Go(ctx, func(ctx context.Context) { // TODO: fix this leak
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
