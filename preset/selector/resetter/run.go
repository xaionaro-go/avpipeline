package resetter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
)

var errInvalidTimeout = errors.New("invalid per-reset timeout")

type resetContextFunc func(
	ctx context.Context,
	timeout time.Duration,
) (context.Context, context.CancelFunc)

// Run executes resetters in order with per-reset timeout containment.
func Run(
	ctx context.Context,
	owner string,
	perResetTimeout time.Duration,
	resetters []Named,
) error {
	return run(ctx, owner, perResetTimeout, resetters, context.WithTimeout)
}

func run(
	ctx context.Context,
	owner string,
	perResetTimeout time.Duration,
	resetters []Named,
	newResetContext resetContextFunc,
) error {
	if perResetTimeout <= 0 {
		return fmt.Errorf("resetter %q: %w: %s", owner, errInvalidTimeout, perResetTimeout)
	}

	var errs []error
	for _, resetter := range resetters {
		if resetter.Resetter == nil {
			continue
		}

		resetCtx, cancel := newResetContext(ctx, perResetTimeout)
		err := resetter.Resetter.Reset(resetCtx)
		resetCtxErr := resetCtx.Err()
		cancel()

		switch {
		case errors.Is(resetCtxErr, context.DeadlineExceeded) && ctx.Err() == nil:
			logger.Warnf(
				ctx,
				"resetter[%s]: %s reset timed out after %s; skipping",
				owner,
				resetter.Name,
				perResetTimeout,
			)
		case err != nil:
			errs = append(errs, fmt.Errorf("resetter %q: unable to reset %s: %w", owner, resetter.Name, err))
		}
	}

	return errors.Join(errs...)
}
