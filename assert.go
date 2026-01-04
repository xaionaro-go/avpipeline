// assert.go provides internal assertion helpers for the avpipeline package.

// Package avpipeline provides the core functionality for building and managing media pipelines.
package avpipeline

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
)

func assert(
	ctx context.Context,
	isTrue bool,
	args ...any,
) {
	if !isTrue {
		logger.Panicf(ctx, "an assertion failed; additional data: %v", args)
	}
}
