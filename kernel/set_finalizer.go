package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/internal"
)

func setFinalizerFree[T interface{ Free() }](
	ctx context.Context,
	freer T,
) {
	internal.SetFinalizerFree(ctx, freer)
}

func setFinalizer[T any](
	ctx context.Context,
	obj T,
	callback func(T),
) {
	internal.SetFinalizer(ctx, obj, callback)
}
