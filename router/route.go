package router

import (
	"context"
	"fmt"
	"sync"

	"slices"

	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
	routertypes "github.com/xaionaro-go/avpipeline/router/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	routeFrameDrop      = true
	routeCloseProcessor = true
)

type RoutePath = routertypes.RoutePath

type FuncPublisherEvent[T any] func(context.Context, *Route[T], Publisher[T])

type Route[T any] struct {
	CustomData         T
	OnPublisherAdded   FuncPublisherEvent[T]
	OnPublisherRemoved FuncPublisherEvent[T]

	// not supported, yet (TODO: fix)
	OnConsumerAdded   func(context.Context, *Route[T], Consumer[T])
	OnConsumerRemoved func(context.Context, *Route[T], Consumer[T])

	// read only:
	Path       RoutePath
	OnOpen     func(context.Context, *Route[T])
	OnClose    func(context.Context, *Route[T])
	IsNodeOpen bool
	Node       *NodeRouting[T]

	// access only when Locker is locked:
	Publishers           Publishers[T]
	PublishersChangeChan chan struct{}

	// internal:
	CancelFunc context.CancelFunc
}

func newRoute[T any](
	ctx context.Context,
	path RoutePath,
	errCh chan<- node.Error,
	onOpen func(context.Context, *Route[T]),
	onClose func(context.Context, *Route[T]),
	onPublisherAdded func(context.Context, *Route[T], Publisher[T]),
	onPublisherRemoved func(context.Context, *Route[T], Publisher[T]),
) (_ret *Route[T]) {
	ctx = belt.WithField(ctx, "path", path)
	logger.Tracef(ctx, "newRoute")
	defer func() { logger.Tracef(ctx, "/newRoute: %v", _ret) }()
	ctx, cancelFn := context.WithCancel(ctx)
	r := &Route[T]{
		Path:                 path,
		OnOpen:               onOpen,
		OnClose:              onClose,
		OnPublisherAdded:     onPublisherAdded,
		OnPublisherRemoved:   onPublisherRemoved,
		PublishersChangeChan: make(chan struct{}),
		CancelFunc:           cancelFn,
	}
	close(r.PublishersChangeChan) // this line is just for local consistency: initially the route is closed until openNodeLocked is called
	processor := processor.NewFromKernel(
		ctx,
		must(NewNodeKernel(ctx)),
		processor.DefaultOptionsRecoder()...,
	)
	var opts node.Options
	logger.Tracef(ctx, "Creating node for route %s with options %v", path, opts)
	r.Node = node.NewWithCustomData[GoBug63285RouteInterface[T]](
		processor,
		opts...,
	)
	r.Node.CustomData = r
	r.openNodeLocked(ctx)
	observability.Go(ctx, func(ctx context.Context) {
		defer r.Close(ctx)
		defer logger.Debugf(ctx, "ended")
		logger.Debugf(ctx, "started")
		r.Node.Serve(ctx, node.ServeConfig{
			// we don't want the whole pipeline to hang just because of one bad consumer:
			FrameDropVideo: routeFrameDrop,
			FrameDropAudio: routeFrameDrop,
			FrameDropOther: routeFrameDrop,
		}, errCh)
	})
	return r
}

func (r *Route[T]) openNodeLocked(
	ctx context.Context,
) {
	logger.Debugf(ctx, "openNodeLocked: %s", r.Path)
	defer func() { logger.Debugf(ctx, "/openNodeLocked: %s", r.Path) }()

	if routeCloseProcessor {
		r.Node.Processor = processor.NewFromKernel(
			ctx,
			must(NewNodeKernel(ctx)),
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

func (r *Route[T]) closeNodeLocked(
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
		observability.Go(ctx, func(ctx context.Context) {
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

func (r *Route[T]) closeLocked(
	ctx context.Context,
	wg *sync.WaitGroup,
) (_err error) {
	logger.Tracef(ctx, "closeLocked: %s", r.Path)
	defer func() { logger.Tracef(ctx, "/closeLocked: %s: %v", r.Path, _err) }()
	if r.IsNodeOpen {
		_err = r.closeNodeLocked(ctx, wg)
	}
	r.CancelFunc()
	return
}

func (r *Route[T]) Close(ctx context.Context) (_err error) {
	var sample T
	logger.Debugf(ctx, "Route[%T].Close", sample)
	defer func() { logger.Debugf(ctx, "/Route[%T].Close: %v", sample, _err) }()
	var wg sync.WaitGroup
	defer wg.Wait()
	return xsync.DoA2R1(ctx, &r.Node.Locker, r.closeLocked, ctx, &wg)
}
func (r *Route[T]) getPublishersChangeChan(
	ctx context.Context,
) <-chan struct{} {
	return xsync.DoR1(ctx, &r.Node.Locker, func() <-chan struct{} {
		return r.PublishersChangeChan
	})
}

func (r *Route[T]) GetPublishers(
	ctx context.Context,
) Publishers[T] {
	return xsync.DoR1(ctx, &r.Node.Locker, func() Publishers[T] {
		return r.Publishers
	})
}

func (r *Route[T]) AddPublisher(
	ctx context.Context,
	publisher Publisher[T],
) (_ret Publishers[T], _err error) {
	logger.Debugf(ctx, "AddPublisher[%s](ctx, %s)", r, publisher)
	defer func() {
		logger.Debugf(ctx, "/AddPublisher[%s](ctx, %s): len(ret): len(ret):%d, %v", r, publisher, len(_ret), _err)
	}()

	var callbackFunc FuncPublisherEvent[T]
	var wg sync.WaitGroup
	defer wg.Wait()
	r.Locker().Do(ctx, func() {
		_ret, _err = r.AddPublisherLocked(ctx, publisher, &wg)
		callbackFunc = r.OnPublisherAdded
	})
	callbackFunc(ctx, r, publisher)
	return
}

func (r *Route[T]) Locker() *xsync.Mutex {
	return &r.Node.Locker
}

func (r *Route[T]) LockDo(ctx context.Context, fn func(ctx context.Context)) {
	r.Locker().Do(ctx, func() {
		fn(ctx)
	})
}

func (r *Route[T]) AddPublisherLocked(
	ctx context.Context,
	publisher Publisher[T],
	wg *sync.WaitGroup,
) (_ret Publishers[T], _err error) {
	ctx = belt.WithField(ctx, "publish_mode", publisher.GetPublishMode(ctx))
	logger.Debugf(ctx, "AddPublisherLocked[%s](ctx, %s)", r, publisher)
	defer func() {
		logger.Debugf(ctx, "/AddPublisherLocked[%s](ctx, %s): len(ret):%d, %v", r, publisher, len(_ret), _err)
	}()

	if !r.IsNodeOpen {
		return nil, ErrRouteClosed{}
	}

	if publisher == nil {
		return nil, fmt.Errorf("publisher == nil")
	}
	if slices.Contains(r.Publishers, publisher) {
		return nil, ErrAlreadyAPublisher{}
	}

	if len(r.Publishers) > 0 {
		removePublisher := func(ctx context.Context, publisher Publisher[T]) {
			logger.Debugf(ctx, "removing publisher %s from route %s", publisher, r)
			err := publisher.Close(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to close publisher %s: %v", publisher, err)
			}
		}

		removePublishers := func() {
			for _, publisher := range r.Publishers {
				publisher := publisher
				wg.Add(1)
				observability.Go(ctx, func(ctx context.Context) {
					defer wg.Done()
					removePublisher(ctx, publisher)
				})
			}
			r.Publishers = r.Publishers[:0]
		}
		mode := publisher.GetPublishMode(ctx)
		switch mode {
		case PublishModeExclusiveTakeover:
			removePublishers()
		case PublishModeExclusiveFail:
			return nil, ErrAlreadyHasPublisher{}
		case PublishModeSharedTakeover, PublishModeSharedFail:
			newPublishers := make([]Publisher[T], 0, len(r.Publishers))
			var publishersToRemove Publishers[T]
			for _, publisher := range r.Publishers {
				if !publisher.GetPublishMode(ctx).IsExclusive() {
					newPublishers = append(newPublishers, publisher)
					continue
				}
				if mode.FailOnConflict() {
					return nil, ErrAlreadyHasPublisher{}
				}
				logger.Debugf(ctx, "conflicting with publisher %s on route %s, requesting to remove it (if it won't be aborted)", publisher, r)
				publishersToRemove = append(publishersToRemove, publisher)
			}
			for _, publisher := range publishersToRemove {
				removePublisher(ctx, publisher)
			}
			r.Publishers = newPublishers
		default:
			return nil, fmt.Errorf("internal error: unknown publishing mode: %s", mode)
		}
	}

	r.Publishers = append(r.Publishers, publisher)
	if r.IsNodeOpen {
		var ch chan<- struct{}
		ch, r.PublishersChangeChan = r.PublishersChangeChan, make(chan struct{})
		close(ch)
	}
	return r.Publishers, nil
}

func (r *Route[T]) RemovePublisher(
	ctx context.Context,
	publisher Publisher[T],
) (Publishers[T], error) {
	var wg sync.WaitGroup
	defer wg.Wait()
	return xsync.DoA3R2(ctx, &r.Node.Locker, r.RemovePublisherLocked, ctx, publisher, &wg)
}

func (r *Route[T]) RemovePublisherLocked(
	ctx context.Context,
	publisher Publisher[T],
	wg *sync.WaitGroup,
) (Publishers[T], error) {
	if r.OnPublisherRemoved != nil {
		callback := r.OnPublisherRemoved
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			callback(ctx, r, publisher)
		})
	}
	for idx, candidate := range r.Publishers {
		if publisher == candidate {
			r.Publishers = slices.Delete(r.Publishers, idx, idx+1)
			if r.IsNodeOpen {
				var ch chan<- struct{}
				ch, r.PublishersChangeChan = r.PublishersChangeChan, make(chan struct{})
				close(ch)
			}
			return r.Publishers, nil
		}
	}
	return nil, ErrPublisherNotFound{}
}

func (r *Route[T]) IsOpen(ctx context.Context) (_ret bool) {
	logger.Debugf(ctx, "IsOpen")
	defer func() { logger.Debugf(ctx, "/IsOpen: %v", _ret) }()
	return xsync.DoR1(ctx, &r.Node.Locker, func() bool {
		return r.IsNodeOpen
	})
}

func (r *Route[T]) WaitForPublisher(
	ctx context.Context,
) (Publishers[T], error) {
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

func (r *Route[T]) String() string {
	if r == nil {
		return "<nil>"
	}
	return string(r.Path)
}
