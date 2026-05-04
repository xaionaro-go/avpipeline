package fanin

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

type PromotionDecision struct {
	Promote bool
	Target  id.MemberID
}

type OpenPromotion[K comparable, M Member] struct{}

func (p *OpenPromotion[K, M]) PlanOpen(
	ctx context.Context,
	current id.MemberID,
	opened id.MemberID,
	candidates []PriorityCandidate[K, M],
) (PromotionDecision, error) {
	candidate, ok := findCandidate(candidates, opened)
	if !ok {
		return PromotionDecision{}, selectorerr.MemberNotFound(opened)
	}
	if !isAvailable(ctx, candidate.Availability) {
		return PromotionDecision{}, nil
	}
	if current != id.NoMemberID && opened >= current {
		return PromotionDecision{}, nil
	}

	return PromotionDecision{
		Promote: true,
		Target:  opened,
	}, nil
}
