// serve.go provides functionality for serving and managing the media pipeline.

// Package avpipeline provides the core functionality for building and managing media pipelines.
package avpipeline

import (
	"context"
	"fmt"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/node/condition"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xcontext"
)

const (
	tooVerbosePTRTracing = false
)

type ServeConfig struct {
	EachNode             node.ServeConfig
	NodeTreeFilter       condition.Condition
	NodeFilter           condition.Condition
	AutoServeNewBranches bool
}

func Serve[T node.Abstract](
	ctx context.Context,
	serveConfig ServeConfig,
	errCh chan<- node.Error,
	nodes ...T,
) {
	var nodesWG sync.WaitGroup
	defer nodesWG.Wait()
	var dstAlreadyVisited sync.Map
	serve(ctx, serveConfig, errCh, &nodesWG, &dstAlreadyVisited, nodes...)
}

func serve[T node.Abstract](
	ctx context.Context,
	serveConfig ServeConfig,
	errCh chan<- node.Error,
	nodesWG *sync.WaitGroup,
	dstAlreadyVisited *sync.Map,
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
			if _, ok := dstAlreadyVisited.LoadOrStore(n, struct{}{}); ok {
				logger.Tracef(ctx, "/Serve[%s]: already visited", nodeKey)
				return
			}
			logger.Tracef(ctx, "Serve[%s]: was not visited", nodeKey)
			dstAlreadyVisited.Store(n, struct{}{})

			if serveConfig.NodeTreeFilter != nil && !serveConfig.NodeTreeFilter.Match(ctx, n) {
				logger.Tracef(ctx, "/Serve[%s]: skipped the whole tree", nodeKey)
				return
			}

			ctx, cancel := context.WithCancel(ctx)
			childrenCtx := xcontext.DetachDone(ctx)
			shouldSkip := false
			if serveConfig.NodeFilter != nil && !serveConfig.NodeFilter.Match(ctx, n) {
				shouldSkip = true
				childrenCtx = ctx // TODO: explain
			}

			childrenCtx, childrenCancelFn := context.WithCancel(childrenCtx)

			pushChangeChan := n.GetChangeChanPushTo()
			currentPushTos := n.GetPushTos(ctx)

			nodesWG.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
				defer nodesWG.Done()
				for {
					select {
					case <-ctx.Done():
						logger.Tracef(ctx, "/Serve[%s]: context done", nodeKey)
						return
					case <-pushChangeChan:
						pushChangeChan = n.GetChangeChanPushTo()
						newPushTos := n.GetPushTos(ctx)
						newNodes := newPushTos.Nodes().Without(currentPushTos.Nodes())
						logger.Tracef(ctx, "Serve[%s]: push change; new nodes count: %d", nodeKey, len(newNodes))
						for _, newNode := range newNodes {
							serve(childrenCtx, serveConfig, errCh, nodesWG, dstAlreadyVisited, newNode)
						}
						currentPushTos = newPushTos
					}
				}
			})

			for _, pushTo := range currentPushTos {
				serve(childrenCtx, serveConfig, errCh, nodesWG, dstAlreadyVisited, pushTo.Node)
			}

			if shouldSkip {
				logger.Tracef(ctx, "/Serve[%s]: skipped", nodeKey)
				return
			}

			logger.Tracef(ctx, "Serve[%s]: starting", nodeKey)
			nodesWG.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
				defer cancel()
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
