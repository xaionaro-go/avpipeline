package selector

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/fanout"
)

func NewFanOut[K comparable, M any](
	ctx context.Context,
	cfg fanout.Config[K, M],
) (*fanout.Controller[K, M], error) {
	controller, err := fanout.New(ctx, cfg)
	if err != nil {
		return nil, wrapConstructorConfigError("fanout", err)
	}

	return controller, nil
}
