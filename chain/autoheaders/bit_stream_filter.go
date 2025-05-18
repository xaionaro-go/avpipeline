package autoheaders

import (
	"context"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/bitstreamfilter"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
)

func tryNewBSFForInBandHeaders[T any](
	ctx context.Context,
) (_ret *NodeWithCustomData[T]) {
	logger.Debugf(ctx, "tryNewBSFForInBandHeaders(ctx)")
	defer func() {
		logger.Debugf(ctx, "/tryNewBSFForInBandHeaders(ctx): %s", spew.Sdump(_ret))
	}()

	bitstreamFilter, err := kernel.NewBitstreamFilter(ctx, bitstreamfilter.ParamsGetterToInBandHeaders{})
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the bitstream filter kernel: %w", err)
		return nil
	}

	return node.NewWithCustomDataFromKernel[T](
		ctx,
		bitstreamFilter,
		processor.DefaultOptionsOutput()...,
	)
}

func tryNewBSFForOOBHeaders[T any](
	ctx context.Context,
) (_ret *NodeWithCustomData[T]) {
	logger.Debugf(ctx, "tryNewBSFForOOBHeaders(ctx)")
	defer func() {
		logger.Debugf(ctx, "/tryNewBSFForOOBHeaders(ctx): %s", spew.Sdump(_ret))
	}()

	bitstreamFilter, err := kernel.NewBitstreamFilter(ctx, bitstreamfilter.ParamsGetterToOOBHeaders{})
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the bitstream filter kernel: %w", err)
		return nil
	}

	return node.NewWithCustomDataFromKernel[T](
		ctx,
		bitstreamFilter,
		processor.DefaultOptionsOutput()...,
	)
}
