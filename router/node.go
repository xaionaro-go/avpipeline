package router

import (
	"context"
	"fmt"
	"time"

	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability/xlogger"
	"github.com/xaionaro-go/secret"
)

// unfortunately there is a bug in Go compiler that leads to compilation errors:
// > fatal error: all goroutines are asleep - deadlock!
//
// to workaround the issue we do not reference type `*Route[T]` directly in `NodeRouting`,
// and instead we add this interface type.
//
// TODO: delete me after https://github.com/golang/go/issues/63285 is fixed
type GoBug63285RouteInterface[T any] interface {
	GetPublishers(
		ctx context.Context,
	) Publishers[T]
}

type ProcessorRouting = processor.FromKernel[*NodeKernel]
type NodeRouting[T any] = node.NodeWithCustomData[GoBug63285RouteInterface[T], *ProcessorRouting]

type Sender interface {
	Close(context.Context) error
}
type NodeRetryOutput = node.NodeWithCustomData[Sender, *processor.FromKernel[*kernel.Retryable[*kernel.Output]]]

func newRetryOutputNode(
	ctx context.Context,
	sender Sender,
	waitForInputFunc func(context.Context) error,
	dstURL string,
	streamKey secret.String,
	cfg kernel.OutputConfig,
) *NodeRetryOutput {
	logger.Tracef(ctx, "newRetryOutputNode")
	defer func() { logger.Tracef(ctx, "/newRetryOutputNode") }()

	outputKernel := kernel.NewRetryable(xlogger.CtxWithMaxLoggingLevel(ctx, logger.LevelWarning),
		func(ctx context.Context) (*kernel.Output, error) {
			if waitForInputFunc != nil {
				err := waitForInputFunc(ctx)
				if err != nil {
					return nil, fmt.Errorf("unable to wait for input: %w", err)
				}
			}
			return kernel.NewOutputFromURL(ctx, dstURL, streamKey, cfg)
		},
		func(ctx context.Context, k *kernel.Output, err error) error {
			logger.Debugf(ctx, "connection ended: %v", err)
			time.Sleep(time.Second)
			return kernel.ErrRetry{Err: err}
		},
	)
	n := node.NewWithCustomDataFromKernel[Sender](
		ctx, outputKernel, processor.DefaultOptionsOutput()...,
	)
	n.CustomData = sender
	return n
}
