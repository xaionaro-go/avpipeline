// route_source.go defines the RouteSource struct for connecting external nodes to the router.

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
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/xsync"
)

type RouteSource[T any, C any, P processor.Abstract] struct {
	Router           *Router[T]
	DstPath          RoutePath
	PublishMode      PublishMode
	TranscoderConfig *transcodertypes.TranscoderConfig
	OnPostStart      func(context.Context, *RouteSource[T, C, P])
	OnPreStop        func(context.Context, *RouteSource[T, C, P])
	OnPostStop       func(context.Context, *RouteSource[T, C, P])
	Locker           xsync.Mutex
	CancelFunc       context.CancelFunc
	Input            *node.NodeWithCustomData[C, P]
	Output           *Route[T]
	WaitGroup        sync.WaitGroup
	StreamForwarder[C, P]
}

func AddRouteSource[T any, C any, P processor.Abstract](
	ctx context.Context,
	r *Router[T],
	srcNode *node.NodeWithCustomData[C, P],
	dstPath RoutePath,
	publishMode PublishMode,
	transcoderConfig *transcodertypes.TranscoderConfig,
	onPostStart func(context.Context, *RouteSource[T, C, P]),
	onPreStop func(context.Context, *RouteSource[T, C, P]),
	onPostStop func(context.Context, *RouteSource[T, C, P]),
) (_ret *RouteSource[T, C, P], _err error) {
	logger.Debugf(ctx, "AddRouteSource(ctx, %v, '%s')", srcNode, dstPath)
	defer func() {
		logger.Debugf(ctx, "/AddRouteSource(ctx, %v, '%s'): %p %v", srcNode, dstPath, _ret, _err)
	}()
	ctx = belt.WithField(ctx, "dst_path", dstPath)

	fwd := &RouteSource[T, C, P]{
		Router:           r,
		Input:            srcNode,
		DstPath:          dstPath,
		PublishMode:      publishMode,
		TranscoderConfig: transcoderConfig,
		OnPostStart:      onPostStart,
		OnPreStop:        onPreStop,
		OnPostStop:       onPostStop,
	}
	if err := fwd.open(ctx); err != nil {
		return nil, fmt.Errorf("unable to initialize: %w", err)
	}

	return fwd, nil
}

func (fwd *RouteSource[T, C, P]) open(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "open")
	defer func() { logger.Debugf(ctx, "/open: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.openLocked, ctx)
}

func (fwd *RouteSource[T, C, P]) openLocked(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "openLocked")
	defer func() { logger.Debugf(ctx, "/openLocked: %v", _err) }()
	if fwd.CancelFunc != nil {
		return fmt.Errorf("internal error: already started")
	}

	ctx, cancelFn := context.WithCancel(ctx)
	fwd.CancelFunc = cancelFn

	return fwd.startLocked(ctx)
}

func (fwd *RouteSource[T, C, P]) GetPublishMode(
	ctx context.Context,
) PublishMode {
	return fwd.PublishMode
}

func (fwd *RouteSource[T, C, P]) Start(
	ctx context.Context,
) (_err error) {
	ctx = belt.WithField(ctx, "input", fwd.Input.String())
	ctx = belt.WithField(ctx, "route", fwd.DstPath)
	logger.Debugf(ctx, "Start")
	defer func() { logger.Debugf(ctx, "/Start: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.startLocked, ctx)
}

func (fwd *RouteSource[T, C, P]) startLocked(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "startLocked")
	defer func() { logger.Debugf(ctx, "/startLocked: %v", _err) }()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fwd.WaitGroup.Add(1)
	defer fwd.WaitGroup.Done()
	defer func() {
		if _err == nil {
			if fwd.OnPostStart != nil {
				fwd.OnPostStart(ctx, fwd)
			}
		} else {
			logger.Tracef(ctx, "initiating doStopLocked because startLocked failed with %v", _err)
			err := fwd.doStopLocked(ctx)
			logger.Tracef(ctx, "doStopLocked result: %v", err)
		}
	}()

	dst, err := fwd.Router.GetRoute(ctx, fwd.DstPath, GetRouteModeCreateTemporaryIfNotFound)
	if err != nil {
		return fmt.Errorf("unable to get the destination route by path '%s': %w", fwd.DstPath, err)
	}
	fwd.Output = dst
	logger.Tracef(ctx, "fwd.Output = %s", dst)

	logger.Tracef(ctx, "AddPublisher")
	if _, err := dst.AddPublisher(ctx, fwd); err != nil {
		return fmt.Errorf("unable to add the RouteSource as a publisher to Route '%s': %w", dst.Path, err)
	}

	f, err := NewStreamForwarder(ctx, fwd.Input, dst.Node, fwd.TranscoderConfig)
	if err != nil {
		return fmt.Errorf("unable to initialize a forwarder from %T to '%s' (%#+v): %w", fwd.Input, dst.Path, fwd.TranscoderConfig, err)
	}
	if err := f.Start(ctx); err != nil {
		return fmt.Errorf("unable to start stream forwarding: %w", err)
	}
	fwd.StreamForwarder = f

	return nil
}

func (fwd *RouteSource[T, C, P]) Stop(
	ctx context.Context,
) (_err error) {
	ctx = belt.WithField(ctx, "input", fwd.Input.String())
	ctx = belt.WithField(ctx, "route", fwd.DstPath)
	logger.Debugf(ctx, "Stop")
	defer func() { logger.Debugf(ctx, "/Stop: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.stopLocked, ctx)
}

func (fwd *RouteSource[T, C, P]) stopLocked(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "stopLocked")
	defer func() { logger.Tracef(ctx, "/stopLocked: %v", _err) }()

	fwd.WaitGroup.Add(1)
	defer fwd.WaitGroup.Done()

	if fwd.OnPostStop != nil {
		defer func() {
			fwd.OnPostStop(ctx, fwd)
		}()
	}

	if fwd.OnPreStop != nil {
		fwd.OnPreStop(ctx, fwd)
	}

	return fwd.doStopLocked(ctx)
}

func (fwd *RouteSource[T, C, P]) doStopLocked(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "doStopLocked")
	defer func() { logger.Tracef(ctx, "/doStopLocked: %v", _err) }()

	if fwd.Output != nil {
		logger.Tracef(ctx, "RemovePublisher")
		if _, err := fwd.Output.RemovePublisher(ctx, fwd); err != nil {
			return fmt.Errorf("unable to remove the RouteSource as a publisher to Route '%s': %w", fwd.Output.Path, err)
		}
		logger.Tracef(ctx, "fwd.Output = nil")
		fwd.Output = nil
	}

	var errs []error
	if fwd.StreamForwarder != nil {
		if err := fwd.StreamForwarder.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("fwd.StreamForwarder.Stop: %w", err))
		}
		fwd.StreamForwarder = nil
	}

	return errors.Join(errs...)
}

func (fwd *RouteSource[T, C, P]) Close(
	ctx context.Context,
) (_err error) {
	ctx = belt.WithField(ctx, "input", fwd.Input.String())
	ctx = belt.WithField(ctx, "route", fwd.DstPath)
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.closeLocked, ctx)
}

func (fwd *RouteSource[T, C, P]) closeLocked(
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

func (fwd *RouteSource[T, C, P]) String() string {
	return fmt.Sprintf("fwd('%s'->'%s')", fwd.Input, fwd.Output)
}

var _ Publisher[any] = (*RouteForwarding[any])(nil)

func (fwd *RouteSource[T, C, P]) GetInputNode(
	ctx context.Context,
) node.Abstract {
	return fwd.Input
}

func (fwd *RouteSource[T, C, P]) GetOutputRoute(
	ctx context.Context,
) *Route[T] {
	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.getOutputRouteLocked, ctx)
}

func (fwd *RouteSource[T, C, P]) getOutputRouteLocked(
	context.Context,
) *Route[T] {
	return fwd.Output
}
