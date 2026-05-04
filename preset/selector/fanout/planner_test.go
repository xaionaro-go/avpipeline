package fanout_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/fanout"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

func TestModePoliciesRepresentAllTopologyModes(t *testing.T) {
	ctx := context.Background()
	allRoute := id.RouteID("all")
	existing := []fanout.ExistingMember[string]{
		{ID: id.MemberID(7), StorageKey: "existing"},
	}

	testCases := []struct {
		name                   string
		mode                   fanout.Mode
		createWithExisting     fanout.CreationAction
		sameKeyWithExisting    fanout.CreationAction
		allowsDifferentOutputs bool
		needsSplitRoutePlanner bool
	}{
		{
			name:                   "forbid",
			mode:                   fanout.ModeForbid,
			createWithExisting:     fanout.CreationActionReject,
			sameKeyWithExisting:    fanout.CreationActionReject,
			allowsDifferentOutputs: false,
		},
		{
			name:                   "same output same tracks",
			mode:                   fanout.ModeSameOutputSameTracks,
			createWithExisting:     fanout.CreationActionReuse,
			sameKeyWithExisting:    fanout.CreationActionReuse,
			allowsDifferentOutputs: false,
		},
		{
			name:                   "same output different tracks",
			mode:                   fanout.ModeSameOutputDifferentTracks,
			createWithExisting:     fanout.CreationActionReuse,
			sameKeyWithExisting:    fanout.CreationActionReuse,
			allowsDifferentOutputs: false,
		},
		{
			name:                   "different outputs same tracks",
			mode:                   fanout.ModeDifferentOutputsSameTracks,
			createWithExisting:     fanout.CreationActionCreate,
			sameKeyWithExisting:    fanout.CreationActionReuse,
			allowsDifferentOutputs: true,
		},
		{
			name:                   "split av",
			mode:                   fanout.ModeDifferentOutputsSameTracksSplitAV,
			createWithExisting:     fanout.CreationActionCreate,
			sameKeyWithExisting:    fanout.CreationActionReuse,
			allowsDifferentOutputs: true,
			needsSplitRoutePlanner: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			creationPlanner := fanout.NewCreationPlanner[string](testCase.mode, allRoute)
			decision, err := creationPlanner.PlanCreate(ctx, "first", nil)
			require.NoError(t, err)
			require.Equal(t, fanout.CreationActionCreate, decision.Action)
			require.Equal(t, id.NoMemberID, decision.ReuseMemberID)
			require.Equal(t, []id.RouteID{allRoute}, decision.RouteIDs)

			decision, err = creationPlanner.PlanCreate(ctx, "new", existing)
			require.NoError(t, err)
			require.Equal(t, testCase.createWithExisting, decision.Action)
			if testCase.createWithExisting == fanout.CreationActionReuse {
				require.Equal(t, existing[0].ID, decision.ReuseMemberID)
			} else {
				require.Equal(t, id.NoMemberID, decision.ReuseMemberID)
			}

			decision, err = creationPlanner.PlanCreate(ctx, "existing", existing)
			require.NoError(t, err)
			require.Equal(t, testCase.sameKeyWithExisting, decision.Action)
			if testCase.sameKeyWithExisting == fanout.CreationActionReuse {
				require.Equal(t, existing[0].ID, decision.ReuseMemberID)
			}

			policy := fanout.NewDifferentOutputPolicy(testCase.mode)
			require.Equal(t, testCase.allowsDifferentOutputs, policy.AllowsDifferentOutputs(ctx))

			routePlanner := fanout.NewRoutePlanner[string](testCase.mode, allRoute)
			plans, err := routePlanner.PlanPreferred(ctx, "preferred")
			if testCase.needsSplitRoutePlanner {
				require.ErrorIs(t, err, selectorerr.ErrInvalidRoutePlan)
				require.Nil(t, plans)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []fanout.RoutePlan[string]{
				{RouteID: allRoute, StorageKey: "preferred"},
			}, plans)
		})
	}
}

func TestSplitRoutePlannerUsesInjectedKeyDecomposition(t *testing.T) {
	ctx := context.Background()
	routePlanner := fanout.NewRoutePlanner[string](
		fanout.ModeDifferentOutputsSameTracksSplitAV,
		"",
		fanout.PreferredRoutePlannerFunc[string](func(
			context.Context,
			string,
		) ([]fanout.RoutePlan[string], error) {
			return []fanout.RoutePlan[string]{
				{RouteID: id.RouteID("audio"), StorageKey: "audio-only"},
				{RouteID: id.RouteID("video"), StorageKey: "video-only"},
			}, nil
		}),
	)

	plans, err := routePlanner.PlanPreferred(ctx, "combined")
	require.NoError(t, err)
	require.Equal(t, []fanout.RoutePlan[string]{
		{RouteID: id.RouteID("audio"), StorageKey: "audio-only"},
		{RouteID: id.RouteID("video"), StorageKey: "video-only"},
	}, plans)
}

func TestRoutePlannerRejectsInvalidPlans(t *testing.T) {
	ctx := context.Background()

	routePlanner := fanout.NewRoutePlanner[string](fanout.ModeDifferentOutputsSameTracks, "")
	plans, err := routePlanner.PlanPreferred(ctx, "preferred")
	require.ErrorIs(t, err, selectorerr.ErrInvalidRoutePlan)
	require.Nil(t, plans)

	expectedErr := errors.New("split failed")
	routePlanner = fanout.NewRoutePlanner[string](
		fanout.ModeDifferentOutputsSameTracksSplitAV,
		"",
		fanout.PreferredRoutePlannerFunc[string](func(
			context.Context,
			string,
		) ([]fanout.RoutePlan[string], error) {
			return nil, expectedErr
		}),
	)
	plans, err = routePlanner.PlanPreferred(ctx, "preferred")
	require.ErrorIs(t, err, expectedErr)
	require.ErrorIs(t, err, selectorerr.ErrInvalidRoutePlan)
	require.Nil(t, plans)

	routePlanner = fanout.NewRoutePlanner[string](
		fanout.ModeDifferentOutputsSameTracksSplitAV,
		"",
		fanout.PreferredRoutePlannerFunc[string](func(
			context.Context,
			string,
		) ([]fanout.RoutePlan[string], error) {
			return []fanout.RoutePlan[string]{
				{RouteID: id.RouteID("audio"), StorageKey: "audio-only"},
				{RouteID: id.RouteID("audio"), StorageKey: "duplicate"},
			}, nil
		}),
	)
	plans, err = routePlanner.PlanPreferred(ctx, "preferred")
	require.ErrorIs(t, err, selectorerr.ErrInvalidRoutePlan)
	require.Nil(t, plans)
}

func TestModeStringCoversKnownAndUnknownModes(t *testing.T) {
	require.Equal(t, "forbid", fanout.ModeForbid.String())
	require.Equal(t, "same-output-same-tracks", fanout.ModeSameOutputSameTracks.String())
	require.Equal(t, "same-output-different-tracks", fanout.ModeSameOutputDifferentTracks.String())
	require.Equal(t, "different-outputs-same-tracks", fanout.ModeDifferentOutputsSameTracks.String())
	require.Equal(t, "different-outputs-same-tracks-split-av", fanout.ModeDifferentOutputsSameTracksSplitAV.String())
	require.Equal(t, "unknown", fanout.Mode(255).String())
}

func TestCreationPlannerUnknownModeRejects(t *testing.T) {
	ctx := context.Background()
	creationPlanner := fanout.NewCreationPlanner[string](fanout.Mode(255), id.RouteID("all"))

	decision, err := creationPlanner.PlanCreate(ctx, "requested", nil)
	require.NoError(t, err)
	require.Equal(t, fanout.CreationActionReject, decision.Action)
	require.Equal(t, id.NoMemberID, decision.ReuseMemberID)
}
