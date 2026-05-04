package orphanretry

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

// Tracker stores orphaned-route retry state and evaluates retry ticks.
type Tracker[K comparable] struct {
	lock     sync.Mutex
	policy   Policy[K]
	now      NowFunc
	recreate RecreateFunc[K]
	nextEra  uint64
	states   map[id.RouteID]trackedState[K]
}

// NewTracker returns a tracker with explicit policy, clock, and recreate hook.
func NewTracker[K comparable](
	policy Policy[K],
	now NowFunc,
	recreate RecreateFunc[K],
) (*Tracker[K], error) {
	var errs []error
	if policy == nil {
		errs = append(errs, selectorerr.InvalidConfig("Policy", nil))
	}
	if now == nil {
		errs = append(errs, selectorerr.InvalidConfig("Now", nil))
	}
	if recreate == nil {
		errs = append(errs, selectorerr.InvalidConfig("Recreate", nil))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return &Tracker[K]{
		policy:   policy,
		now:      now,
		recreate: recreate,
		states:   map[id.RouteID]trackedState[K]{},
	}, nil
}

// RecordDemotion records the storage key needed to recreate a demoted route.
func (t *Tracker[K]) RecordDemotion(
	_ context.Context,
	routeID id.RouteID,
	storageKey K,
) {
	now := t.now()
	state := trackedState[K]{
		State: State[K]{
			RouteID:        routeID,
			StorageKey:     storageKey,
			FirstAttemptAt: now,
			NextAttemptAt:  now,
		},
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	t.nextEra++
	state.era = t.nextEra
	t.states[routeID] = state
}

// MarkRecovered removes retry state for a route that recovered.
func (t *Tracker[K]) MarkRecovered(
	_ context.Context,
	routeID id.RouteID,
) {
	t.lock.Lock()
	defer t.lock.Unlock()

	delete(t.states, routeID)
}

// Tick evaluates all recorded orphan routes once.
func (t *Tracker[K]) Tick(
	ctx context.Context,
	current func(context.Context, id.RouteID) (id.MemberID, bool),
) error {
	if current == nil {
		return selectorerr.InvalidConfig("Current", nil)
	}

	now := t.now()
	var errs []error
	for _, state := range t.snapshotStates() {
		if err := t.tickState(ctx, now, current, state); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

type trackedState[K comparable] struct {
	State[K]
	era uint64
}

func (t *Tracker[K]) tickState(
	ctx context.Context,
	now time.Time,
	current func(context.Context, id.RouteID) (id.MemberID, bool),
	state trackedState[K],
) error {
	currentValue, ok := current(ctx, state.RouteID)
	if !ok {
		t.deleteIfStillSame(state)
		return nil
	}
	if currentValue != id.NoMemberID {
		t.deleteIfStillSame(state)
		return nil
	}

	decision := t.policy.Next(now, state.State)
	if decision.Retire {
		t.deleteIfStillSame(state)
		return nil
	}
	if !decision.Attempt {
		return nil
	}

	attemptState, ok := t.recordAttemptIfStillSame(state, now, decision)
	if !ok {
		return nil
	}

	return t.recreate(ctx, attemptState.RouteID, attemptState.StorageKey)
}

func (t *Tracker[K]) snapshotStates() []trackedState[K] {
	t.lock.Lock()
	defer t.lock.Unlock()

	states := make([]trackedState[K], 0, len(t.states))
	for _, state := range t.states {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].RouteID < states[j].RouteID
	})

	return states
}

func (t *Tracker[K]) deleteIfStillSame(
	expected trackedState[K],
) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if state, ok := t.states[expected.RouteID]; ok && sameTrackedStateIdentity(state, expected) {
		delete(t.states, expected.RouteID)
	}
}

func (t *Tracker[K]) recordAttemptIfStillSame(
	expected trackedState[K],
	now time.Time,
	decision Decision,
) (trackedState[K], bool) {
	t.lock.Lock()
	defer t.lock.Unlock()

	state, ok := t.states[expected.RouteID]
	if !ok {
		return trackedState[K]{}, false
	}
	if !sameTrackedStateVersion(state, expected) {
		return trackedState[K]{}, false
	}

	if state.Attempts != ^uint64(0) {
		state.Attempts++
	}
	state.LastAttemptAt = now
	state.NextAttemptAt = decision.NextAt
	t.states[expected.RouteID] = state

	return state, true
}

func sameTrackedStateIdentity[K comparable](
	state trackedState[K],
	expected trackedState[K],
) bool {
	return state.era == expected.era &&
		state.RouteID == expected.RouteID &&
		state.StorageKey == expected.StorageKey &&
		state.FirstAttemptAt.Equal(expected.FirstAttemptAt)
}

func sameTrackedStateVersion[K comparable](
	state trackedState[K],
	expected trackedState[K],
) bool {
	return sameTrackedStateIdentity(state, expected) &&
		state.Attempts == expected.Attempts &&
		state.LastAttemptAt.Equal(expected.LastAttemptAt) &&
		state.NextAttemptAt.Equal(expected.NextAttemptAt)
}
