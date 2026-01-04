// assert.go provides internal assertion helpers for the condition package.

package condition

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
