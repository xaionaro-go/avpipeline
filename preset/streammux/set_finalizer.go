package streammux

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
