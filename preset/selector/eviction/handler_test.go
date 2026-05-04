package eviction_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/eviction"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

func TestNewHandlerValidatesRequiredFieldsAndAllowsOptionalHooks(t *testing.T) {
	ctx := context.Background()
	members := member.NewRegistry[string, testMember]()
	routes := route.NewRegistry()
	attachments := attachment.NewIndex()
	recommit := func(context.Context, id.RouteID, string) error {
		return nil
	}

	testCases := []struct {
		name string
		cfg  eviction.Config[string, testMember]
	}{
		{
			name: "members",
			cfg: eviction.Config[string, testMember]{
				Routes:      routes,
				Attachments: attachments,
				Recommit:    recommit,
			},
		},
		{
			name: "routes",
			cfg: eviction.Config[string, testMember]{
				Members:     members,
				Attachments: attachments,
				Recommit:    recommit,
			},
		},
		{
			name: "attachments",
			cfg: eviction.Config[string, testMember]{
				Members:  members,
				Routes:   routes,
				Recommit: recommit,
			},
		},
		{
			name: "recommit",
			cfg: eviction.Config[string, testMember]{
				Members:     members,
				Routes:      routes,
				Attachments: attachments,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handler, err := eviction.NewHandler(testCase.cfg)
			require.Nil(t, handler)
			require.ErrorIs(t, err, selectorerr.ErrInvalidConfig)
			require.Contains(t, err.Error(), testCase.name)
		})
	}

	handler, err := eviction.NewHandler(eviction.Config[string, testMember]{
		Members:     members,
		Routes:      routes,
		Attachments: attachments,
		Recommit:    recommit,
	})
	require.NoError(t, err)
	result, err := handler.Evict(ctx, member.Entry[string, testMember]{
		ID:         id.MemberID(404),
		StorageKey: "missing",
	})
	require.NoError(t, err)
	require.Equal(t, eviction.Result{}, result)
}

func TestEvictMissingOrStaleDeadEntryIsNoOp(t *testing.T) {
	ctx := context.Background()
	fixture := newFixture(t, ctx)
	var recreateCalls int
	fixture.recreate = func(context.Context, id.RouteID, string) error {
		recreateCalls++
		return nil
	}
	handler := fixture.handler(t)

	missing := member.Entry[string, testMember]{
		ID:         id.MemberID(99),
		StorageKey: "missing",
	}
	result, err := handler.Evict(ctx, missing)
	require.NoError(t, err)
	require.Equal(t, eviction.Result{}, result)

	dead := fixture.putMember(t, ctx, id.MemberID(1), "dead")
	stale := dead
	stale.StorageKey = "stale"
	result, err = handler.Evict(ctx, stale)
	require.NoError(t, err)
	require.Equal(t, eviction.Result{}, result)

	_, ok := fixture.members.LoadByID(ctx, dead.ID)
	require.True(t, ok)
	require.Zero(t, recreateCalls)
}

func TestEvictPreservesNewerEntryBoundToSameStorageKey(t *testing.T) {
	ctx := context.Background()
	fixture := newFixture(t, ctx)
	oldEntry := fixture.putMember(t, ctx, id.MemberID(1), "stable")
	require.True(t, fixture.members.UnbindStorageKey(ctx, oldEntry))
	newEntry := fixture.putMember(t, ctx, id.MemberID(2), "stable")
	pair := fixture.addRoute(t, ctx, id.RouteID("video"), oldEntry.ID, oldEntry.ID)
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), oldEntry.ID))
	handler := fixture.handler(t)

	result, err := handler.Evict(ctx, oldEntry)
	require.NoError(t, err)
	require.Equal(t, []id.RouteID{id.RouteID("video")}, result.DemotedRoutes)
	require.Equal(t, id.NoMemberID, pair.Current(ctx))

	_, ok := fixture.members.LoadByID(ctx, oldEntry.ID)
	require.False(t, ok)
	loaded, ok := fixture.members.LoadByStorageKey(ctx, "stable")
	require.True(t, ok)
	require.Equal(t, newEntry.ID, loaded.ID)
}

func TestEvictDemotesOnlyAttachedRoutesPointingAtDeadMember(t *testing.T) {
	ctx := context.Background()
	fixture := newFixture(t, ctx)
	dead := fixture.putMember(t, ctx, id.MemberID(2), "dead")
	live := fixture.putMember(t, ctx, id.MemberID(7), "live")
	videoPair := fixture.addRoute(t, ctx, id.RouteID("video"), dead.ID, dead.ID)
	audioPair := fixture.addRoute(t, ctx, id.RouteID("audio"), live.ID, live.ID)
	unattachedPair := fixture.addRoute(t, ctx, id.RouteID("metadata"), dead.ID, dead.ID)
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), dead.ID))
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("audio"), dead.ID))
	handler := fixture.handler(t)

	result, err := handler.Evict(ctx, dead)
	require.NoError(t, err)

	require.Equal(t, []id.RouteID{id.RouteID("video")}, result.DemotedRoutes)
	require.Equal(t, id.NoMemberID, videoPair.Current(ctx))
	require.Equal(t, id.NoMemberID, videoPair.SyncerCurrent(ctx))
	require.Equal(t, live.ID, audioPair.Current(ctx))
	require.Equal(t, live.ID, audioPair.SyncerCurrent(ctx))
	require.Equal(t, dead.ID, unattachedPair.Current(ctx))
	require.Equal(t, dead.ID, unattachedPair.SyncerCurrent(ctx))
}

func TestEvictReturnsDeterministicResultOrdering(t *testing.T) {
	ctx := context.Background()
	fixture := newFixture(t, ctx)
	dead := fixture.putMember(t, ctx, id.MemberID(3), "dead")
	fixture.retry = &recordingRetryTracker[string]{}
	fixture.recreate = func(context.Context, id.RouteID, string) error {
		return nil
	}
	fixture.addRoute(t, ctx, id.RouteID("video"), dead.ID, dead.ID)
	fixture.addRoute(t, ctx, id.RouteID("audio"), dead.ID, dead.ID)
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), dead.ID))
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("audio"), dead.ID))
	handler := fixture.handler(t)

	result, err := handler.Evict(ctx, dead)
	require.NoError(t, err)

	require.Equal(t, []id.RouteID{id.RouteID("audio"), id.RouteID("video")}, result.DemotedRoutes)
	require.Equal(t, []id.RouteID{id.RouteID("audio"), id.RouteID("video")}, result.RetryRecorded)
	require.Equal(t, []id.RouteID{id.RouteID("audio"), id.RouteID("video")}, result.Recreated)
}

func TestEvictRecommitsSameRouteLiveSibling(t *testing.T) {
	ctx := context.Background()
	fixture := newFixture(t, ctx)
	dead := fixture.putMember(t, ctx, id.MemberID(5), "dead")
	otherRouteSibling := fixture.putMember(t, ctx, id.MemberID(1), "other-route")
	sameRouteSibling := fixture.putMember(t, ctx, id.MemberID(8), "same-route")
	pair := fixture.addRoute(t, ctx, id.RouteID("video"), dead.ID, dead.ID)
	fixture.addRoute(t, ctx, id.RouteID("audio"), otherRouteSibling.ID, otherRouteSibling.ID)
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), dead.ID))
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), sameRouteSibling.ID))
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("audio"), otherRouteSibling.ID))

	var calls []hookCall[string]
	fixture.recommit = func(ctx context.Context, routeID id.RouteID, storageKey string) error {
		calls = append(calls, hookCall[string]{routeID: routeID, storageKey: storageKey})
		require.NoError(t, pair.Switch().SetValue(ctx, int32(sameRouteSibling.ID)))
		require.NoError(t, pair.Syncer().SetValue(ctx, int32(sameRouteSibling.ID)))
		return nil
	}
	fixture.recreate = func(context.Context, id.RouteID, string) error {
		t.Fatal("recreate must not run when a same-route live sibling exists")
		return nil
	}
	handler := fixture.handler(t)

	result, err := handler.Evict(ctx, dead)
	require.NoError(t, err)

	require.Equal(t, []hookCall[string]{
		{routeID: id.RouteID("video"), storageKey: "same-route"},
	}, calls)
	require.Equal(t, []id.RouteID{id.RouteID("video")}, result.Recommitted)
	require.Empty(t, result.Recreated)
	require.Equal(t, sameRouteSibling.ID, pair.Current(ctx))
	require.Equal(t, sameRouteSibling.ID, pair.SyncerCurrent(ctx))
}

func TestEvictNoSiblingRecreatesOnceAndKeepsFailureDemotedForRetry(t *testing.T) {
	ctx := context.Background()
	recreateErr := errors.New("destination still unavailable")

	for _, testCase := range []struct {
		name          string
		recreateErr   error
		wantRecreated bool
	}{
		{name: "success", wantRecreated: true},
		{name: "failure", recreateErr: recreateErr},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFixture(t, ctx)
			dead := fixture.putMember(t, ctx, id.MemberID(4), "dead")
			pair := fixture.addRoute(t, ctx, id.RouteID("video"), dead.ID, dead.ID)
			require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), dead.ID))
			fixture.retry = &recordingRetryTracker[string]{}

			var calls []hookCall[string]
			fixture.recreate = func(_ context.Context, routeID id.RouteID, storageKey string) error {
				calls = append(calls, hookCall[string]{routeID: routeID, storageKey: storageKey})
				return testCase.recreateErr
			}
			handler := fixture.handler(t)

			result, err := handler.Evict(ctx, dead)
			if testCase.recreateErr != nil {
				require.ErrorIs(t, err, recreateErr)
				require.Len(t, result.RecreateErrs, 1)
				require.ErrorIs(t, result.RecreateErrs[0], recreateErr)
			} else {
				require.NoError(t, err)
				require.Empty(t, result.RecreateErrs)
			}

			require.Equal(t, []hookCall[string]{
				{routeID: id.RouteID("video"), storageKey: "dead"},
			}, calls)
			require.Equal(t, []id.RouteID{id.RouteID("video")}, result.DemotedRoutes)
			require.Equal(t, []id.RouteID{id.RouteID("video")}, result.RetryRecorded)
			if testCase.wantRecreated {
				require.Equal(t, []id.RouteID{id.RouteID("video")}, result.Recreated)
			} else {
				require.Empty(t, result.Recreated)
			}
			require.Equal(t, id.NoMemberID, pair.Current(ctx))
			require.Equal(t, id.NoMemberID, pair.SyncerCurrent(ctx))
		})
	}
}

func TestEvictRecordsRetryBeforeDemotionAndSkipsRetryWhenUnconfigured(t *testing.T) {
	ctx := context.Background()
	fixture := newFixture(t, ctx)
	dead := fixture.putMember(t, ctx, id.MemberID(6), "dead")
	pair := fixture.addRoute(t, ctx, id.RouteID("video"), dead.ID, dead.ID)
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), dead.ID))
	fixture.retry = &recordingRetryTracker[string]{
		onRecord: func(_ id.RouteID, _ string) {
			assert.Equal(t, dead.ID, pair.Current(ctx))
			assert.Equal(t, dead.ID, pair.SyncerCurrent(ctx))
		},
	}
	handler := fixture.handler(t)

	result, err := handler.Evict(ctx, dead)
	require.NoError(t, err)
	require.Equal(t, []id.RouteID{id.RouteID("video")}, result.RetryRecorded)

	fixture = newFixture(t, ctx)
	dead = fixture.putMember(t, ctx, id.MemberID(6), "dead")
	fixture.addRoute(t, ctx, id.RouteID("video"), dead.ID, dead.ID)
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), dead.ID))
	handler = fixture.handler(t)

	result, err = handler.Evict(ctx, dead)
	require.NoError(t, err)
	require.Empty(t, result.RetryRecorded)
}

func TestEvictUnknownAttachedRouteWrapsUnknownRouteID(t *testing.T) {
	ctx := context.Background()
	fixture := newFixture(t, ctx)
	dead := fixture.putMember(t, ctx, id.MemberID(10), "dead")
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("missing"), dead.ID))
	handler := fixture.handler(t)

	result, err := handler.Evict(ctx, dead)
	require.ErrorIs(t, err, route.ErrUnknownRouteID)
	require.Equal(t, eviction.Result{}, result)

	_, ok := fixture.members.LoadByID(ctx, dead.ID)
	require.False(t, ok)
}

func TestEvictFormatsKeysThroughSafeFormatterAndRedactsByDefault(t *testing.T) {
	ctx := context.Background()
	sensitiveKey := "sensitive://raw-key"
	expectedErr := errors.New("recreate failed")

	fixture := newFixture(t, ctx)
	dead := fixture.putMember(t, ctx, id.MemberID(11), sensitiveKey)
	fixture.addRoute(t, ctx, id.RouteID("video"), dead.ID, dead.ID)
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), dead.ID))
	fixture.recreate = func(context.Context, id.RouteID, string) error {
		return expectedErr
	}
	handler := fixture.handler(t)

	result, err := handler.Evict(ctx, dead)
	require.ErrorIs(t, err, expectedErr)
	require.Len(t, result.RecreateErrs, 1)
	require.Contains(t, result.RecreateErrs[0].Error(), "<redacted>")
	require.NotContains(t, result.RecreateErrs[0].Error(), sensitiveKey)

	fixture = newFixture(t, ctx)
	dead = fixture.putMember(t, ctx, id.MemberID(11), sensitiveKey)
	fixture.addRoute(t, ctx, id.RouteID("video"), dead.ID, dead.ID)
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), dead.ID))
	fixture.safeKeys = safekey.FormatterFunc[string](func(context.Context, string) string {
		return "safe-key"
	})
	fixture.recreate = func(context.Context, id.RouteID, string) error {
		return expectedErr
	}
	handler = fixture.handler(t)

	result, err = handler.Evict(ctx, dead)
	require.ErrorIs(t, err, expectedErr)
	require.Len(t, result.RecreateErrs, 1)
	require.Contains(t, result.RecreateErrs[0].Error(), "safe-key")
	require.False(t, strings.Contains(result.RecreateErrs[0].Error(), sensitiveKey))
}

func TestEvictHooksCanCallRegistriesAndObserveSnapshots(t *testing.T) {
	ctx := context.Background()
	fixture := newFixture(t, ctx)
	dead := fixture.putMember(t, ctx, id.MemberID(12), "reusable")
	pair := fixture.addRoute(t, ctx, id.RouteID("video"), dead.ID, dead.ID)
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), dead.ID))

	fixture.recreate = func(ctx context.Context, routeID id.RouteID, storageKey string) error {
		_, err := fixture.members.Put(ctx, id.MemberID(99), storageKey, testMember{})
		require.NoError(t, err, "dead storage key must be unbound before recreate hook")
		require.NoError(t, fixture.routes.Add(ctx, route.State{ID: id.RouteID("metadata")}))
		require.NoError(t, fixture.attachments.Attach(ctx, routeID, id.MemberID(99)))
		return nil
	}
	handler := fixture.handler(t)

	result, err := handler.Evict(ctx, dead)
	require.NoError(t, err)
	require.Equal(t, []id.RouteID{id.RouteID("video")}, result.Recreated)
	require.Equal(t, id.NoMemberID, pair.Current(ctx))

	loaded, ok := fixture.members.LoadByStorageKey(ctx, "reusable")
	require.True(t, ok)
	require.Equal(t, id.MemberID(99), loaded.ID)
	_, ok = fixture.routes.Load(ctx, id.RouteID("metadata"))
	require.True(t, ok)
	require.Contains(t, fixture.attachments.MembersForRoute(ctx, id.RouteID("video")), id.MemberID(99))
}

func TestEvictRecommitHookCanCallRegistries(t *testing.T) {
	ctx := context.Background()
	fixture := newFixture(t, ctx)
	dead := fixture.putMember(t, ctx, id.MemberID(13), "dead")
	sibling := fixture.putMember(t, ctx, id.MemberID(14), "sibling")
	pair := fixture.addRoute(t, ctx, id.RouteID("video"), dead.ID, dead.ID)
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), dead.ID))
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("video"), sibling.ID))

	fixture.recommit = func(ctx context.Context, routeID id.RouteID, storageKey string) error {
		loaded, ok := fixture.members.LoadByStorageKey(ctx, storageKey)
		require.True(t, ok)
		require.Equal(t, sibling.ID, loaded.ID)
		require.NoError(t, fixture.routes.Add(ctx, route.State{ID: id.RouteID("metadata")}))
		require.NoError(t, fixture.attachments.Attach(ctx, routeID, sibling.ID))
		require.NoError(t, pair.Switch().SetValue(ctx, int32(sibling.ID)))
		require.NoError(t, pair.Syncer().SetValue(ctx, int32(sibling.ID)))
		return nil
	}
	handler := fixture.handler(t)

	result, err := handler.Evict(ctx, dead)
	require.NoError(t, err)
	require.Equal(t, []id.RouteID{id.RouteID("video")}, result.Recommitted)

	_, ok := fixture.routes.Load(ctx, id.RouteID("metadata"))
	require.True(t, ok)
	require.Equal(t, sibling.ID, pair.Current(ctx))
	require.Equal(t, sibling.ID, pair.SyncerCurrent(ctx))
}

type testMember struct{}

type fixture struct {
	members     *member.Registry[string, testMember]
	routes      *route.Registry
	attachments *attachment.Index
	retry       eviction.RetryTracker[string]
	recommit    eviction.RecommitFunc[string]
	recreate    eviction.RecreateFunc[string]
	safeKeys    safekey.Formatter[string]
}

func newFixture(
	t *testing.T,
	ctx context.Context,
) *fixture {
	t.Helper()

	f := &fixture{
		members:     member.NewRegistry[string, testMember](),
		routes:      route.NewRegistry(),
		attachments: attachment.NewIndex(),
	}
	f.recommit = func(context.Context, id.RouteID, string) error {
		return nil
	}
	return f
}

func (f *fixture) handler(
	t *testing.T,
) *eviction.Handler[string, testMember] {
	t.Helper()

	handler, err := eviction.NewHandler(eviction.Config[string, testMember]{
		Members:          f.members,
		Routes:           f.routes,
		Attachments:      f.attachments,
		RetryTracker:     f.retry,
		Recommit:         f.recommit,
		Recreate:         f.recreate,
		SafeKeyFormatter: f.safeKeys,
	})
	require.NoError(t, err)
	return handler
}

func (f *fixture) putMember(
	t *testing.T,
	ctx context.Context,
	memberID id.MemberID,
	storageKey string,
) member.Entry[string, testMember] {
	t.Helper()

	entry, err := f.members.Put(ctx, memberID, storageKey, testMember{})
	require.NoError(t, err)
	return entry
}

func (f *fixture) addRoute(
	t *testing.T,
	ctx context.Context,
	routeID id.RouteID,
	current id.MemberID,
	syncer id.MemberID,
) *switchpair.Pair {
	t.Helper()

	pair, err := switchpair.New(ctx, switchpair.Config{InitialValue: current})
	require.NoError(t, err)
	pair.Syncer().CurrentValue.Store(int32(syncer))
	require.NoError(t, f.routes.Add(ctx, route.State{
		ID:   routeID,
		Pair: pair,
	}))
	return pair
}

type hookCall[K comparable] struct {
	routeID    id.RouteID
	storageKey K
}

type retryCall[K comparable] struct {
	routeID    id.RouteID
	storageKey K
}

type recordingRetryTracker[K comparable] struct {
	calls    []retryCall[K]
	onRecord func(routeID id.RouteID, storageKey K)
}

func (r *recordingRetryTracker[K]) RecordDemotion(
	_ context.Context,
	routeID id.RouteID,
	storageKey K,
) {
	r.calls = append(r.calls, retryCall[K]{
		routeID:    routeID,
		storageKey: storageKey,
	})
	if r.onRecord != nil {
		r.onRecord(routeID, storageKey)
	}
}
