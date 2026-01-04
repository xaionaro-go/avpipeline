// assert.go provides a simple assertion helper.

package boilerplate

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
)

func assert(
	ctx context.Context,
	condition bool,
	args ...any,
) {
	if !condition {
		logger.Panicf(ctx, "assertion failed: %v", args)
	}
}
