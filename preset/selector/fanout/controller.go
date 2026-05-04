package fanout

import (
	"context"
	"fmt"
	"sync"

	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/eviction"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
)

type Controller[K comparable, M any] struct {
	lock                  sync.Mutex
	routes                *route.Registry
	members               *member.Registry[K, M]
	attachments           *attachment.Index
	memberIDs             member.Allocator
	creationPlanner       CreationPlanner[K]
	preferredRoutePlanner PreferredRoutePlanner[K]
	differentOutputPolicy DifferentOutputPolicy
	preferenceSwitcher    *PreferenceSwitcher[K, M]
	eviction              EvictionHandler[K, M]
	retryTracker          RetryTracker[K]
	safeKeyFormatter      safekey.Formatter[K]
}

func New[K comparable, M any](
	_ context.Context,
	cfg Config[K, M],
) (*Controller[K, M], error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	return &Controller[K, M]{
		routes:                cfg.Routes,
		members:               cfg.Members,
		attachments:           cfg.Attachments,
		memberIDs:             cfg.MemberIDs,
		creationPlanner:       cfg.CreationPlanner,
		preferredRoutePlanner: cfg.PreferredRoutePlanner,
		differentOutputPolicy: cfg.DifferentOutputPolicy,
		preferenceSwitcher:    cfg.PreferenceSwitcher.withSafeKeyFormatter(cfg.SafeKeyFormatter),
		eviction:              cfg.Eviction,
		retryTracker:          cfg.RetryTracker,
		safeKeyFormatter:      cfg.SafeKeyFormatter,
	}, nil
}

var _ EvictionHandler[string, struct{}] = (*eviction.Handler[string, struct{}])(nil)

func (c *Controller[K, M]) SetPreferred(
	ctx context.Context,
	requested K,
) error {
	snapshots, err := c.snapshotPreferredSwitch(ctx, requested)
	if err != nil {
		return err
	}

	return c.preferenceSwitcher.switchValidatedSnapshots(ctx, snapshots)
}

func (c *Controller[K, M]) snapshotPreferredSwitch(
	ctx context.Context,
	requested K,
) ([]preferredRouteSnapshot[K], error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if !c.differentOutputPolicy.AllowsDifferentOutputs(ctx) {
		return nil, fmt.Errorf(
			"storage key %s: %w",
			safekey.Format(ctx, c.safeKeyFormatter, requested),
			ErrDifferentOutputsNotAllowed,
		)
	}

	plans, err := c.preferredRoutePlanner.PlanPreferred(ctx, requested)
	if err != nil {
		return nil, fmt.Errorf(
			"plan preferred storage key %s: %w",
			safekey.Format(ctx, c.safeKeyFormatter, requested),
			err,
		)
	}

	return c.preferenceSwitcher.validatePlans(ctx, plans, c.routes, c.members, c.attachments)
}
