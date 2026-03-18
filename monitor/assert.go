//go:build with_libav
// +build with_libav

// assert.go provides internal assertion helpers for the monitor package.

package monitor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
)

func assert(
	ctx context.Context,
	isTrue bool,
	fmtAndArgs ...any,
) {
	if !isTrue {
		if len(fmtAndArgs) > 0 {
			if fmtStr, ok := fmtAndArgs[0].(string); ok {
				logger.Panicf(ctx, "assertion failed: "+fmtStr, fmtAndArgs[1:]...)
				return
			}
		}
		logger.Panicf(ctx, "assertion failed; additional data: %v", fmtAndArgs)
	}
}
