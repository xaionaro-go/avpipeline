package transcoderwithpassthrough

import (
	"context"
	"runtime"

	"github.com/xaionaro-go/avpipeline/logger"
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
