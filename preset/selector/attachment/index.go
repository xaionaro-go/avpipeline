package attachment

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
)

type RouteExistsFunc func(ctx context.Context, routeID id.RouteID) bool

type Index struct {
	lock           sync.Mutex
	routeExists    RouteExistsFunc
	routeToMembers map[id.RouteID]map[id.MemberID]struct{}
	memberToRoutes map[id.MemberID]map[id.RouteID]struct{}
}

func NewIndex(
	routeExists ...RouteExistsFunc,
) *Index {
	var exists RouteExistsFunc
	if len(routeExists) > 0 {
		exists = routeExists[0]
	}

	return &Index{
		routeExists:    exists,
		routeToMembers: map[id.RouteID]map[id.MemberID]struct{}{},
		memberToRoutes: map[id.MemberID]map[id.RouteID]struct{}{},
	}
}

func (i *Index) Attach(
	ctx context.Context,
	routeID id.RouteID,
	memberID id.MemberID,
) error {
	if i.routeExists != nil && !i.routeExists(ctx, routeID) {
		return fmt.Errorf("%w: route %q", route.ErrUnknownRouteID, routeID)
	}

	i.lock.Lock()
	defer i.lock.Unlock()

	memberSet := i.routeToMembers[routeID]
	if memberSet == nil {
		memberSet = map[id.MemberID]struct{}{}
		i.routeToMembers[routeID] = memberSet
	}
	memberSet[memberID] = struct{}{}

	routeSet := i.memberToRoutes[memberID]
	if routeSet == nil {
		routeSet = map[id.RouteID]struct{}{}
		i.memberToRoutes[memberID] = routeSet
	}
	routeSet[routeID] = struct{}{}

	return nil
}

func (i *Index) Detach(
	_ context.Context,
	routeID id.RouteID,
	memberID id.MemberID,
) {
	i.lock.Lock()
	defer i.lock.Unlock()

	if memberSet := i.routeToMembers[routeID]; memberSet != nil {
		delete(memberSet, memberID)
		if len(memberSet) == 0 {
			delete(i.routeToMembers, routeID)
		}
	}
	if routeSet := i.memberToRoutes[memberID]; routeSet != nil {
		delete(routeSet, routeID)
		if len(routeSet) == 0 {
			delete(i.memberToRoutes, memberID)
		}
	}
}

func (i *Index) RoutesForMember(
	_ context.Context,
	memberID id.MemberID,
) []id.RouteID {
	i.lock.Lock()
	defer i.lock.Unlock()

	routeSet := i.memberToRoutes[memberID]
	if len(routeSet) == 0 {
		return nil
	}

	routes := make([]id.RouteID, 0, len(routeSet))
	for routeID := range routeSet {
		routes = append(routes, routeID)
	}
	slices.SortFunc(routes, compareRouteID)

	return routes
}

func (i *Index) MembersForRoute(
	_ context.Context,
	routeID id.RouteID,
) []id.MemberID {
	i.lock.Lock()
	defer i.lock.Unlock()

	memberSet := i.routeToMembers[routeID]
	if len(memberSet) == 0 {
		return nil
	}

	members := make([]id.MemberID, 0, len(memberSet))
	for memberID := range memberSet {
		members = append(members, memberID)
	}
	slices.SortFunc(members, compareMemberID)

	return members
}

func (i *Index) FirstSibling(
	ctx context.Context,
	routeID id.RouteID,
	dead id.MemberID,
	alive func(id.MemberID) bool,
) (id.MemberID, bool) {
	members := i.MembersForRoute(ctx, routeID)
	for _, memberID := range members {
		if memberID == dead {
			continue
		}
		if alive != nil && !alive(memberID) {
			continue
		}
		return memberID, true
	}

	return 0, false
}

func compareMemberID(
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
