package fanout_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/eviction"
	"github.com/xaionaro-go/avpipeline/preset/selector/fanout"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

func TestNewValidatesRequiredControllerFields(t *testing.T) {
	ctx := context.Background()
	fixture := newControllerFixture(t, ctx)

	testCases := []struct {
		name   string
		mutate func(*fanout.Config[string, testMember])
	}{
		{name: "routes", mutate: func(cfg *fanout.Config[string, testMember]) { cfg.Routes = nil }},
		{name: "members", mutate: func(cfg *fanout.Config[string, testMember]) { cfg.Members = nil }},
		{name: "attachments", mutate: func(cfg *fanout.Config[string, testMember]) { cfg.Attachments = nil }},
		{name: "member ids", mutate: func(cfg *fanout.Config[string, testMember]) { cfg.MemberIDs = nil }},
		{name: "creation planner", mutate: func(cfg *fanout.Config[string, testMember]) { cfg.CreationPlanner = nil }},
		{name: "preferred route planner", mutate: func(cfg *fanout.Config[string, testMember]) { cfg.PreferredRoutePlanner = nil }},
		{name: "different output policy", mutate: func(cfg *fanout.Config[string, testMember]) { cfg.DifferentOutputPolicy = nil }},
		{name: "preference switcher", mutate: func(cfg *fanout.Config[string, testMember]) { cfg.PreferenceSwitcher = nil }},
		{name: "eviction", mutate: func(cfg *fanout.Config[string, testMember]) { cfg.Eviction = nil }},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := fixture.config()
			testCase.mutate(&cfg)

			controller, err := fanout.New(ctx, cfg)
			require.Nil(t, controller)
			require.ErrorIs(t, err, selectorerr.ErrInvalidConfig)
			require.Contains(t, err.Error(), testCase.name)
		})
	}
}

func TestAddRouteRejectsDuplicatesThroughRouteRegistry(t *testing.T) {
	ctx := context.Background()
	fixture := newControllerFixture(t, ctx)
	controller := fixture.controller(t, ctx)
	pair := mustPair(t, ctx, id.MemberID(1))

	require.NoError(t, controller.AddRoute(ctx, id.RouteID("all"), pair))

	err := controller.AddRoute(ctx, id.RouteID("all"), pair)
	require.ErrorIs(t, err, route.ErrDuplicateRouteID)
}

func TestAddMemberCreateAllocatesIgnoresCreateReuseIDAndAttachesRoutes(t *testing.T) {
	ctx := context.Background()
	fixture := newControllerFixture(t, ctx)
	fixture.creationPlanner = fanout.CreationPlannerFunc[string](func(
		context.Context,
		string,
		[]fanout.ExistingMember[string],
	) (fanout.CreationDecision[string], error) {
		return fanout.CreationDecision[string]{
			Action:        fanout.CreationActionCreate,
			StorageKey:    "storage",
			ReuseMemberID: id.MemberID(99),
			RouteIDs:      []id.RouteID{id.RouteID("all")},
		}, nil
	})
	controller := fixture.controller(t, ctx)
	require.NoError(t, controller.AddRoute(ctx, id.RouteID("all"), mustPair(t, ctx, id.NoMemberID)))

	entry, decision, err := controller.AddMember(ctx, "requested", testMember{name: "created"})
	require.NoError(t, err)
	require.Equal(t, fanout.CreationActionCreate, decision.Action)
	require.Equal(t, id.MemberID(10), entry.ID)
	require.Equal(t, "storage", entry.StorageKey)
	require.Equal(t, []id.RouteID{id.RouteID("all")}, fixture.attachments.RoutesForMember(ctx, entry.ID))
	require.Equal(t, 1, fixture.memberIDs.calls)

	loaded, ok := fixture.members.LoadByStorageKey(ctx, "storage")
	require.True(t, ok)
	require.Equal(t, entry.Token, loaded.Token)
}

func TestAddMemberReuseDoesNotAllocateAndRejectDoesNotUseReuseID(t *testing.T) {
	ctx := context.Background()

	t.Run("reuse", func(t *testing.T) {
		fixture := newControllerFixture(t, ctx)
		existing, err := fixture.members.Put(ctx, id.MemberID(3), "existing", testMember{name: "existing"})
		require.NoError(t, err)
		fixture.creationPlanner = fanout.CreationPlannerFunc[string](func(
			context.Context,
			string,
			[]fanout.ExistingMember[string],
		) (fanout.CreationDecision[string], error) {
			return fanout.CreationDecision[string]{
				Action:        fanout.CreationActionReuse,
				StorageKey:    "existing",
				ReuseMemberID: existing.ID,
				RouteIDs:      []id.RouteID{id.RouteID("all")},
			}, nil
		})
		controller := fixture.controller(t, ctx)
		require.NoError(t, controller.AddRoute(ctx, id.RouteID("all"), mustPair(t, ctx, existing.ID)))

		entry, decision, err := controller.AddMember(ctx, "requested", testMember{name: "ignored"})
		require.NoError(t, err)
		require.Equal(t, fanout.CreationActionReuse, decision.Action)
		require.Equal(t, existing.ID, entry.ID)
		require.Zero(t, fixture.memberIDs.calls)
		require.Equal(t, []id.RouteID{id.RouteID("all")}, fixture.attachments.RoutesForMember(ctx, existing.ID))
	})

	t.Run("reject", func(t *testing.T) {
		fixture := newControllerFixture(t, ctx)
		fixture.creationPlanner = fanout.CreationPlannerFunc[string](func(
			context.Context,
			string,
			[]fanout.ExistingMember[string],
		) (fanout.CreationDecision[string], error) {
			return fanout.CreationDecision[string]{
				Action:        fanout.CreationActionReject,
				StorageKey:    "secret://requested",
				ReuseMemberID: id.MemberID(3),
			}, nil
		})
		controller := fixture.controller(t, ctx)

		entry, decision, err := controller.AddMember(ctx, "secret://requested", testMember{name: "rejected"})
		require.ErrorIs(t, err, fanout.ErrCreationRejected)
		require.Equal(t, fanout.CreationActionReject, decision.Action)
		require.Zero(t, entry)
		require.Zero(t, fixture.memberIDs.calls)
		require.Contains(t, err.Error(), "<redacted>")
		require.NotContains(t, err.Error(), "secret://requested")
	})
}

func TestAddMemberRejectsInvalidRoutePlansBeforeMutation(t *testing.T) {
	ctx := context.Background()

	for _, testCase := range []struct {
		name     string
		routeIDs []id.RouteID
	}{
		{name: "missing route list", routeIDs: nil},
		{name: "empty route id", routeIDs: []id.RouteID{""}},
		{name: "duplicate route id", routeIDs: []id.RouteID{id.RouteID("all"), id.RouteID("all")}},
		{name: "unknown route id", routeIDs: []id.RouteID{id.RouteID("missing")}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newControllerFixture(t, ctx)
			fixture.creationPlanner = fanout.CreationPlannerFunc[string](func(
				context.Context,
				string,
				[]fanout.ExistingMember[string],
			) (fanout.CreationDecision[string], error) {
				return fanout.CreationDecision[string]{
					Action:     fanout.CreationActionCreate,
					StorageKey: "storage",
					RouteIDs:   testCase.routeIDs,
				}, nil
			})
			controller := fixture.controller(t, ctx)
			require.NoError(t, controller.AddRoute(ctx, id.RouteID("all"), mustPair(t, ctx, id.NoMemberID)))

			entry, _, err := controller.AddMember(ctx, "storage", testMember{name: "invalid"})
			require.ErrorIs(t, err, selectorerr.ErrInvalidRoutePlan)
			require.Zero(t, entry)
			require.Zero(t, fixture.memberIDs.calls)
			_, ok := fixture.members.LoadByStorageKey(ctx, "storage")
			require.False(t, ok)
		})
	}
}

func TestAddMemberWrapsPlannerAllocatorAndMissingReuseErrors(t *testing.T) {
	ctx := context.Background()
	expectedPlannerErr := errors.New("planner failed")
	expectedAllocatorErr := errors.New("allocator failed")

	t.Run("planner", func(t *testing.T) {
		fixture := newControllerFixture(t, ctx)
		fixture.creationPlanner = fanout.CreationPlannerFunc[string](func(
			context.Context,
			string,
			[]fanout.ExistingMember[string],
		) (fanout.CreationDecision[string], error) {
			return fanout.CreationDecision[string]{}, expectedPlannerErr
		})
		controller := fixture.controller(t, ctx)

		entry, _, err := controller.AddMember(ctx, "secret://requested", testMember{name: "planner"})
		require.ErrorIs(t, err, expectedPlannerErr)
		require.Zero(t, entry)
		require.Contains(t, err.Error(), "<redacted>")
		require.NotContains(t, err.Error(), "secret://requested")
	})

	t.Run("allocator", func(t *testing.T) {
		fixture := newControllerFixture(t, ctx)
		fixture.memberIDs.err = expectedAllocatorErr
		controller := fixture.controller(t, ctx)
		require.NoError(t, controller.AddRoute(ctx, id.RouteID("all"), mustPair(t, ctx, id.NoMemberID)))

		entry, _, err := controller.AddMember(ctx, "storage", testMember{name: "allocator"})
		require.ErrorIs(t, err, expectedAllocatorErr)
		require.Zero(t, entry)
	})

	t.Run("missing reuse member", func(t *testing.T) {
		fixture := newControllerFixture(t, ctx)
		fixture.creationPlanner = fanout.CreationPlannerFunc[string](func(
			context.Context,
			string,
			[]fanout.ExistingMember[string],
		) (fanout.CreationDecision[string], error) {
			return fanout.CreationDecision[string]{
				Action:        fanout.CreationActionReuse,
				StorageKey:    "missing",
				ReuseMemberID: id.MemberID(404),
			}, nil
		})
		controller := fixture.controller(t, ctx)

		entry, _, err := controller.AddMember(ctx, "missing", testMember{name: "missing"})
		require.ErrorIs(t, err, selectorerr.ErrInvalidRoutePlan)
		require.ErrorIs(t, err, selectorerr.ErrMemberNotFound)
		require.Zero(t, entry)
	})
}

func TestAddMemberWrapsDuplicateStorageKeyWithSafeFormatter(t *testing.T) {
	ctx := context.Background()
	rawKey := "secret://stable"
	fixture := newControllerFixture(t, ctx)
	fixture.safeKeys = safekey.FormatterFunc[string](func(context.Context, string) string {
		return "safe-stable"
	})
	fixture.creationPlanner = fanout.CreationPlannerFunc[string](func(
		context.Context,
		string,
		[]fanout.ExistingMember[string],
	) (fanout.CreationDecision[string], error) {
		return fanout.CreationDecision[string]{
			Action:     fanout.CreationActionCreate,
			StorageKey: rawKey,
			RouteIDs:   []id.RouteID{id.RouteID("all")},
		}, nil
	})
	_, err := fixture.members.Put(ctx, id.MemberID(2), rawKey, testMember{name: "old"})
	require.NoError(t, err)
	controller := fixture.controller(t, ctx)
	require.NoError(t, controller.AddRoute(ctx, id.RouteID("all"), mustPair(t, ctx, id.NoMemberID)))

	entry, _, err := controller.AddMember(ctx, rawKey, testMember{name: "new"})
	require.ErrorIs(t, err, member.ErrDuplicateStorageKey)
	require.Zero(t, entry)
	require.Contains(t, err.Error(), "safe-stable")
	require.False(t, strings.Contains(err.Error(), rawKey))
}

func TestSetPreferredDelegatesPlannerAndPolicy(t *testing.T) {
	ctx := context.Background()
	fixture := newControllerFixture(t, ctx)
	fixture.differentOutputPolicy = fanout.DifferentOutputPolicyFunc(func(context.Context) bool {
		return false
	})
	controller := fixture.controller(t, ctx)

	err := controller.SetPreferred(ctx, "target")
	require.ErrorIs(t, err, fanout.ErrDifferentOutputsNotAllowed)
	require.Zero(t, fixture.preferredPlanner.calls)
}

func TestSetPreferredPlansAndSwitchesThroughController(t *testing.T) {
	ctx := context.Background()
	fixture := newControllerFixture(t, ctx)
	target, err := fixture.members.Put(ctx, id.MemberID(7), "target", testMember{name: "target"})
	require.NoError(t, err)
	controller := fixture.controller(t, ctx)
	pair := mustPair(t, ctx, id.MemberID(1))
	require.NoError(t, controller.AddRoute(ctx, id.RouteID("all"), pair))
	require.NoError(t, fixture.attachments.Attach(ctx, id.RouteID("all"), target.ID))

	require.NoError(t, controller.SetPreferred(ctx, "target"))
	require.Equal(t, 1, fixture.preferredPlanner.calls)
	require.Equal(t, target.ID, pair.Current(ctx))
	require.Equal(t, target.ID, pair.SyncerCurrent(ctx))
}

func TestSetPreferredWrapsPlannerErrorsWithSafeKey(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("planner failed")
	fixture := newControllerFixture(t, ctx)
	fixture.preferredPlanner.err = expectedErr
	controller := fixture.controller(t, ctx)

	err := controller.SetPreferred(ctx, "secret://requested")
	require.ErrorIs(t, err, expectedErr)
	require.Contains(t, err.Error(), "<redacted>")
	require.NotContains(t, err.Error(), "secret://requested")
}

func TestEvictMemberAndRetryTickDelegateToConfiguredHandlers(t *testing.T) {
	ctx := context.Background()
	fixture := newControllerFixture(t, ctx)
	dead := member.Entry[string, testMember]{
		ID:         id.MemberID(4),
		StorageKey: "dead",
		Value:      testMember{name: "dead"},
	}
	fixture.eviction.result = eviction.Result{DemotedRoutes: []id.RouteID{id.RouteID("all")}}
	controller := fixture.controller(t, ctx)
	require.NoError(t, controller.AddRoute(ctx, id.RouteID("all"), mustPair(t, ctx, id.MemberID(8))))

	result, err := controller.EvictMember(ctx, dead)
	require.NoError(t, err)
	require.Equal(t, fixture.eviction.result, result)
	require.Equal(t, []member.Entry[string, testMember]{dead}, fixture.eviction.calls)

	fixture.retryTracker.currentChecks = []id.RouteID{id.RouteID("all"), id.RouteID("missing")}
	require.NoError(t, controller.RetryTick(ctx))
	require.True(t, fixture.retryTracker.called)
	require.Equal(t, map[id.RouteID]id.MemberID{
		id.RouteID("all"): id.MemberID(8),
	}, fixture.retryTracker.currentValues)
	require.Equal(t, []id.RouteID{id.RouteID("missing")}, fixture.retryTracker.missingRoutes)
}

func TestRetryTickIsNoOpWhenRetryTrackerIsNotConfigured(t *testing.T) {
	ctx := context.Background()
	fixture := newControllerFixture(t, ctx)
	fixture.retryTracker = nil
	controller := fixture.controller(t, ctx)

	require.NoError(t, controller.RetryTick(ctx))
}

type testMember struct {
	name string
}

type controllerFixture struct {
	routes                *route.Registry
	members               *member.Registry[string, testMember]
	attachments           *attachment.Index
	memberIDs             *recordingAllocator
	creationPlanner       fanout.CreationPlanner[string]
	preferredPlanner      *recordingPreferredPlanner
	differentOutputPolicy fanout.DifferentOutputPolicy
	preferenceSwitcher    *fanout.PreferenceSwitcher[string, testMember]
	eviction              *recordingEviction
	retryTracker          *recordingRetryTracker
	safeKeys              safekey.Formatter[string]
}

func newControllerFixture(
	t *testing.T,
	ctx context.Context,
) *controllerFixture {
	t.Helper()

	routes := route.NewRegistry()
	fixture := &controllerFixture{
		routes:          routes,
		members:         member.NewRegistry[string, testMember](),
		attachments:     attachment.NewIndex(routeExistsFromRegistry(routes)),
		memberIDs:       &recordingAllocator{next: id.MemberID(10)},
		creationPlanner: fanout.NewCreationPlanner[string](fanout.ModeDifferentOutputsSameTracks, id.RouteID("all")),
		preferredPlanner: &recordingPreferredPlanner{
			plans: []fanout.RoutePlan[string]{
				{RouteID: id.RouteID("all"), StorageKey: "target"},
			},
		},
		differentOutputPolicy: fanout.NewDifferentOutputPolicy(fanout.ModeDifferentOutputsSameTracks),
		eviction:              &recordingEviction{},
		retryTracker:          &recordingRetryTracker{},
	}
	fixture.preferenceSwitcher = fanout.NewPreferenceSwitcher[string, testMember](nil)
	return fixture
}

func (f *controllerFixture) config() fanout.Config[string, testMember] {
	var retryTracker fanout.RetryTracker[string]
	if f.retryTracker != nil {
		retryTracker = f.retryTracker
	}

	return fanout.Config[string, testMember]{
		Routes:                f.routes,
		Members:               f.members,
		Attachments:           f.attachments,
		MemberIDs:             f.memberIDs,
		CreationPlanner:       f.creationPlanner,
		PreferredRoutePlanner: f.preferredPlanner,
		DifferentOutputPolicy: f.differentOutputPolicy,
		PreferenceSwitcher:    f.preferenceSwitcher,
		Eviction:              f.eviction,
		RetryTracker:          retryTracker,
		SafeKeyFormatter:      f.safeKeys,
	}
}

func (f *controllerFixture) controller(
	t *testing.T,
	ctx context.Context,
) *fanout.Controller[string, testMember] {
	t.Helper()

	controller, err := fanout.New(ctx, f.config())
	require.NoError(t, err)
	return controller
}

type recordingAllocator struct {
	next  id.MemberID
	calls int
	err   error
}

func (a *recordingAllocator) Allocate(
	context.Context,
) (id.MemberID, error) {
	a.calls++
	if a.err != nil {
		return 0, a.err
	}

	allocated := a.next
	a.next++
	return allocated, nil
}

type recordingPreferredPlanner struct {
	plans []fanout.RoutePlan[string]
	err   error
	calls int
}

func (p *recordingPreferredPlanner) PlanPreferred(
	context.Context,
	string,
) ([]fanout.RoutePlan[string], error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}

	return append([]fanout.RoutePlan[string](nil), p.plans...), nil
}

type recordingEviction struct {
	calls  []member.Entry[string, testMember]
	result eviction.Result
	err    error
}

func (e *recordingEviction) Evict(
	_ context.Context,
	dead member.Entry[string, testMember],
) (eviction.Result, error) {
	e.calls = append(e.calls, dead)
	return e.result, e.err
}

type recordingRetryTracker struct {
	called        bool
	currentChecks []id.RouteID
	currentValues map[id.RouteID]id.MemberID
	missingRoutes []id.RouteID
	err           error
}

func (r *recordingRetryTracker) Tick(
	ctx context.Context,
	current func(context.Context, id.RouteID) (id.MemberID, bool),
) error {
	r.called = true
	r.currentValues = map[id.RouteID]id.MemberID{}
	for _, routeID := range r.currentChecks {
		memberID, ok := current(ctx, routeID)
		if !ok {
			r.missingRoutes = append(r.missingRoutes, routeID)
			continue
		}
		r.currentValues[routeID] = memberID
	}

	return r.err
}

func routeExistsFromRegistry(
	routes *route.Registry,
) attachment.RouteExistsFunc {
	return func(
		ctx context.Context,
		routeID id.RouteID,
	) bool {
		_, ok := routes.Load(ctx, routeID)
		return ok
	}
}

func mustPair(
	t *testing.T,
	ctx context.Context,
	initial id.MemberID,
) *switchpair.Pair {
	t.Helper()

	var pair *switchpair.Pair
	var err error
	pair, err = switchpair.New(ctx, switchpair.Config{
		InitialValue: initial,
		Hooks: switchpair.Hooks{
			OnAfterSwitch: func(ctx context.Context, _ packetorframe.InputUnion, _, to id.MemberID) {
				require.NoError(t, pair.Syncer().SetValue(ctx, int32(to)))
			},
		},
	})
	require.NoError(t, err)
	return pair
}

var _ fanout.CreationPlanner[string] = fanout.CreationPlannerFunc[string](nil)

var _ = errors.Is
