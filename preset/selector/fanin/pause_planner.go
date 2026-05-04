package fanin

import (
	"context"
	"slices"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type PauseState struct {
	Current         id.MemberID
	PreviousPending id.MemberID
	Target          id.MemberID
	UnpausedCount   int
	Priorities      []id.MemberID
	Paused          map[id.MemberID]bool
}

type PausePlan struct {
	UnpauseBeforeSwitch  []id.MemberID
	PauseAfterSwitch     []id.MemberID
	PausePreviousPending []id.MemberID
}

type PausePlanner[K comparable, M Member] struct{}

func (p *PausePlanner[K, M]) PlanSwitch(
	_ context.Context,
	state PauseState,
) (PausePlan, error) {
	var plan PausePlan
	for _, priority := range sortedPriorities(state.Priorities) {
		if priority > state.Target {
			continue
		}
		if !state.isPaused(priority) {
			continue
		}
		plan.UnpauseBeforeSwitch = append(plan.UnpauseBeforeSwitch, priority)
	}

	for _, priority := range sortedPriorities(state.Priorities) {
		if priority <= state.Target {
			continue
		}
		if priority > state.Current {
			continue
		}
		if state.isPaused(priority) {
			continue
		}
		plan.PauseAfterSwitch = append(plan.PauseAfterSwitch, priority)
	}

	if shouldPausePreviousPending(state) {
		plan.PausePreviousPending = append(plan.PausePreviousPending, state.PreviousPending)
	}

	return plan, nil
}

func (p *PausePlanner[K, M]) PlanPause(
	_ context.Context,
	state PauseState,
	target id.MemberID,
) (PausePlan, error) {
	if state.isPaused(target) {
		return PausePlan{}, nil
	}
	if state.UnpausedCount <= 1 {
		return PausePlan{}, ErrCannotPauseSoleActiveMember
	}

	return PausePlan{
		PauseAfterSwitch: []id.MemberID{target},
	}, nil
}

func (p *PausePlanner[K, M]) PlanUnpause(
	_ context.Context,
	state PauseState,
	target id.MemberID,
) (PausePlan, error) {
	if !state.isPaused(target) {
		return PausePlan{}, nil
	}

	return PausePlan{
		UnpauseBeforeSwitch: []id.MemberID{target},
	}, nil
}

func shouldPausePreviousPending(
	state PauseState,
) bool {
	if state.PreviousPending == id.NoMemberID {
		return false
	}
	if state.PreviousPending <= state.Current {
		return false
	}
	return state.PreviousPending != state.Target
}

func (s PauseState) isPaused(
	priority id.MemberID,
) bool {
	if s.Paused == nil {
		return false
	}
	return s.Paused[priority]
}

func sortedPriorities(
	priorities []id.MemberID,
) []id.MemberID {
	ret := append([]id.MemberID(nil), priorities...)
	slices.Sort(ret)
	return ret
}
