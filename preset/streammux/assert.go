package streammux

import (
	"context"
	"runtime/debug"

	"github.com/xaionaro-go/avpipeline/logger"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func assertNoError(err error) {
	if err != nil {
		panic(err)
	}
}

func assert(ctx context.Context, condition bool) {
	if !condition {
		logger.Fatalf(ctx, "assertion failed: %s", debug.Stack())
	}
}
