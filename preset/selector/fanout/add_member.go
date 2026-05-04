package fanout

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

func (c *Controller[K, M]) AddMember(
	ctx context.Context,
	storageKey K,
	value M,
) (member.Entry[K, M], CreationDecision[K], error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	existing := c.snapshotExistingMembers(ctx)
	decision, err := c.creationPlanner.PlanCreate(ctx, storageKey, existing)
	if err != nil {
		return member.Entry[K, M]{}, decision, fmt.Errorf(
			"plan creation for storage key %s: %w",
			safekey.Format(ctx, c.safeKeyFormatter, storageKey),
			err,
		)
	}

	switch decision.Action {
	case CreationActionCreate:
		return c.addCreatedMember(ctx, decision, value)
	case CreationActionReuse:
		return c.reuseMember(ctx, decision)
	case CreationActionReject:
		return member.Entry[K, M]{}, decision, fmt.Errorf(
			"storage key %s: %w",
			safekey.Format(ctx, c.safeKeyFormatter, decision.StorageKey),
			ErrCreationRejected,
		)
	default:
		return member.Entry[K, M]{}, decision, selectorerr.InvalidRoutePlan("", fmt.Errorf("unknown creation action %d", decision.Action))
	}
}

func (c *Controller[K, M]) snapshotExistingMembers(
	ctx context.Context,
) []ExistingMember[K] {
	var existing []ExistingMember[K]
	c.members.Range(ctx, func(entry member.Entry[K, M]) bool {
		existing = append(existing, ExistingMember[K]{
			ID:         entry.ID,
			StorageKey: entry.StorageKey,
		})
		return true
	})

	return existing
}

func (c *Controller[K, M]) addCreatedMember(
	ctx context.Context,
	decision CreationDecision[K],
	value M,
) (member.Entry[K, M], CreationDecision[K], error) {
	if err := validateRouteIDs(ctx, c.routes, decision.RouteIDs); err != nil {
		return member.Entry[K, M]{}, decision, err
	}

	memberID, err := c.memberIDs.Allocate(ctx)
	if err != nil {
		return member.Entry[K, M]{}, decision, fmt.Errorf(
			"allocate member for storage key %s: %w",
			safekey.Format(ctx, c.safeKeyFormatter, decision.StorageKey),
			err,
		)
	}

	entry, err := c.members.Put(ctx, memberID, decision.StorageKey, value)
	if err != nil {
		return member.Entry[K, M]{}, decision, fmt.Errorf(
			"put member %d storage key %s: %w",
			memberID,
			safekey.Format(ctx, c.safeKeyFormatter, decision.StorageKey),
			err,
		)
	}
	if err := c.attachRoutes(ctx, entry.ID, decision.RouteIDs); err != nil {
		return member.Entry[K, M]{}, decision, err
	}

	return entry, decision, nil
}

func (c *Controller[K, M]) reuseMember(
	ctx context.Context,
	decision CreationDecision[K],
) (member.Entry[K, M], CreationDecision[K], error) {
	if decision.ReuseMemberID == id.NoMemberID {
		return member.Entry[K, M]{}, decision, selectorerr.InvalidRoutePlan("", selectorerr.MemberNotFound(decision.ReuseMemberID))
	}

	entry, ok := c.members.LoadByID(ctx, decision.ReuseMemberID)
	if !ok {
		return member.Entry[K, M]{}, decision, selectorerr.InvalidRoutePlan("", selectorerr.MemberNotFound(decision.ReuseMemberID))
	}
	if len(decision.RouteIDs) == 0 {
		return entry, decision, nil
	}
	if err := validateRouteIDs(ctx, c.routes, decision.RouteIDs); err != nil {
		return member.Entry[K, M]{}, decision, err
	}
	if err := c.attachRoutes(ctx, entry.ID, decision.RouteIDs); err != nil {
		return member.Entry[K, M]{}, decision, err
	}

	return entry, decision, nil
}

func (c *Controller[K, M]) attachRoutes(
	ctx context.Context,
	memberID id.MemberID,
	routeIDs []id.RouteID,
) error {
	for _, routeID := range routeIDs {
		if err := c.attachments.Attach(ctx, routeID, memberID); err != nil {
			return selectorerr.InvalidRoutePlan(routeID, err)
		}
	}

	return nil
}
