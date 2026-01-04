// bit_stream_filter.go provides helpers to create bitstream filters for header fixing.

// Package autoheaders provides a preset that automatically fixes stream headers.
package autoheaders

import (
	"context"

	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/bitstreamfilter"
	"github.com/xaionaro-go/avpipeline/logger"
)

func tryNewBSFForInBandHeaders(
	ctx context.Context,
) (_ret *kernel.BitstreamFilter) {
	logger.Debugf(ctx, "tryNewBSFForInBandHeaders(ctx)")
	defer func() {
		logger.Debugf(ctx, "/tryNewBSFForInBandHeaders(ctx): %s", spew.Sdump(_ret))
	}()

	bitstreamFilter, err := kernel.NewBitstreamFilter(ctx, bitstreamfilter.ParamsGetterToInBandHeaders{})
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the bitstream filter kernel: %w", err)
		return nil
	}

	return bitstreamFilter
}

func tryNewBSFForOOBHeaders(
	ctx context.Context,
) (_ret *kernel.BitstreamFilter) {
	logger.Debugf(ctx, "tryNewBSFForOOBHeaders(ctx)")
	defer func() {
		logger.Debugf(ctx, "/tryNewBSFForOOBHeaders(ctx): %s", spew.Sdump(_ret))
	}()

	bitstreamFilter, err := kernel.NewBitstreamFilter(ctx, bitstreamfilter.ParamsGetterToOOBHeaders{})
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the bitstream filter kernel: %w", err)
		return nil
	}

	return bitstreamFilter
}

func tryNewBSFForCorrectedOOBHeaders(
	ctx context.Context,
) (_ret *kernel.BitstreamFilter) {
	logger.Debugf(ctx, "tryNewBSFForCorrectedOOBHeaders(ctx)")
	defer func() {
		logger.Debugf(ctx, "/tryNewBSFForCorrectedOOBHeaders(ctx): %s", spew.Sdump(_ret))
	}()

	bitstreamFilter, err := kernel.NewBitstreamFilter(ctx, bitstreamfilter.ParamsGetterToCorrectedOOBHeaders{})
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the bitstream filter kernel: %w", err)
		return nil
	}

	return bitstreamFilter
}
