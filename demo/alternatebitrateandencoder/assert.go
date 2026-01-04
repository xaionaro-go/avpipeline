// assert.go simple assertion function.
package main

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
