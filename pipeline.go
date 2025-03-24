package avpipeline

import (
	"context"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xcontext"
)

func ServeRecursively[T AbstractNode](
	ctx context.Context,
	p T,
	serveConfig ServeConfig,
	errCh chan<- ErrNode,
) {
	logger.Tracef(ctx, "ServeRecursively[%s]", p)
	defer func() { logger.Tracef(ctx, "/ServeRecursively[%s]", p) }()

	childrenCtx, childrenCancelFn := context.WithCancel(xcontext.DetachDone(ctx))
	var wg sync.WaitGroup
	dstAlreadyStarted := map[AbstractNode]struct{}{}
	for _, pushTo := range p.GetPushPacketsTos() {
		if _, ok := dstAlreadyStarted[pushTo.Node]; ok {
			continue
		}
		pushTo := pushTo
		wg.Add(1)
		observability.Go(ctx, func() {
			defer wg.Done()
			ServeRecursively(childrenCtx, pushTo.Node, serveConfig, errCh)
		})
		dstAlreadyStarted[pushTo.Node] = struct{}{}
	}
	for _, pushTo := range p.GetPushFramesTos() {
		if _, ok := dstAlreadyStarted[pushTo.Node]; ok {
			continue
		}
		pushTo := pushTo
		wg.Add(1)
		observability.Go(ctx, func() {
			defer wg.Done()
			ServeRecursively(childrenCtx, pushTo.Node, serveConfig, errCh)
		})
		dstAlreadyStarted[pushTo.Node] = struct{}{}
	}
	defer wg.Wait()
	defer childrenCancelFn()

	p.Serve(ctx, serveConfig, errCh)
}
