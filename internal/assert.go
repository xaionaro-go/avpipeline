package internal

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
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
		logger.Panic(ctx, "assertion failed")
		return
	}

	logger.Panic(ctx, "assertion failed", extraArgs)
}
