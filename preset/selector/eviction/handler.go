package eviction

import (
	"context"
	"errors"
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

var ErrMissingRoutePair = errors.New("missing route switch pair")

type Handler[K comparable, M any] struct {
	members          *member.Registry[K, M]
	routes           *route.Registry
	attachments      *attachment.Index
	retryTracker     RetryTracker[K]
	recommit         RecommitFunc[K]
	recreate         RecreateFunc[K]
	safeKeyFormatter safekey.Formatter[K]
}

type routeSnapshot struct {
	id         id.RouteID
	pair       *switchpair.Pair
	willDemote bool
}

func NewHandler[K comparable, M any](
	cfg Config[K, M],
) (*Handler[K, M], error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	return &Handler[K, M]{
		members:          cfg.Members,
		routes:           cfg.Routes,
		attachments:      cfg.Attachments,
		retryTracker:     cfg.RetryTracker,
		recommit:         cfg.Recommit,
		recreate:         cfg.Recreate,
		safeKeyFormatter: cfg.SafeKeyFormatter,
	}, nil
}

func (h *Handler[K, M]) Evict(
	ctx context.Context,
	dead member.Entry[K, M],
) (Result, error) {
	if !h.members.CompareAndDelete(ctx, dead) {
		return Result{}, nil
	}

	routes, err := h.snapshotAttachedRoutes(ctx, dead.ID)
	if err != nil {
		return Result{}, err
	}

	for _, routeID := range h.attachments.RoutesForMember(ctx, dead.ID) {
		h.attachments.Detach(ctx, routeID, dead.ID)
	}

	liveMembers := h.snapshotMembers(ctx)
	return h.evictRoutes(ctx, dead, routes, liveMembers)
}

func (h *Handler[K, M]) snapshotAttachedRoutes(
	ctx context.Context,
	deadID id.MemberID,
) ([]routeSnapshot, error) {
	routeIDs := h.attachments.RoutesForMember(ctx, deadID)
	snapshots := make([]routeSnapshot, 0, len(routeIDs))
	for _, routeID := range routeIDs {
		state, ok := h.routes.Load(ctx, routeID)
		if !ok {
			return nil, unknownRouteError(routeID)
		}
		if state.Pair == nil {
			return nil, selectorerr.InvalidConfig(
				"routes",
				fmt.Errorf("route %q: %w", routeID, ErrMissingRoutePair),
			)
		}

		snapshots = append(snapshots, routeSnapshot{
			id:         routeID,
			pair:       state.Pair,
			willDemote: state.Pair.Current(ctx) == deadID || state.Pair.SyncerCurrent(ctx) == deadID,
		})
	}

	return snapshots, nil
}

func unknownRouteError(
	routeID id.RouteID,
) error {
	return fmt.Errorf("%w: route %q", route.ErrUnknownRouteID, routeID)
}

func (h *Handler[K, M]) snapshotMembers(
	ctx context.Context,
) map[id.MemberID]member.Entry[K, M] {
	liveMembers := map[id.MemberID]member.Entry[K, M]{}
	h.members.Range(ctx, func(entry member.Entry[K, M]) bool {
		liveMembers[entry.ID] = entry
		return true
	})

	return liveMembers
}

func (h *Handler[K, M]) evictRoutes(
	ctx context.Context,
	dead member.Entry[K, M],
	routes []routeSnapshot,
	liveMembers map[id.MemberID]member.Entry[K, M],
) (Result, error) {
	var result Result
	for _, route := range routes {
		if !route.willDemote {
			continue
		}
		h.recordRetry(ctx, route.id, dead.StorageKey, &result)
	}

	var errs []error
	for _, route := range routes {
		if !route.willDemote {
			continue
		}

		demotion := route.pair.DemoteIfCurrent(ctx, dead.ID)
		if !demotion.SwitchDemoted && !demotion.SyncerDemoted {
			continue
		}
		result.DemotedRoutes = append(result.DemotedRoutes, route.id)

		if sibling, ok := h.findSibling(ctx, route.id, dead.ID, liveMembers); ok {
			if err := h.recommitRoute(ctx, route.id, sibling.StorageKey); err != nil {
				errs = append(errs, err)
				continue
			}
			result.Recommitted = append(result.Recommitted, route.id)
			continue
		}

		if err := h.recreateRoute(ctx, route.id, dead.StorageKey, &result); err != nil {
			errs = append(errs, err)
		}
	}

	return result, errors.Join(errs...)
}

func (h *Handler[K, M]) recordRetry(
	ctx context.Context,
	routeID id.RouteID,
	storageKey K,
	result *Result,
) {
	if h.retryTracker == nil {
		return
	}

	h.retryTracker.RecordDemotion(ctx, routeID, storageKey)
	result.RetryRecorded = append(result.RetryRecorded, routeID)
}

func (h *Handler[K, M]) recommitRoute(
	ctx context.Context,
	routeID id.RouteID,
	storageKey K,
) error {
	if err := h.recommit(ctx, routeID, storageKey); err != nil {
		return fmt.Errorf(
			"recommit route %q storage key %s: %w",
			routeID,
			safekey.Format(ctx, h.safeKeyFormatter, storageKey),
			err,
		)
	}

	return nil
}

func (h *Handler[K, M]) recreateRoute(
	ctx context.Context,
	routeID id.RouteID,
	storageKey K,
	result *Result,
) error {
	if h.recreate == nil {
		return nil
	}

	if err := h.recreate(ctx, routeID, storageKey); err != nil {
		wrapped := fmt.Errorf(
			"recreate route %q storage key %s: %w",
			routeID,
			safekey.Format(ctx, h.safeKeyFormatter, storageKey),
			err,
		)
		result.RecreateErrs = append(result.RecreateErrs, wrapped)
		return wrapped
	}

	result.Recreated = append(result.Recreated, routeID)
	return nil
}
