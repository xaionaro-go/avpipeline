package avpipeline

import (
	"context"
	"fmt"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/node/condition"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xcontext"
)

const (
	tooVerbosePTRTracing = false
)

type ServeConfig struct {
	EachNode       node.ServeConfig
	NodeTreeFilter condition.Condition
	NodeFilter     condition.Condition
}

func Serve[T node.Abstract](
	ctx context.Context,
	serveConfig ServeConfig,
	errCh chan<- node.Error,
	nodes ...T,
) {
	var nodesWG sync.WaitGroup
	defer nodesWG.Wait()
	dstAlreadyVisited := map[node.Abstract]struct{}{}
	serve(ctx, serveConfig, errCh, &nodesWG, &dstAlreadyVisited, nodes...)
}
func serve[T node.Abstract](
	ctx context.Context,
	serveConfig ServeConfig,
	errCh chan<- node.Error,
	nodesWG *sync.WaitGroup,
	dstAlreadyVisited *map[node.Abstract]struct{},
	nodes ...T,
) {
	for _, n := range nodes {
		func(n T) {
			if tooVerbosePTRTracing && logger.FromCtx(ctx).Level() >= logger.LevelTrace {
				ctx = logger.CtxWithLogger(ctx, logger.FromCtx(ctx).WithMessagePrefix(fmt.Sprintf("%p: ", any(n))))
				ctx = belt.WithField(ctx, "node_ptr", fmt.Sprintf("%p", any(n)))
				ctx = belt.WithField(ctx, "proc_ptr", fmt.Sprintf("%p", any(n.GetProcessor())))
			}
			nodeKey := fmt.Sprintf("%s:%p", any(n), any(n))
			logger.Tracef(ctx, "Serve[%s]", nodeKey)
			if _, ok := (*dstAlreadyVisited)[n]; ok {
				logger.Tracef(ctx, "/Serve[%s]: already visited", nodeKey)
				return
			}
			logger.Tracef(ctx, "Serve[%s]: was not visited (%v)", nodeKey, (*dstAlreadyVisited))
			(*dstAlreadyVisited)[n] = struct{}{}

			if serveConfig.NodeTreeFilter != nil && !serveConfig.NodeTreeFilter.Match(ctx, n) {
				logger.Tracef(ctx, "/Serve[%s]: skipped the whole tree", nodeKey)
				return
			}

			childrenCtx := xcontext.DetachDone(ctx)
			shouldSkip := false
			if serveConfig.NodeFilter != nil && !serveConfig.NodeFilter.Match(ctx, n) {
				shouldSkip = true
				childrenCtx = ctx
			}

			childrenCtx, childrenCancelFn := context.WithCancel(childrenCtx)
			for _, pushTo := range n.GetPushPacketsTos() {
				serve(childrenCtx, serveConfig, errCh, nodesWG, dstAlreadyVisited, pushTo.Node)
			}
			for _, pushTo := range n.GetPushFramesTos() {
				serve(childrenCtx, serveConfig, errCh, nodesWG, dstAlreadyVisited, pushTo.Node)
			}

			if shouldSkip {
				logger.Tracef(ctx, "/Serve[%s]: skipped", nodeKey)
				return
			}

			logger.Tracef(ctx, "Serve[%s]: starting", nodeKey)
			nodesWG.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
				defer nodesWG.Done()
				defer logger.Tracef(ctx, "/Serve[%s]: ended", nodeKey)
				defer func() {
					logger.Debugf(ctx, "Serve[%s]: cancelling context...", nodeKey)
					childrenCancelFn()
				}()
				n.Serve(ctx, serveConfig.EachNode, errCh)
			})
		}(n)
	}
}
