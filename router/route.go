package router

import (
	"context"
	"fmt"

	"slices"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
	routertypes "github.com/xaionaro-go/avpipeline/router/types"
	avptypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

const (
	routeFrameDrop = true
)

type RoutePath = routertypes.RoutePath

type Route struct {
	Locker xsync.Mutex

	// read only:
	Path RoutePath

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
	onClosed func(context.Context, *Route),
) *Route {
	ctx = belt.WithField(ctx, "path", path)
	ctx, cancelFn := context.WithCancel(ctx)
	r := &Route{
		Path:                 path,
		PublishersChangeChan: make(chan struct{}),
		CancelFunc:           cancelFn,
	}
	r.Node = node.NewWithCustomDataFromKernel[*Route](
		ctx,
		kernel.NewMapStreamIndices(ctx, r),
		processor.DefaultOptionsRecoder()...,
	)
	r.Node.CustomData = r
	if onOpen != nil {
		onOpen(ctx, r)
	}
	observability.Go(ctx, func() {
		defer r.Close(ctx)
		if onClosed != nil {
			defer onClosed(ctx, r)
		}
		defer logger.Debugf(ctx, "ended")
		logger.Debugf(ctx, "started")
		r.Node.Serve(ctx, node.ServeConfig{
			// we don't want the whole pipeline to hang just because of one bad consumer:
			FrameDrop: routeFrameDrop,
		}, errCh)
	})
	return r
}

func (r *Route) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	r.CancelFunc()
	return nil
}

func (r *Route) getPublishersChangeChan(
	ctx context.Context,
) <-chan struct{} {
	return xsync.DoR1(ctx, &r.Locker, func() <-chan struct{} {
		return r.PublishersChangeChan
	})
}

func (r *Route) StreamIndexAssign(
	ctx context.Context,
	input avptypes.InputPacketOrFrameUnion,
) (typing.Optional[int], error) {
	return typing.Opt(input.GetStreamIndex()), nil
}

func (r *Route) GetPublishers(
	ctx context.Context,
) Publishers {
	return xsync.DoR1(ctx, &r.Locker, func() Publishers {
		return r.Publishers
	})
}

func (r *Route) AddPublisher(
	ctx context.Context,
	publisher Publisher,
) error {
	return xsync.DoR1(ctx, &r.Locker, func() error {
		if len(r.Publishers) > 0 {
			return fmt.Errorf("there are already %d publishers on this route", len(r.Publishers))
		}
		_ = r.addPublisherLocked(ctx, publisher)
		return nil
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
	var ch chan<- struct{}
	ch, r.PublishersChangeChan = r.PublishersChangeChan, make(chan struct{})
	close(ch)
	return r.Publishers
}

func (r *Route) RemovePublisher(
	ctx context.Context,
	publisher Publisher,
) (Publishers, error) {
	return xsync.DoA2R2(ctx, &r.Locker, r.removePublisherLocked, ctx, publisher)
}

func (r *Route) removePublisherLocked(
	ctx context.Context,
	publisher Publisher,
) (Publishers, error) {
	for idx, candidate := range r.Publishers {
		if publisher == candidate {
			r.Publishers = slices.Delete(r.Publishers, idx, idx+1)
			var ch chan<- struct{}
			ch, r.PublishersChangeChan = r.PublishersChangeChan, make(chan struct{})
			close(ch)
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
