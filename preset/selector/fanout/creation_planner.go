package fanout

import (
	"context"
	"errors"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

var ErrCreationRejected = errors.New("fanout creation rejected")

type CreationPlanner[K comparable] interface {
	PlanCreate(ctx context.Context, requested K, existing []ExistingMember[K]) (CreationDecision[K], error)
}

type CreationPlannerFunc[K comparable] func(
	ctx context.Context,
	requested K,
	existing []ExistingMember[K],
) (CreationDecision[K], error)

func (f CreationPlannerFunc[K]) PlanCreate(
	ctx context.Context,
	requested K,
	existing []ExistingMember[K],
) (CreationDecision[K], error) {
	return f(ctx, requested, existing)
}

type ModeCreationPlanner[K comparable] struct {
	mode     Mode
	routeIDs []id.RouteID
}

func NewCreationPlanner[K comparable](
	mode Mode,
	routeIDs ...id.RouteID,
) *ModeCreationPlanner[K] {
	return &ModeCreationPlanner[K]{
		mode:     mode,
		routeIDs: append([]id.RouteID(nil), routeIDs...),
	}
}

func (p *ModeCreationPlanner[K]) PlanCreate(
	_ context.Context,
	requested K,
	existing []ExistingMember[K],
) (CreationDecision[K], error) {
	switch p.mode {
	case ModeForbid:
		return p.planForbid(requested, existing), nil
	case ModeSameOutputSameTracks, ModeSameOutputDifferentTracks:
		return p.planSameOutput(requested, existing), nil
	case ModeDifferentOutputsSameTracks, ModeDifferentOutputsSameTracksSplitAV:
		return p.planDifferentOutputs(requested, existing), nil
	default:
		return rejectDecision(requested), nil
	}
}

func (p *ModeCreationPlanner[K]) planForbid(
	requested K,
	existing []ExistingMember[K],
) CreationDecision[K] {
	if len(existing) == 0 {
		return p.createDecision(requested)
	}

	return rejectDecision(requested)
}

func (p *ModeCreationPlanner[K]) planSameOutput(
	requested K,
	existing []ExistingMember[K],
) CreationDecision[K] {
	if len(existing) == 0 {
		return p.createDecision(requested)
	}

	return p.reuseDecision(requested, existing[0])
}

func (p *ModeCreationPlanner[K]) planDifferentOutputs(
	requested K,
	existing []ExistingMember[K],
) CreationDecision[K] {
	for _, existingMember := range existing {
		if existingMember.StorageKey == requested {
			return p.reuseDecision(requested, existingMember)
		}
	}

	return p.createDecision(requested)
}

func (p *ModeCreationPlanner[K]) createDecision(
	requested K,
) CreationDecision[K] {
	return CreationDecision[K]{
		Action:        CreationActionCreate,
		StorageKey:    requested,
		ReuseMemberID: id.NoMemberID,
		RouteIDs:      append([]id.RouteID(nil), p.routeIDs...),
	}
}

func (p *ModeCreationPlanner[K]) reuseDecision(
	requested K,
	existing ExistingMember[K],
) CreationDecision[K] {
	return CreationDecision[K]{
		Action:        CreationActionReuse,
		StorageKey:    requested,
		ReuseMemberID: existing.ID,
		RouteIDs:      append([]id.RouteID(nil), p.routeIDs...),
	}
}

func rejectDecision[K comparable](
	requested K,
) CreationDecision[K] {
	return CreationDecision[K]{
		Action:        CreationActionReject,
		StorageKey:    requested,
		ReuseMemberID: id.NoMemberID,
	}
}
