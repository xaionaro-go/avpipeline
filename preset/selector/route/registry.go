package route

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type Registry struct {
	lock   sync.Mutex
	routes map[id.RouteID]State
}

func NewRegistry() *Registry {
	return &Registry{
		routes: map[id.RouteID]State{},
	}
}

func (r *Registry) Add(
	_ context.Context,
	state State,
) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	if _, ok := r.routes[state.ID]; ok {
		return fmt.Errorf("%w: route %q", ErrDuplicateRouteID, state.ID)
	}
	r.routes[state.ID] = state

	return nil
}

func (r *Registry) Load(
	_ context.Context,
	routeID id.RouteID,
) (State, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	state, ok := r.routes[routeID]
	return state, ok
}

func (r *Registry) Range(
	ctx context.Context,
	fn func(State) bool,
) {
	snapshot := r.snapshot(ctx)
	for _, state := range snapshot {
		if !fn(state) {
			return
		}
	}
}

func (r *Registry) snapshot(
	_ context.Context,
) []State {
	r.lock.Lock()
	defer r.lock.Unlock()

	snapshot := make([]State, 0, len(r.routes))
	for _, state := range r.routes {
		snapshot = append(snapshot, state)
	}
	slices.SortFunc(snapshot, func(
		left State,
		right State,
	) int {
		return compareRouteID(left.ID, right.ID)
	})

	return snapshot
}

func compareRouteID(
	left id.RouteID,
	right id.RouteID,
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
