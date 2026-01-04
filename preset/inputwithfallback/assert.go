// assert.go provides an assertion helper for the inputwithfallback package.

package inputwithfallback

import (
	"context"

	"github.com/xaionaro-go/avpipeline/internal"
)

func assert(
	ctx context.Context,
	mustBeTrue bool,
	extraArgs ...any,
) {
	internal.Assert(ctx, mustBeTrue, extraArgs)
}
