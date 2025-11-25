//go:build with_libav
// +build with_libav

package monitor

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
