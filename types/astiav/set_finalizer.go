// set_finalizer.go provides internal helpers for setting finalizers on astiav objects.

package astiav

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
