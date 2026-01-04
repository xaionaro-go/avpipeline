// route_forwarding_to_remote.go provides functions to add route forwarding to remote destinations.

package router

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	transcodertypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

type RouteForwardingToRemote[T any] struct {
	StreamForwarder[GoBug63285RouteInterface[T], *ProcessorRouting]
	Input      *Route[T]
	Output     *NodeRetryOutput
	ErrChan    chan node.Error
	CancelFunc context.CancelFunc
	CloseOnce  sync.Once
}

func (r *Router[T]) AddRouteForwardingToRemote(
	ctx context.Context,
	sourcePath RoutePath,
	dstURL string,
	streamKey secret.String,
	transcoderConfig *transcodertypes.TranscoderConfig,
) (_ret *RouteForwardingToRemote[T], _err error) {
	logger.Debugf(ctx, "AddRouteForwardingToRemote(ctx, '%s', '%s')", sourcePath, dstURL)
	defer func() {
		logger.Debugf(ctx, "/AddRouteForwardingToRemote(ctx, '%s', '%s'): %v %v", sourcePath, dstURL, _ret, _err)
	}()

	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		if _err != nil {
			cancelFn()
		}
	}()
	ctx = belt.WithField(ctx, "src_path", sourcePath)
	ctx = belt.WithField(ctx, "dst_url", dstURL)

	logger.Tracef(ctx, "s.Router.GetRoute")
	input, err := r.GetRoute(ctx, sourcePath, GetRouteModeWaitForPublisher)
	logger.Tracef(ctx, "/s.Router.GetRoute: %v %v", input, err)
	if err != nil {
		return nil, fmt.Errorf("unable to get a route by path '%s': %w", sourcePath, err)
	}
	if input == nil {
		return nil, fmt.Errorf("there is no active route by path '%s'", sourcePath)
	}

	logger.Tracef(ctx, "building RouteForwardingToRemote")

	fwd := &RouteForwardingToRemote[T]{
		Input:      input,
		ErrChan:    make(chan node.Error, 100),
		CancelFunc: cancelFn,
	}
	defer func() {
		if _err != nil {
			fwd.Close(ctx)
		}
	}()

	fwd.Output = newRetryOutputNode(ctx, fwd, func(ctx context.Context) error {
		_, err := fwd.Input.WaitForPublisher(ctx)
		return err
	}, dstURL, streamKey, kernel.OutputConfig{})

	fwd.StreamForwarder, err = NewStreamForwarder(ctx, fwd.Input.Node, fwd.Output, transcoderConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to build a stream forwarder from '%s' to '%s': %w", fwd.Input, fwd.Output, err)
	}

	logger.Tracef(ctx, "built RouteForwardingToRemote")

	if err := fwd.init(ctx); err != nil {
		return nil, fmt.Errorf("unable to initialize: %w", err)
	}
	return fwd, nil
}

func (fwd *RouteForwardingToRemote[T]) init(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "init")
	defer func() { logger.Debugf(ctx, "/init: %v", _err) }()
	if err := fwd.StreamForwarder.Start(ctx); err != nil {
		return fmt.Errorf("unable to add myself into the source's 'PushPacketsTo': %w", err)
	}
	observability.Go(ctx, func(ctx context.Context) {
		defer close(fwd.ErrChan)
		fwd.Output.Serve(ctx, node.ServeConfig{
			DebugData: fwd,
		}, fwd.ErrChan)
	})
	observability.Go(ctx, func(ctx context.Context) {
		for err := range fwd.ErrChan {
			logger.Errorf(ctx, "got an error: %v", err)
			_ = fwd.Close(ctx)
		}
	})
	return nil
}

func (fwd *RouteForwardingToRemote[T]) Close(
	ctx context.Context,
) (_err error) {
	var errs []error
	fwd.CloseOnce.Do(func() {
		logger.Debugf(ctx, "Close")
		defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
		if fwd.StreamForwarder != nil {
			if err := fwd.StreamForwarder.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("unable to stop forwarding the traffic: %w", err))
			}
			fwd.StreamForwarder = nil
		}
		if fwd.Output != nil {
			if err := fwd.Output.Processor.Close(ctx); err != nil {
				errs = append(errs, fmt.Errorf("unable to close the output: %w", err))
			}
			fwd.Output = nil
		}
		fwd.CancelFunc()
	})
	return errors.Join(errs...)
}
