package router

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability/xlogger"
	"github.com/xaionaro-go/secret"
)

type ProcessorRouting = processor.FromKernel[*kernel.MapStreamIndices]
type NodeRouting[T any] = node.NodeWithCustomData[*Route[T], *ProcessorRouting]

type Sender interface {
	Close(context.Context) error
}
type NodeRetryOutput = node.NodeWithCustomData[Sender, *processor.FromKernel[*kernel.Retry[*kernel.Output]]]

func newRetryOutputNode(
	ctx context.Context,
	sender Sender,
	waitForInputFunc func(context.Context) error,
	dstURL string,
	streamKey secret.String,
	cfg kernel.OutputConfig,
) *NodeRetryOutput {
	logger.Tracef(ctx, "newOutputNode")
	defer func() { logger.Tracef(ctx, "/newOutputNode") }()

	outputKernel := kernel.NewRetry(xlogger.CtxWithMaxLoggingLevel(ctx, logger.LevelWarning),
		func(ctx context.Context) (*kernel.Output, error) {
			if waitForInputFunc != nil {
				err := waitForInputFunc(ctx)
				if err != nil {
					return nil, fmt.Errorf("unable to wait for input: %w", err)
				}
			}
			return kernel.NewOutputFromURL(ctx, dstURL, streamKey, cfg)
		},
		func(ctx context.Context, k *kernel.Output) error {
			return nil
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
