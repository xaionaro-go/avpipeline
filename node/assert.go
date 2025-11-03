package node

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

func assertSoft(
	ctx context.Context,
	mustBeTrue bool,
	extraArgs ...any,
) {
	internal.AssertSoft(ctx, mustBeTrue, extraArgs...)
}
