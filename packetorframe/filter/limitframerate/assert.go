package limitframerate

import (
	"context"

	"github.com/xaionaro-go/avpipeline/internal"
)

func assert(
	ctx context.Context,
	mustBeTrue bool,
	extraArgs ...any,
) {
	internal.Assert(ctx, mustBeTrue, extraArgs...)
}
