package internal

import (
	"context"
	"runtime"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func SetFinalizerFree[T interface{ Free() }](
	ctx context.Context,
	freer T,
) {
	runtime.SetFinalizer(freer, func(freer T) {
		logger.Debugf(ctx, "freeing %T", freer)
		freer.Free()
	})
}

func SetFinalizer[T any](
	ctx context.Context,
	obj T,
	callback func(in T),
) {
	runtime.SetFinalizer(obj, callback)
}
