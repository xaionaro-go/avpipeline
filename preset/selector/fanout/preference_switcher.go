package fanout

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

type PreferenceSwitcher[K comparable, M any] struct {
	safeKeyFormatter safekey.Formatter[K]
}

type preferredRouteSnapshot[K comparable] struct {
	routeID    id.RouteID
	storageKey K
	memberID   id.MemberID
	pair       *switchpair.Pair
	current    id.MemberID
	syncer     id.MemberID
}

type routeStateSnapshotter interface {
	Current(ctx context.Context) id.MemberID
	SyncerCurrent(ctx context.Context) id.MemberID
}

func NewPreferenceSwitcher[K comparable, M any](
	safeKeyFormatter safekey.Formatter[K],
) *PreferenceSwitcher[K, M] {
	return &PreferenceSwitcher[K, M]{
		safeKeyFormatter: safeKeyFormatter,
	}
}

func (s *PreferenceSwitcher[K, M]) withSafeKeyFormatter(
	safeKeyFormatter safekey.Formatter[K],
) *PreferenceSwitcher[K, M] {
	if safeKeyFormatter == nil || s.safeKeyFormatter != nil {
		return s
	}

	cloned := *s
	cloned.safeKeyFormatter = safeKeyFormatter
	return &cloned
}

func (s *PreferenceSwitcher[K, M]) SwitchPreferred(
	ctx context.Context,
	plans []RoutePlan[K],
	routes *route.Registry,
	members *member.Registry[K, M],
	attachments *attachment.Index,
) error {
	snapshots, err := s.validatePlans(ctx, plans, routes, members, attachments)
	if err != nil {
		return err
	}

	return s.switchValidatedSnapshots(ctx, snapshots)
}

func (s *PreferenceSwitcher[K, M]) switchValidatedSnapshots(
	ctx context.Context,
	snapshots []preferredRouteSnapshot[K],
) error {
	var alreadyPreferred []id.RouteID
	for _, snapshot := range snapshots {
		if snapshot.current != snapshot.syncer {
			return ErrSwitchAlreadyInProgress{
				RouteID:        snapshot.routeID,
				SwitchMemberID: snapshot.current,
				SyncerMemberID: snapshot.syncer,
			}
		}
		if snapshot.current == snapshot.memberID {
			alreadyPreferred = append(alreadyPreferred, snapshot.routeID)
		}
	}
	if len(alreadyPreferred) == len(snapshots) {
		return ErrAllAlreadyPreferred{RouteIDs: alreadyPreferred}
	}

	for _, snapshot := range snapshots {
		if snapshot.current == snapshot.memberID {
			continue
		}
		if err := snapshot.pair.SetValue(ctx, snapshot.memberID); err != nil {
			return fmt.Errorf(
				"switch route %q to storage key %s: %w",
				snapshot.routeID,
				safekey.Format(ctx, s.safeKeyFormatter, snapshot.storageKey),
				err,
			)
		}
	}

	return nil
}

func (s *PreferenceSwitcher[K, M]) validatePlans(
	ctx context.Context,
	plans []RoutePlan[K],
	routes *route.Registry,
	members *member.Registry[K, M],
	attachments *attachment.Index,
) ([]preferredRouteSnapshot[K], error) {
	if err := validateRoutePlansSyntax(plans); err != nil {
		return nil, err
	}

	snapshots := make([]preferredRouteSnapshot[K], 0, len(plans))
	for _, plan := range plans {
		state, ok := routes.Load(ctx, plan.RouteID)
		if !ok {
			return nil, selectorerr.RouteNotFound(plan.RouteID)
		}
		if state.Pair == nil {
			return nil, selectorerr.InvalidConfig("routes", fmt.Errorf("route %q: missing switch pair", plan.RouteID))
		}

		entry, ok := members.LoadByStorageKey(ctx, plan.StorageKey)
		if !ok {
			return nil, fmt.Errorf(
				"%w: storage key %s",
				selectorerr.ErrMemberNotFound,
				safekey.Format(ctx, s.safeKeyFormatter, plan.StorageKey),
			)
		}
		if !isMemberAttachedToRoute(ctx, attachments, plan.RouteID, entry.ID) {
			return nil, unattachedRoutePlanError(
				plan.RouteID,
				safekey.Format(ctx, s.safeKeyFormatter, plan.StorageKey),
			)
		}

		current, syncer := currentAndSyncer(ctx, state.Pair)
		snapshots = append(snapshots, preferredRouteSnapshot[K]{
			routeID:    plan.RouteID,
			storageKey: plan.StorageKey,
			memberID:   entry.ID,
			pair:       state.Pair,
			current:    current,
			syncer:     syncer,
		})
	}

	return snapshots, nil
}

func currentAndSyncer(
	ctx context.Context,
	snapshotter routeStateSnapshotter,
) (id.MemberID, id.MemberID) {
	syncer := snapshotter.SyncerCurrent(ctx)
	current := snapshotter.Current(ctx)

	return current, syncer
}

func isMemberAttachedToRoute(
	ctx context.Context,
	attachments *attachment.Index,
	routeID id.RouteID,
	memberID id.MemberID,
) bool {
	for _, attachedMemberID := range attachments.MembersForRoute(ctx, routeID) {
		if attachedMemberID == memberID {
			return true
		}
	}

	return false
}
