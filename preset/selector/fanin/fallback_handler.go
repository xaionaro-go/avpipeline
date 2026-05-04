package fanin

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

type Failure[K comparable] struct {
	RouteID    id.RouteID
	MemberID   id.MemberID
	StorageKey K
	Current    id.MemberID
	Next       id.MemberID
	Cause      error
}

type FallbackDecision struct {
	IgnoreFailure bool
	SwitchTo      id.MemberID
	UseRecreate   bool
}

type FallbackHandler[K comparable, M Member] struct {
	PriorityPolicy *PriorityPolicy[K, M]
}

func (h *FallbackHandler[K, M]) HandleFailure(
	ctx context.Context,
	failure Failure[K],
	candidates []PriorityCandidate[K, M],
) (FallbackDecision, error) {
	if failure.MemberID != failure.Current && failure.MemberID != failure.Next {
		return FallbackDecision{
			IgnoreFailure: true,
		}, nil
	}
	if _, ok := findCandidate(candidates, failure.MemberID); !ok {
		return FallbackDecision{}, selectorerr.MemberNotFound(failure.MemberID)
	}

	next, ok, err := h.priorityPolicy().FirstAvailableAfter(ctx, candidates, failure.MemberID)
	if err != nil {
		return FallbackDecision{}, err
	}
	if !ok {
		return FallbackDecision{
			UseRecreate: true,
		}, nil
	}

	return FallbackDecision{
		SwitchTo: next,
	}, nil
}

func (h *FallbackHandler[K, M]) priorityPolicy() *PriorityPolicy[K, M] {
	if h.PriorityPolicy != nil {
		return h.PriorityPolicy
	}
	return &PriorityPolicy[K, M]{}
}

func findCandidate[K comparable, M Member](
	candidates []PriorityCandidate[K, M],
	priority id.MemberID,
) (PriorityCandidate[K, M], bool) {
	for _, candidate := range candidates {
		if candidate.Priority == priority {
			return candidate, true
		}
	}

	return PriorityCandidate[K, M]{}, false
}
