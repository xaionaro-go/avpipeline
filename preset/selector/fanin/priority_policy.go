package fanin

import (
	"context"
	"fmt"
	"slices"

	"github.com/xaionaro-go/avpipeline/preset/selector/availability"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

type PriorityCandidate[K comparable, M Member] struct {
	Priority     id.MemberID
	Entry        member.Entry[K, M]
	Availability availability.Candidate
}

type PriorityPolicy[K comparable, M Member] struct{}

func (p *PriorityPolicy[K, M]) FirstAvailableAfter(
	ctx context.Context,
	candidates []PriorityCandidate[K, M],
	after id.MemberID,
) (id.MemberID, bool, error) {
	ordered, err := sortedPriorityCandidates(candidates)
	if err != nil {
		return 0, false, err
	}

	for _, candidate := range ordered {
		if candidate.Priority <= after {
			continue
		}
		if !isAvailable(ctx, candidate.Availability) {
			continue
		}
		return candidate.Priority, true, nil
	}

	return 0, false, nil
}

func sortedPriorityCandidates[K comparable, M Member](
	candidates []PriorityCandidate[K, M],
) ([]PriorityCandidate[K, M], error) {
	ordered := append([]PriorityCandidate[K, M](nil), candidates...)
	for _, candidate := range ordered {
		if candidate.Priority < 0 {
			return nil, selectorerr.InvalidConfig(
				"priority",
				fmt.Errorf("negative priority %d", candidate.Priority),
			)
		}
	}

	slices.SortFunc(ordered, func(
		left PriorityCandidate[K, M],
		right PriorityCandidate[K, M],
	) int {
		return comparePriority(left.Priority, right.Priority)
	})

	return ordered, nil
}

func comparePriority(
	left id.MemberID,
	right id.MemberID,
) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func isAvailable(
	ctx context.Context,
	candidate availability.Candidate,
) bool {
	if !candidate.Present {
		return false
	}
	if candidate.Source == nil {
		return true
	}
	return candidate.Source.HasResources(ctx)
}
