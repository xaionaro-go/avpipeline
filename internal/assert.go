package internal

import (
	"context"
	"runtime/debug"

	"github.com/xaionaro-go/avpipeline/logger"
)

func Assert(
	ctx context.Context,
	mustBeTrue bool,
	extraArgs ...any,
) {
	if mustBeTrue {
		return
	}

	if len(extraArgs) == 0 {
		logger.Panicf(ctx, "assertion failed:\n%s", debug.Stack())
		return
	}

	logger.Panic(ctx, "assertion failed:\nExtra args:", extraArgs, "\n", string(debug.Stack()))
}
