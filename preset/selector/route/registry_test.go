package route_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
)

func TestRegistryRejectsDuplicateRouteID(t *testing.T) {
	ctx := context.Background()
	registry := route.NewRegistry()
	state := route.State{ID: id.RouteID("all")}

	require.NoError(t, registry.Add(ctx, state))

	err := registry.Add(ctx, state)
	require.True(t, errors.Is(err, route.ErrDuplicateRouteID), "error: %v", err)

	loaded, ok := registry.Load(ctx, id.RouteID("all"))
	require.True(t, ok)
	require.Equal(t, state.ID, loaded.ID)
}

func TestRegistryLoadReportsMissingRoute(t *testing.T) {
	ctx := context.Background()
	registry := route.NewRegistry()

	loaded, ok := registry.Load(ctx, id.RouteID("missing"))
	require.False(t, ok)
	require.Zero(t, loaded)
}

func TestRegistryRangeUsesSnapshotAndAllowsRegistryCallsInCallback(t *testing.T) {
	ctx := context.Background()
	registry := route.NewRegistry()

	require.NoError(t, registry.Add(ctx, route.State{ID: id.RouteID("audio")}))
	require.NoError(t, registry.Add(ctx, route.State{ID: id.RouteID("video")}))

	seen := map[id.RouteID]bool{}
	registry.Range(ctx, func(state route.State) bool {
		seen[state.ID] = true
		if len(seen) == 1 {
			require.NoError(t, registry.Add(ctx, route.State{ID: id.RouteID("metadata")}))
		}
		return true
	})

	require.Equal(t, map[id.RouteID]bool{
		id.RouteID("audio"): true,
		id.RouteID("video"): true,
	}, seen)

	_, ok := registry.Load(ctx, id.RouteID("metadata"))
	require.True(t, ok)
}

func TestRegistryRangeStopsEarlyInRouteIDOrder(t *testing.T) {
	ctx := context.Background()
	registry := route.NewRegistry()

	require.NoError(t, registry.Add(ctx, route.State{ID: id.RouteID("video")}))
	require.NoError(t, registry.Add(ctx, route.State{ID: id.RouteID("metadata")}))
	require.NoError(t, registry.Add(ctx, route.State{ID: id.RouteID("audio")}))

	var seen []id.RouteID
	registry.Range(ctx, func(state route.State) bool {
		seen = append(seen, state.ID)
		return state.ID != id.RouteID("metadata")
	})

	require.Equal(t, []id.RouteID{
		id.RouteID("audio"),
		id.RouteID("metadata"),
	}, seen)
}
