package selector

import (
	"context"
	"errors"

	"github.com/xaionaro-go/avpipeline/preset/selector/fanin"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

func NewFanIn[K comparable, M fanin.Member](
	ctx context.Context,
	cfg fanin.Config[K, M],
) (*fanin.Controller[K, M], error) {
	controller, err := fanin.New(ctx, cfg)
	if err != nil {
		return nil, wrapConstructorConfigError("fanin", err)
	}

	return controller, nil
}

func wrapConstructorConfigError(
	direction string,
	err error,
) error {
	if !errors.Is(err, selectorerr.ErrInvalidConfig) {
		return err
	}

	return selectorerr.InvalidConfig(direction, err)
}
