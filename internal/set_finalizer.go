package internal

import (
	"context"
	"runtime"

	"github.com/xaionaro-go/avpipeline/logger"
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

func SetFinalizerClose[T interface{ Close(context.Context) error }](
	ctx context.Context,
	freer T,
) {
	runtime.SetFinalizer(freer, func(freer T) {
		logger.Debugf(ctx, "freeing %T", freer)
		err := freer.Close(ctx)
		if err != nil {
			logger.Errorf(ctx, "failed to close %T: %v", freer, err)
		}
	})
}

func SetFinalizer[T any](
	ctx context.Context,
	obj T,
	callback func(in T),
) {
	runtime.SetFinalizer(obj, callback)
}
