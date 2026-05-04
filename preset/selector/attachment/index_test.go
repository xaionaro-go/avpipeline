package attachment_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
)

func routeExistsFromRegistry(
	registry *route.Registry,
) attachment.RouteExistsFunc {
	return func(
		ctx context.Context,
		routeID id.RouteID,
	) bool {
		_, ok := registry.Load(ctx, routeID)
		return ok
	}
}

func TestIndexRejectsUnknownRouteWhenRouteExistenceIsProvided(t *testing.T) {
	ctx := context.Background()
	routes := route.NewRegistry()
	index := attachment.NewIndex(routeExistsFromRegistry(routes))

	err := index.Attach(ctx, id.RouteID("missing"), id.MemberID(1))
	require.True(t, errors.Is(err, route.ErrUnknownRouteID), "error: %v", err)

	require.NoError(t, routes.Add(ctx, route.State{ID: id.RouteID("all")}))
	require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(1)))

	require.Equal(t, []id.MemberID{id.MemberID(1)}, index.MembersForRoute(ctx, id.RouteID("all")))
}

func TestIndexEmptyLookupsReturnNil(t *testing.T) {
	ctx := context.Background()
	index := attachment.NewIndex()

	require.Nil(t, index.MembersForRoute(ctx, id.RouteID("missing")))
	require.Nil(t, index.RoutesForMember(ctx, id.MemberID(1)))

	sibling, ok := index.FirstSibling(ctx, id.RouteID("missing"), id.MemberID(1), nil)
	require.False(t, ok)
	require.Zero(t, sibling)
}

func TestIndexAttachesAndDetachesRouteMemberPairs(t *testing.T) {
	ctx := context.Background()
	routes := route.NewRegistry()
	require.NoError(t, routes.Add(ctx, route.State{ID: id.RouteID("audio")}))
	require.NoError(t, routes.Add(ctx, route.State{ID: id.RouteID("video")}))
	index := attachment.NewIndex(routeExistsFromRegistry(routes))

	require.NoError(t, index.Attach(ctx, id.RouteID("audio"), id.MemberID(2)))
	require.NoError(t, index.Attach(ctx, id.RouteID("audio"), id.MemberID(1)))
	require.NoError(t, index.Attach(ctx, id.RouteID("video"), id.MemberID(1)))

	require.Equal(
		t,
		[]id.MemberID{id.MemberID(1), id.MemberID(2)},
		index.MembersForRoute(ctx, id.RouteID("audio")),
	)
	require.Equal(
		t,
		[]id.RouteID{id.RouteID("audio"), id.RouteID("video")},
		index.RoutesForMember(ctx, id.MemberID(1)),
	)

	index.Detach(ctx, id.RouteID("audio"), id.MemberID(1))

	require.Equal(t, []id.MemberID{id.MemberID(2)}, index.MembersForRoute(ctx, id.RouteID("audio")))
	require.Equal(t, []id.RouteID{id.RouteID("video")}, index.RoutesForMember(ctx, id.MemberID(1)))
}

func TestIndexDetachLastPairCleansRouteAndMemberLookups(t *testing.T) {
	ctx := context.Background()
	index := attachment.NewIndex()

	require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(1)))

	index.Detach(ctx, id.RouteID("all"), id.MemberID(1))

	require.Nil(t, index.MembersForRoute(ctx, id.RouteID("all")))
	require.Nil(t, index.RoutesForMember(ctx, id.MemberID(1)))
}

func TestIndexReturnsDeterministicRouteAndMemberOrder(t *testing.T) {
	ctx := context.Background()
	index := attachment.NewIndex()

	require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(3)))
	require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(1)))
	require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(2)))
	require.NoError(t, index.Attach(ctx, id.RouteID("video"), id.MemberID(9)))
	require.NoError(t, index.Attach(ctx, id.RouteID("audio"), id.MemberID(9)))
	require.NoError(t, index.Attach(ctx, id.RouteID("metadata"), id.MemberID(9)))

	require.Equal(t, []id.MemberID{
		id.MemberID(1),
		id.MemberID(2),
		id.MemberID(3),
	}, index.MembersForRoute(ctx, id.RouteID("all")))
	require.Equal(t, []id.RouteID{
		id.RouteID("audio"),
		id.RouteID("metadata"),
		id.RouteID("video"),
	}, index.RoutesForMember(ctx, id.MemberID(9)))
}

func TestFirstSiblingIsScopedToSameRoute(t *testing.T) {
	ctx := context.Background()
	index := attachment.NewIndex()

	require.NoError(t, index.Attach(ctx, id.RouteID("audio"), id.MemberID(1)))
	require.NoError(t, index.Attach(ctx, id.RouteID("video"), id.MemberID(2)))

	_, ok := index.FirstSibling(ctx, id.RouteID("audio"), id.MemberID(1), func(id.MemberID) bool {
		return true
	})
	require.False(t, ok)

	require.NoError(t, index.Attach(ctx, id.RouteID("audio"), id.MemberID(3)))

	sibling, ok := index.FirstSibling(ctx, id.RouteID("audio"), id.MemberID(1), func(candidate id.MemberID) bool {
		return candidate == id.MemberID(3)
	})
	require.True(t, ok)
	require.Equal(t, id.MemberID(3), sibling)
}

func TestFirstSiblingSkipsCandidatesRejectedByAlive(t *testing.T) {
	ctx := context.Background()
	index := attachment.NewIndex()

	require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(3)))
	require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(1)))
	require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(2)))

	var checked []id.MemberID
	sibling, ok := index.FirstSibling(ctx, id.RouteID("all"), id.MemberID(1), func(candidate id.MemberID) bool {
		checked = append(checked, candidate)
		return candidate == id.MemberID(3)
	})

	require.True(t, ok)
	require.Equal(t, id.MemberID(3), sibling)
	require.Equal(t, []id.MemberID{
		id.MemberID(2),
		id.MemberID(3),
	}, checked)
}

func TestFirstSiblingUsesSnapshotAndAllowsIndexCallsInAliveCallback(t *testing.T) {
	ctx := context.Background()
	index := attachment.NewIndex()

	require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(1)))
	require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(2)))

	sibling, ok := index.FirstSibling(ctx, id.RouteID("all"), id.MemberID(1), func(candidate id.MemberID) bool {
		require.NoError(t, index.Attach(ctx, id.RouteID("all"), id.MemberID(3)))
		index.Detach(ctx, id.RouteID("all"), candidate)
		return candidate == id.MemberID(2)
	})
	require.True(t, ok)
	require.Equal(t, id.MemberID(2), sibling)

	require.Equal(
		t,
		[]id.MemberID{id.MemberID(1), id.MemberID(3)},
		index.MembersForRoute(ctx, id.RouteID("all")),
	)
}
