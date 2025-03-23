package main

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
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
