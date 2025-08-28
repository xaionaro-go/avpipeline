package streammux

import (
	"context"

	"github.com/xaionaro-go/avpipeline/internal"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func assertNoError(err error) {
	if err != nil {
		panic(err)
	}
}

func assert(
	ctx context.Context,
	mustBeTrue bool,
	extraArgs ...any,
) {
	internal.Assert(ctx, mustBeTrue, extraArgs)
}
