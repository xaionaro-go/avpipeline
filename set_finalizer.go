package avpipeline

import (
	"context"
	"runtime"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func setFinalizerFree[T interface{ Free() }](
	ctx context.Context,
	freer T,
) {
	runtime.SetFinalizer(freer, func(freer T) {
		logger.Debugf(ctx, "freeing %T", freer)
		freer.Free()
	})
}
