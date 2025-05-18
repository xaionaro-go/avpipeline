package router

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/xsync"
)

func (r *Route) ResetNode(ctx context.Context) (_ret error) {
	logger.Debugf(ctx, "ResetNode")
	defer func() { logger.Debugf(ctx, "/ResetNode: %v", _ret) }()
	var wg sync.WaitGroup
	defer wg.Wait()
	return xsync.DoA2R1(ctx, &r.Node.Locker, r.resetNodeLocked, ctx, &wg)
}

func (r *Route) resetNodeLocked(
	ctx context.Context,
	wg *sync.WaitGroup,
) (_ret error) {
	logger.Debugf(ctx, "resetNodeLocked")
	defer func() { logger.Debugf(ctx, "/resetNodeLocked: %v", _ret) }()
	var errs []error
	if err := r.closeNodeLocked(ctx, wg); err != nil {
		if !errors.Is(err, ErrAlreadyClosed{}) {
			errs = append(errs, fmt.Errorf("unable to close the old node: %w", err))
		}
	}
	r.openNodeLocked(ctx)
	return errors.Join(errs...)
}
