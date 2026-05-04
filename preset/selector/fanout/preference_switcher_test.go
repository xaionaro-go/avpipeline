package fanout_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/fanout"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

func TestSwitchPreferredAggregatesOnlyWhenEveryRouteAlreadyPreferred(t *testing.T) {
	ctx := context.Background()
	fixture := newPreferenceFixture(t, ctx)
	audio := fixture.putMember(t, ctx, id.MemberID(1), "audio")
	video := fixture.putMember(t, ctx, id.MemberID(2), "video")
	fixture.addRoute(t, ctx, id.RouteID("audio"), audio.ID, audio.ID)
	fixture.addRoute(t, ctx, id.RouteID("video"), video.ID, video.ID)
	fixture.attach(t, ctx, id.RouteID("audio"), audio.ID)
	fixture.attach(t, ctx, id.RouteID("video"), video.ID)

	err := fixture.switcher.SwitchPreferred(ctx, []fanout.RoutePlan[string]{
		{RouteID: id.RouteID("audio"), StorageKey: "audio"},
		{RouteID: id.RouteID("video"), StorageKey: "video"},
	}, fixture.routes, fixture.members, fixture.attachments)

	var alreadyPreferred fanout.ErrAllAlreadyPreferred
	require.ErrorAs(t, err, &alreadyPreferred)
	require.ErrorIs(t, err, selectorerr.ErrAlreadyPreferred)
	require.Equal(t, []id.RouteID{id.RouteID("audio"), id.RouteID("video")}, alreadyPreferred.RouteIDs)
	require.Contains(t, alreadyPreferred.Error(), "already preferred")
}

func TestSwitchPreferredStillSwitchesPartialNonActiveRoutes(t *testing.T) {
	ctx := context.Background()
	fixture := newPreferenceFixture(t, ctx)
	audio := fixture.putMember(t, ctx, id.MemberID(1), "audio")
	oldVideo := fixture.putMember(t, ctx, id.MemberID(2), "old-video")
	newVideo := fixture.putMember(t, ctx, id.MemberID(3), "new-video")
	audioPair := fixture.addRoute(t, ctx, id.RouteID("audio"), audio.ID, audio.ID)
	videoPair := fixture.addRoute(t, ctx, id.RouteID("video"), oldVideo.ID, oldVideo.ID)
	fixture.attach(t, ctx, id.RouteID("audio"), audio.ID)
	fixture.attach(t, ctx, id.RouteID("video"), oldVideo.ID)
	fixture.attach(t, ctx, id.RouteID("video"), newVideo.ID)

	err := fixture.switcher.SwitchPreferred(ctx, []fanout.RoutePlan[string]{
		{RouteID: id.RouteID("audio"), StorageKey: "audio"},
		{RouteID: id.RouteID("video"), StorageKey: "new-video"},
	}, fixture.routes, fixture.members, fixture.attachments)
	require.NoError(t, err)

	require.Equal(t, audio.ID, audioPair.Current(ctx))
	require.Equal(t, audio.ID, audioPair.SyncerCurrent(ctx))
	require.Equal(t, newVideo.ID, videoPair.Current(ctx))
	require.Equal(t, newVideo.ID, videoPair.SyncerCurrent(ctx))
}

func TestSwitchPreferredDetectsSwitchInProgressBeforeMutation(t *testing.T) {
	ctx := context.Background()
	fixture := newPreferenceFixture(t, ctx)
	current := fixture.putMember(t, ctx, id.MemberID(1), "current")
	syncing := fixture.putMember(t, ctx, id.MemberID(2), "syncing")
	target := fixture.putMember(t, ctx, id.MemberID(3), "target")
	pair := fixture.addRoute(t, ctx, id.RouteID("all"), current.ID, syncing.ID)
	fixture.attach(t, ctx, id.RouteID("all"), current.ID)
	fixture.attach(t, ctx, id.RouteID("all"), target.ID)

	err := fixture.switcher.SwitchPreferred(ctx, []fanout.RoutePlan[string]{
		{RouteID: id.RouteID("all"), StorageKey: "target"},
	}, fixture.routes, fixture.members, fixture.attachments)

	var inProgress fanout.ErrSwitchAlreadyInProgress
	require.ErrorAs(t, err, &inProgress)
	require.Equal(t, id.RouteID("all"), inProgress.RouteID)
	require.Equal(t, current.ID, inProgress.SwitchMemberID)
	require.Equal(t, syncing.ID, inProgress.SyncerMemberID)
	require.Contains(t, inProgress.Error(), "switch already in progress")
	require.Equal(t, current.ID, pair.Current(ctx))
	require.Equal(t, syncing.ID, pair.SyncerCurrent(ctx))
}

func TestSwitchPreferredKeepsRouteStateIndependentForSplitAV(t *testing.T) {
	ctx := context.Background()
	fixture := newPreferenceFixture(t, ctx)
	audio := fixture.putMember(t, ctx, id.MemberID(11), "audio")
	video := fixture.putMember(t, ctx, id.MemberID(12), "video")
	audioPair := fixture.addRoute(t, ctx, id.RouteID("audio"), id.NoMemberID, id.NoMemberID)
	videoPair := fixture.addRoute(t, ctx, id.RouteID("video"), id.NoMemberID, id.NoMemberID)
	fixture.attach(t, ctx, id.RouteID("audio"), audio.ID)
	fixture.attach(t, ctx, id.RouteID("video"), video.ID)

	err := fixture.switcher.SwitchPreferred(ctx, []fanout.RoutePlan[string]{
		{RouteID: id.RouteID("audio"), StorageKey: "audio"},
		{RouteID: id.RouteID("video"), StorageKey: "video"},
	}, fixture.routes, fixture.members, fixture.attachments)
	require.NoError(t, err)

	require.Equal(t, audio.ID, audioPair.Current(ctx))
	require.Equal(t, video.ID, videoPair.Current(ctx))
	require.NotEqual(t, audioPair.Current(ctx), videoPair.Current(ctx))
}

func TestSwitchPreferredRejectsInvalidPlansAndUnknownRoutesBeforeMutation(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name        string
		plans       []fanout.RoutePlan[string]
		expectedErr error
	}{
		{
			name:        "empty plans",
			plans:       nil,
			expectedErr: selectorerr.ErrInvalidRoutePlan,
		},
		{
			name: "empty route id",
			plans: []fanout.RoutePlan[string]{
				{RouteID: "", StorageKey: "target"},
			},
			expectedErr: selectorerr.ErrInvalidRoutePlan,
		},
		{
			name: "duplicate route id",
			plans: []fanout.RoutePlan[string]{
				{RouteID: id.RouteID("all"), StorageKey: "target"},
				{RouteID: id.RouteID("all"), StorageKey: "target"},
			},
			expectedErr: selectorerr.ErrInvalidRoutePlan,
		},
		{
			name: "unknown route id",
			plans: []fanout.RoutePlan[string]{
				{RouteID: id.RouteID("missing"), StorageKey: "target"},
			},
			expectedErr: selectorerr.ErrRouteNotFound,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newPreferenceFixture(t, ctx)
			target := fixture.putMember(t, ctx, id.MemberID(3), "target")
			pair := fixture.addRoute(t, ctx, id.RouteID("all"), id.MemberID(1), id.MemberID(1))
			fixture.attach(t, ctx, id.RouteID("all"), target.ID)

			err := fixture.switcher.SwitchPreferred(ctx, testCase.plans, fixture.routes, fixture.members, fixture.attachments)
			require.ErrorIs(t, err, testCase.expectedErr)
			require.Equal(t, id.MemberID(1), pair.Current(ctx))
			require.Equal(t, id.MemberID(1), pair.SyncerCurrent(ctx))
		})
	}
}

func TestSwitchPreferredRejectsMissingMemberAndUnattachedPlanWithSafeKey(t *testing.T) {
	ctx := context.Background()
	rawKey := "secret://target"

	t.Run("redacted missing member", func(t *testing.T) {
		fixture := newPreferenceFixture(t, ctx)
		pair := fixture.addRoute(t, ctx, id.RouteID("all"), id.MemberID(1), id.MemberID(1))

		err := fixture.switcher.SwitchPreferred(ctx, []fanout.RoutePlan[string]{
			{RouteID: id.RouteID("all"), StorageKey: rawKey},
		}, fixture.routes, fixture.members, fixture.attachments)
		require.ErrorIs(t, err, selectorerr.ErrMemberNotFound)
		require.Contains(t, err.Error(), "<redacted>")
		require.False(t, strings.Contains(err.Error(), rawKey))
		require.Equal(t, id.MemberID(1), pair.Current(ctx))
	})

	t.Run("custom format unattached member", func(t *testing.T) {
		fixture := newPreferenceFixture(t, ctx)
		fixture.switcher = fanout.NewPreferenceSwitcher[string, testMember](
			safekey.FormatterFunc[string](func(context.Context, string) string {
				return "safe-target"
			}),
		)
		target := fixture.putMember(t, ctx, id.MemberID(3), rawKey)
		pair := fixture.addRoute(t, ctx, id.RouteID("all"), id.MemberID(1), id.MemberID(1))
		require.NotContains(t, fixture.attachments.MembersForRoute(ctx, id.RouteID("all")), target.ID)

		err := fixture.switcher.SwitchPreferred(ctx, []fanout.RoutePlan[string]{
			{RouteID: id.RouteID("all"), StorageKey: rawKey},
		}, fixture.routes, fixture.members, fixture.attachments)
		require.ErrorIs(t, err, selectorerr.ErrInvalidRoutePlan)
		require.Contains(t, err.Error(), "safe-target")
		require.False(t, strings.Contains(err.Error(), rawKey))
		require.Equal(t, id.MemberID(1), pair.Current(ctx))
	})
}

type preferenceFixture struct {
	routes      *route.Registry
	members     *member.Registry[string, testMember]
	attachments *attachment.Index
	switcher    *fanout.PreferenceSwitcher[string, testMember]
}

func newPreferenceFixture(
	t *testing.T,
	ctx context.Context,
) *preferenceFixture {
	t.Helper()

	routes := route.NewRegistry()
	return &preferenceFixture{
		routes:      routes,
		members:     member.NewRegistry[string, testMember](),
		attachments: attachment.NewIndex(routeExistsFromRegistry(routes)),
		switcher:    fanout.NewPreferenceSwitcher[string, testMember](nil),
	}
}

func (f *preferenceFixture) putMember(
	t *testing.T,
	ctx context.Context,
	memberID id.MemberID,
	storageKey string,
) member.Entry[string, testMember] {
	t.Helper()

	entry, err := f.members.Put(ctx, memberID, storageKey, testMember{name: storageKey})
	require.NoError(t, err)
	return entry
}

func (f *preferenceFixture) addRoute(
	t *testing.T,
	ctx context.Context,
	routeID id.RouteID,
	current id.MemberID,
	syncer id.MemberID,
) *switchpair.Pair {
	t.Helper()

	pair := mustPair(t, ctx, current)
	pair.Syncer().CurrentValue.Store(int32(syncer))
	require.NoError(t, f.routes.Add(ctx, route.State{ID: routeID, Pair: pair}))
	return pair
}

func (f *preferenceFixture) attach(
	t *testing.T,
	ctx context.Context,
	routeID id.RouteID,
	memberID id.MemberID,
) {
	t.Helper()

	require.NoError(t, f.attachments.Attach(ctx, routeID, memberID))
}

var _ = errors.Is
