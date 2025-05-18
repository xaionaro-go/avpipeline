package router

import (
	"context"
	"fmt"
	"sync"

	"slices"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
	routertypes "github.com/xaionaro-go/avpipeline/router/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	routeFrameDrop      = true
	routeCloseProcessor = true
	routeAutoReset      = false
)

type RoutePath = routertypes.RoutePath

type Route struct {
	// read only:
	Path       RoutePath
	OnOpen     func(context.Context, *Route)
	OnClose    func(context.Context, *Route)
	IsNodeOpen bool

	// access only when Locker is locked:
	Node                 *NodeRouting
	Publishers           Publishers
	PublishersChangeChan chan struct{}

	// internal:
	CancelFunc context.CancelFunc
}

func newRoute(
	ctx context.Context,
	path RoutePath,
	errCh chan<- node.Error,
	onOpen func(context.Context, *Route),
	onClose func(context.Context, *Route),
) *Route {
	ctx = belt.WithField(ctx, "path", path)
	ctx, cancelFn := context.WithCancel(ctx)
	r := &Route{
		Path:                 path,
		OnOpen:               onOpen,
		OnClose:              onClose,
		PublishersChangeChan: make(chan struct{}),
		CancelFunc:           cancelFn,
	}
	close(r.PublishersChangeChan)
	r.Node = node.NewWithCustomDataFromKernel[*Route](
		ctx,
		kernel.NewMapStreamIndices(ctx, nil),
		processor.DefaultOptionsRecoder()...,
	)
	r.Node.CustomData = r
	r.openNodeLocked(ctx)
	observability.Go(ctx, func() {
		defer r.Close(ctx)
		defer logger.Debugf(ctx, "ended")
		logger.Debugf(ctx, "started")
		r.Node.Serve(ctx, node.ServeConfig{
			// we don't want the whole pipeline to hang just because of one bad consumer:
			FrameDrop: routeFrameDrop,
		}, errCh)
	})
	return r
}

func (r *Route) openNodeLocked(
	ctx context.Context,
) {
	logger.Debugf(ctx, "openNodeLocked: %s", r.Path)
	defer func() { logger.Debugf(ctx, "/openNodeLocked: %s", r.Path) }()

	if routeCloseProcessor {
		r.Node.Processor = processor.NewFromKernel(
			ctx,
			kernel.NewMapStreamIndices(ctx, nil),
			processor.DefaultOptionsRecoder()...,
		)
	}
	if r.IsNodeOpen {
		panic("is already open")
	}

	r.IsNodeOpen = true
	r.PublishersChangeChan = make(chan struct{})
	if r.OnOpen != nil {
		r.OnOpen(ctx, r)
	}
}

func (r *Route) closeNodeLocked(
	ctx context.Context,
	wg *sync.WaitGroup,
) (_err error) {
	logger.Debugf(ctx, "closeNodeLocked: %s", r.Path)
	defer func() { logger.Debugf(ctx, "/closeNodeLocked: %s: %v", r.Path, _err) }()

	if !r.IsNodeOpen {
		return ErrAlreadyClosed{}
	}
	r.IsNodeOpen = false
	if r.OnClose != nil {
		wg.Add(1)
		observability.Go(ctx, func() {
			defer wg.Done()
			r.OnClose(ctx, r)
		})
	}
	if routeCloseProcessor {
		_err = r.Node.Processor.Close(ctx)
	}
	close(r.PublishersChangeChan)
	return
}

func (r *Route) closeLocked(
	ctx context.Context,
	wg *sync.WaitGroup,
) (_err error) {
	if r.IsNodeOpen {
		_err = r.closeNodeLocked(ctx, wg)
	}
	r.CancelFunc()
	return
}

func (r *Route) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	var wg sync.WaitGroup
	defer wg.Wait()
	return xsync.DoA2R1(ctx, &r.Node.Locker, r.closeLocked, ctx, &wg)
}
func (r *Route) getPublishersChangeChan(
	ctx context.Context,
) <-chan struct{} {
	return xsync.DoR1(ctx, &r.Node.Locker, func() <-chan struct{} {
		return r.PublishersChangeChan
	})
}

func (r *Route) GetPublishers(
	ctx context.Context,
) Publishers {
	return xsync.DoR1(ctx, &r.Node.Locker, func() Publishers {
		return r.Publishers
	})
}

func (r *Route) AddPublisher(
	ctx context.Context,
	publisher Publisher,
) (_err error) {
	logger.Debugf(ctx, "AddPublisher[%s](ctx, %#+v)", r, publisher)
	defer func() { logger.Debugf(ctx, "/AddPublisher[%s](ctx, %#+v): %v", r, publisher, _err) }()
	var wg sync.WaitGroup
	defer wg.Wait()
	return xsync.DoR1(ctx, &r.Node.Locker, func() error {
		if !r.IsNodeOpen {
			return ErrRouteClosed{}
		}

		// currently we support only one publisher:
		for _, publisher := range r.Publishers {
			wg.Add(1)
			observability.Go(ctx, func() {
				defer wg.Done()
				logger.Errorf(ctx, "closing publisher %s to free-up the route for another publisher", publisher)
				err := publisher.Close(ctx)
				if err != nil {
					logger.Errorf(ctx, "unable to close the publisher: %v", err)
				}
			})
		}
		r.Publishers = r.Publishers[:0]

		_ = r.addPublisherLocked(ctx, publisher)
		return nil
	})
}

func (r *Route) LockDo(ctx context.Context, fn func(ctx context.Context)) {
	r.Node.Locker.Do(ctx, func() {
		fn(ctx)
	})
}

func (r *Route) addPublisherLocked(
	_ context.Context,
	publisher Publisher,
) Publishers {
	if slices.Contains(r.Publishers, publisher) {
		// already added
		return r.Publishers
	}
	r.Publishers = append(r.Publishers, publisher)
	if r.IsNodeOpen {
		var ch chan<- struct{}
		ch, r.PublishersChangeChan = r.PublishersChangeChan, make(chan struct{})
		close(ch)
	}
	return r.Publishers
}

func (r *Route) RemovePublisher(
	ctx context.Context,
	publisher Publisher,
) (Publishers, error) {
	var wg sync.WaitGroup
	defer wg.Wait()
	return xsync.DoA3R2(ctx, &r.Node.Locker, r.removePublisherLocked, ctx, publisher, &wg)
}

func (r *Route) removePublisherLocked(
	ctx context.Context,
	publisher Publisher,
	wg *sync.WaitGroup,
) (Publishers, error) {
	for idx, candidate := range r.Publishers {
		if publisher == candidate {
			r.Publishers = slices.Delete(r.Publishers, idx, idx+1)
			if r.IsNodeOpen {
				var ch chan<- struct{}
				ch, r.PublishersChangeChan = r.PublishersChangeChan, make(chan struct{})
				close(ch)
			}

			if routeAutoReset && len(r.Publishers) == 0 {
				// TODO: add an option to keep the clients to wait for a new stream
				logger.Debugf(ctx, "zero publishers left; dropping the clients")
				if err := r.resetNodeLocked(ctx, wg); err != nil {
					return r.Publishers, fmt.Errorf("unable to reset the node: %w", err)
				}
			}
			return r.Publishers, nil
		}
	}

	return nil, fmt.Errorf("the publisher is not found in the list of route's publishers")
}

func (r *Route) WaitForPublisher(
	ctx context.Context,
) (Publishers, error) {
	ch := r.getPublishersChangeChan(ctx)
	for {
		publishers := r.GetPublishers(ctx)
		if len(publishers) > 0 {
			return publishers, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ch:
			ch = r.getPublishersChangeChan(ctx)
		}
	}
}

func (r *Route) String() string {
	return string(r.Path)
}
