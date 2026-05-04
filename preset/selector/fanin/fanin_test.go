package fanin_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/availability"
	"github.com/xaionaro-go/avpipeline/preset/selector/fanin"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/orphanretry"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchprogress"
)

func TestPriorityPolicyFirstAvailableAfterUsesSparseAvailability(t *testing.T) {
	ctx := context.Background()
	policy := fanin.PriorityPolicy[string, *recordingMember]{}

	candidates := []fanin.PriorityCandidate[string, *recordingMember]{
		candidate[string](0, availability.Present(nil)),
		candidate[string](5, availability.Absent()),
		candidate[string](10, availability.Present(availableSource(false))),
		candidate[string](12, availability.Present(nil)),
	}

	priority, ok, err := policy.FirstAvailableAfter(ctx, candidates, 0)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, id.MemberID(12), priority)

	priority, ok, err = policy.FirstAvailableAfter(ctx, candidates, id.NoMemberID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, id.MemberID(0), priority)

	priority, ok, err = policy.FirstAvailableAfter(ctx, []fanin.PriorityCandidate[string, *recordingMember]{
		candidate[string](2, availability.Present(nil)),
		candidate[string](0, availability.Present(nil)),
	}, id.NoMemberID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, id.MemberID(0), priority)

	_, _, err = policy.FirstAvailableAfter(ctx, []fanin.PriorityCandidate[string, *recordingMember]{
		candidate[string](-1, availability.Present(nil)),
	}, id.NoMemberID)
	require.ErrorIs(t, err, selectorerr.ErrInvalidConfig)
}

func TestOpenPromotionPromotesOnlyExistingAvailableHigherPriorityMember(t *testing.T) {
	ctx := context.Background()
	promotion := fanin.OpenPromotion[string, *recordingMember]{}
	candidates := []fanin.PriorityCandidate[string, *recordingMember]{
		candidate[string](1, availability.Present(nil)),
		candidate[string](3, availability.Present(nil)),
		candidate[string](5, availability.Present(availableSource(false))),
	}

	decision, err := promotion.PlanOpen(ctx, 3, 1, candidates)
	require.NoError(t, err)
	assert.True(t, decision.Promote)
	assert.Equal(t, id.MemberID(1), decision.Target)

	decision, err = promotion.PlanOpen(ctx, 3, 5, candidates)
	require.NoError(t, err)
	assert.False(t, decision.Promote)

	decision, err = promotion.PlanOpen(ctx, 3, 3, candidates)
	require.NoError(t, err)
	assert.False(t, decision.Promote)

	_, err = promotion.PlanOpen(ctx, 3, 7, candidates)
	require.ErrorIs(t, err, selectorerr.ErrMemberNotFound)
}

func TestFallbackHandlerUsesSparseFallbackAndSilencesInactiveErrors(t *testing.T) {
	ctx := context.Background()
	handler := fanin.FallbackHandler[string, *recordingMember]{}
	candidates := []fanin.PriorityCandidate[string, *recordingMember]{
		candidate[string](0, availability.Present(nil)),
		candidate[string](10, availability.Present(availableSource(false))),
		candidate[string](20, availability.Present(nil)),
	}

	decision, err := handler.HandleFailure(ctx, fanin.Failure[string]{
		RouteID:  id.RouteID("input"),
		MemberID: 0,
		Current:  0,
		Next:     id.NoMemberID,
		Cause:    errors.New("failed"),
	}, candidates)
	require.NoError(t, err)
	assert.False(t, decision.IgnoreFailure)
	assert.False(t, decision.UseRecreate)
	assert.Equal(t, id.MemberID(20), decision.SwitchTo)

	decision, err = handler.HandleFailure(ctx, fanin.Failure[string]{
		RouteID:  id.RouteID("input"),
		MemberID: 7,
		Current:  0,
		Next:     id.MemberID(1),
		Cause:    errors.New("inactive failed"),
	}, candidates)
	require.NoError(t, err)
	assert.True(t, decision.IgnoreFailure)

	_, err = handler.HandleFailure(ctx, fanin.Failure[string]{
		RouteID:  id.RouteID("input"),
		MemberID: 1,
		Current:  1,
		Next:     id.NoMemberID,
		Cause:    errors.New("missing active"),
	}, candidates)
	require.ErrorIs(t, err, selectorerr.ErrMemberNotFound)
}

func TestPauseMemberRejectsSoleActiveMemberWithoutMutating(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	controller := newController(t, pair, nil)
	sole := &recordingMember{}
	require.NoError(t, controller.AddMember(ctx, 0, "main", sole, nil))

	err := controller.PauseMember(ctx, 0)
	require.ErrorIs(t, err, fanin.ErrCannotPauseSoleActiveMember)
	assert.False(t, sole.paused)
	assert.Zero(t, sole.pauseCalls)
}

func TestPauseAndUnpauseMemberApplyLifecycleOutsidePlanning(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	controller := newController(t, pair, nil)
	main := &recordingMember{}
	fallback := &recordingMember{}
	require.NoError(t, controller.AddMember(ctx, 0, "main", main, nil))
	require.NoError(t, controller.AddMember(ctx, 1, "fallback", fallback, nil))

	require.NoError(t, controller.PauseMember(ctx, 1))
	assert.True(t, fallback.paused)
	assert.Equal(t, 1, fallback.pauseCalls)

	require.NoError(t, controller.PauseMember(ctx, 1))
	assert.Equal(t, 1, fallback.pauseCalls)

	require.NoError(t, controller.UnpauseMember(ctx, 1))
	assert.False(t, fallback.paused)
	assert.Equal(t, 1, fallback.unpauseCalls)

	fallback.pauseErr = errors.New("pause failed")
	err := controller.PauseMember(ctx, 1)
	require.ErrorIs(t, err, fallback.pauseErr)
	assert.False(t, fallback.paused)
}

func TestMissingMemberOperationsDoNotMutateOrLeakReservations(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	controller := newController(t, pair, nil)
	main := &recordingMember{}
	require.NoError(t, controller.AddMember(ctx, 0, "main", main, nil))

	for _, run := range []struct {
		name string
		call func() error
	}{
		{name: "pause", call: func() error { return controller.PauseMember(ctx, 7) }},
		{name: "unpause", call: func() error { return controller.UnpauseMember(ctx, 7) }},
		{name: "switch", call: func() error { return controller.SwitchTo(ctx, 7) }},
		{name: "open", call: func() error { return controller.OnMemberOpen(ctx, 7) }},
	} {
		t.Run(run.name, func(t *testing.T) {
			err := run.call()
			require.ErrorIs(t, err, selectorerr.ErrMemberNotFound)
			assert.Equal(t, id.MemberID(0), pair.Current(ctx))
			assert.Equal(t, id.NoMemberID, pair.Next(ctx))
			assert.Zero(t, main.pauseCalls)
			assert.Zero(t, main.unpauseCalls)
			assert.Zero(t, controller.gate.InFlight())
		})
	}

	pair.Switch().CurrentValue.Store(7)
	err := controller.OnMemberError(ctx, 7, errors.New("active missing"))
	require.ErrorIs(t, err, selectorerr.ErrMemberNotFound)
	assert.Equal(t, id.MemberID(7), pair.Current(ctx))
	assert.Zero(t, controller.gate.InFlight())
}

func TestSwitchToUnpausesTargetAndIntermediateThenPausesPreviousPending(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	pair.Switch().NextValue.Store(4)
	controller := newController(t, pair, nil)

	main := &recordingMember{}
	intermediate := &recordingMember{paused: true}
	target := &recordingMember{paused: true}
	previousPending := &recordingMember{}
	require.NoError(t, controller.AddMember(ctx, 0, "main", main, nil))
	require.NoError(t, controller.AddMember(ctx, 1, "mid", intermediate, nil))
	require.NoError(t, controller.AddMember(ctx, 2, "target", target, nil))
	require.NoError(t, controller.AddMember(ctx, 4, "pending", previousPending, nil))

	require.NoError(t, controller.SwitchTo(ctx, 2))
	assert.False(t, intermediate.paused)
	assert.False(t, target.paused)
	assert.True(t, previousPending.paused)
	assert.Equal(t, 1, intermediate.unpauseCalls)
	assert.Equal(t, 1, target.unpauseCalls)
	assert.Equal(t, 1, previousPending.pauseCalls)
	assert.Equal(t, id.MemberID(2), pair.Current(ctx))
	assert.Zero(t, controller.gate.InFlight())
}

func TestSwitchToPromotionPausesFormerLowerPriorityMembersAfterImmediateSwitch(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 4)
	controller := newController(t, pair, nil)
	target := &recordingMember{}
	intermediate := &recordingMember{}
	current := &recordingMember{}
	require.NoError(t, controller.AddMember(ctx, 2, "target", target, nil))
	require.NoError(t, controller.AddMember(ctx, 3, "mid", intermediate, nil))
	require.NoError(t, controller.AddMember(ctx, 4, "current", current, nil))

	require.NoError(t, controller.SwitchTo(ctx, 2))
	assert.Equal(t, id.MemberID(2), pair.Current(ctx))
	assert.False(t, target.paused)
	assert.True(t, intermediate.paused)
	assert.True(t, current.paused)
	assert.Equal(t, 1, intermediate.pauseCalls)
	assert.Equal(t, 1, current.pauseCalls)
}

func TestPausePlannerPausesPreviousPendingOnlyForLegacyRule(t *testing.T) {
	ctx := context.Background()
	planner := fanin.PausePlanner[string, *recordingMember]{}

	for _, run := range []struct {
		name            string
		current         id.MemberID
		previousPending id.MemberID
		target          id.MemberID
		wantPause       []id.MemberID
	}{
		{
			name:            "previous pending lower priority than current",
			current:         1,
			previousPending: 4,
			target:          2,
			wantPause:       []id.MemberID{4},
		},
		{
			name:            "previous pending higher priority than current",
			current:         2,
			previousPending: 1,
			target:          3,
		},
		{
			name:            "previous pending same as target",
			current:         1,
			previousPending: 4,
			target:          4,
		},
	} {
		t.Run(run.name, func(t *testing.T) {
			plan, err := planner.PlanSwitch(ctx, fanin.PauseState{
				Current:         run.current,
				PreviousPending: run.previousPending,
				Target:          run.target,
				Priorities:      []id.MemberID{0, 1, 2, 3, 4},
				Paused:          map[id.MemberID]bool{},
			})
			require.NoError(t, err)
			assert.Equal(t, run.wantPause, plan.PausePreviousPending)
		})
	}
}

func TestPausePlannerNoOpsUnpauseWhenTargetIsAlreadyUnpaused(t *testing.T) {
	ctx := context.Background()
	planner := fanin.PausePlanner[string, *recordingMember]{}

	plan, err := planner.PlanUnpause(ctx, fanin.PauseState{
		Target:     1,
		Priorities: []id.MemberID{1},
	}, 1)
	require.NoError(t, err)
	assert.Empty(t, plan.UnpauseBeforeSwitch)
}

func TestOnMemberErrorIgnoresInactiveMemberWithoutSideEffects(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	asyncErrors := newAsyncRecorder()
	controller := newControllerWithAsync(t, pair, nil, asyncErrors.Handle)
	active := &recordingMember{}
	inactive := &recordingMember{}
	require.NoError(t, controller.AddMember(ctx, 0, "active", active, nil))
	require.NoError(t, controller.AddMember(ctx, 5, "inactive", inactive, nil))

	require.NoError(t, controller.OnMemberError(ctx, 5, errors.New("inactive failed")))
	assert.Equal(t, id.MemberID(0), pair.Current(ctx))
	assert.Equal(t, id.NoMemberID, pair.Next(ctx))
	assert.Empty(t, asyncErrors.errs)
	assert.Zero(t, active.pauseCalls+active.unpauseCalls)
	assert.Zero(t, inactive.pauseCalls+inactive.unpauseCalls)
	assert.Zero(t, controller.gate.InFlight())
}

func TestDefaultRecreateDisabledKeepsLegacyNoFallbackBehavior(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	controller := newController(t, pair, nil)
	active := &recordingMember{}
	require.NoError(t, controller.AddMember(ctx, 0, "active", active, nil))

	require.NoError(t, controller.OnMemberError(ctx, 0, errors.New("active failed")))
	assert.Equal(t, id.MemberID(0), pair.Current(ctx))
	assert.Equal(t, id.MemberID(0), pair.SyncerCurrent(ctx))
	assert.Zero(t, controller.gate.InFlight())
	require.NoError(t, controller.RetryTick(ctx))
}

func TestOptInRecreateRecordsDemotionAndRetriesOncePerTickWhileOrphaned(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	recreateErr := errors.New("recreate failed")
	var calls []recreateCall[string]
	tracker := newTracker(t, func(_ context.Context, routeID id.RouteID, storageKey string) error {
		calls = append(calls, recreateCall[string]{routeID: routeID, storageKey: storageKey})
		return recreateErr
	})
	controller := newController(t, pair, &fanin.RecreateConfig[string]{
		Policy:  orphanretry.StreamMuxCompatibilityPolicy[string](),
		Tracker: tracker,
		Recreate: func(ctx context.Context, routeID id.RouteID, storageKey string) error {
			return trackerRecreate(ctx, routeID, storageKey, &calls, recreateErr)
		},
		RecordOnNoFallback: true,
	})
	active := &recordingMember{}
	require.NoError(t, controller.AddMember(ctx, 0, "active-key", active, nil))

	err := controller.OnMemberError(ctx, 0, errors.New("active failed"))
	require.ErrorIs(t, err, recreateErr)
	assert.Equal(t, id.NoMemberID, pair.Current(ctx))
	assert.Equal(t, id.NoMemberID, pair.SyncerCurrent(ctx))
	assert.Equal(t, []recreateCall[string]{
		{routeID: id.RouteID("input"), storageKey: "active-key"},
	}, calls)

	require.ErrorIs(t, controller.RetryTick(ctx), recreateErr)
	require.ErrorIs(t, controller.RetryTick(ctx), recreateErr)
	assert.Equal(t, []recreateCall[string]{
		{routeID: id.RouteID("input"), storageKey: "active-key"},
		{routeID: id.RouteID("input"), storageKey: "active-key"},
		{routeID: id.RouteID("input"), storageKey: "active-key"},
	}, calls)

	pair.Switch().CurrentValue.Store(9)
	require.NoError(t, controller.RetryTick(ctx))
	require.NoError(t, controller.RetryTick(ctx))
	assert.Len(t, calls, 3)
}

func TestSwitchLifecycleErrorsAreReportedAsAsyncErrors(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	pair.Switch().NextValue.Store(4)
	asyncErrors := newAsyncRecorder()
	controller := newControllerWithAsync(t, pair, nil, asyncErrors.Handle)
	unpauseErr := errors.New("unpause failed")
	pauseErr := errors.New("pause failed")
	require.NoError(t, controller.AddMember(ctx, 0, "main", &recordingMember{}, nil))
	require.NoError(t, controller.AddMember(ctx, 1, "target", &recordingMember{paused: true, unpauseErr: unpauseErr}, nil))
	require.NoError(t, controller.AddMember(ctx, 4, "pending", &recordingMember{pauseErr: pauseErr}, nil))

	require.NoError(t, controller.SwitchTo(ctx, 1))
	require.Len(t, asyncErrors.errs, 2)
	assert.ErrorIs(t, asyncErrors.errs[0], unpauseErr)
	assert.ErrorIs(t, asyncErrors.errs[1], pauseErr)
	assert.Zero(t, controller.gate.InFlight())
}

func TestNewBuildsRecreateTrackerWhenTrackerIsOmitted(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	recreateErr := errors.New("recreate failed")
	var calls []recreateCall[string]
	controller := newController(t, pair, &fanin.RecreateConfig[string]{
		Policy: orphanretry.StreamMuxCompatibilityPolicy[string](),
		Recreate: func(ctx context.Context, routeID id.RouteID, storageKey string) error {
			return trackerRecreate(ctx, routeID, storageKey, &calls, recreateErr)
		},
		RecordOnNoFallback: true,
	})
	require.NoError(t, controller.AddMember(ctx, 0, "active-key", &recordingMember{}, nil))

	require.ErrorIs(t, controller.OnMemberError(ctx, 0, errors.New("active failed")), recreateErr)
	require.ErrorIs(t, controller.RetryTick(ctx), recreateErr)
	assert.Equal(t, []recreateCall[string]{
		{routeID: id.RouteID("input"), storageKey: "active-key"},
		{routeID: id.RouteID("input"), storageKey: "active-key"},
	}, calls)
}

func TestConfiguredSwitchFailureRecordsRecreateDemotion(t *testing.T) {
	ctx := context.Background()
	switchErr := errors.New("switch failed")
	pair := newFailingPair(t, 0, switchErr)
	var calls []recreateCall[string]
	tracker := newTracker(t, func(_ context.Context, routeID id.RouteID, storageKey string) error {
		calls = append(calls, recreateCall[string]{routeID: routeID, storageKey: storageKey})
		return nil
	})
	controller := newController(t, pair, &fanin.RecreateConfig[string]{
		Policy:  orphanretry.StreamMuxCompatibilityPolicy[string](),
		Tracker: tracker,
		Recreate: func(ctx context.Context, routeID id.RouteID, storageKey string) error {
			return trackerRecreate(ctx, routeID, storageKey, &calls, nil)
		},
		RecordOnSwitchFailure: true,
	})
	active := &recordingMember{}
	fallback := &recordingMember{}
	require.NoError(t, controller.AddMember(ctx, 0, "active-key", active, nil))
	require.NoError(t, controller.AddMember(ctx, 1, "fallback-key", fallback, nil))

	err := controller.OnMemberError(ctx, 0, errors.New("active failed"))
	require.ErrorIs(t, err, switchErr)
	assert.Equal(t, id.NoMemberID, pair.Current(ctx))
	assert.Equal(t, []recreateCall[string]{
		{routeID: id.RouteID("input"), storageKey: "active-key"},
	}, calls)
}

func TestOnMemberOpenDelegatesPromotionSwitchAndCleansRecovery(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, id.NoMemberID)
	var retryCalls []recreateCall[string]
	tracker := newTracker(t, func(_ context.Context, routeID id.RouteID, storageKey string) error {
		retryCalls = append(retryCalls, recreateCall[string]{routeID: routeID, storageKey: storageKey})
		return nil
	})
	tracker.RecordDemotion(ctx, id.RouteID("input"), "stale-key")
	controller := newController(t, pair, &fanin.RecreateConfig[string]{
		Policy:  orphanretry.StreamMuxCompatibilityPolicy[string](),
		Tracker: tracker,
		Recreate: func(ctx context.Context, routeID id.RouteID, storageKey string) error {
			return trackerRecreate(ctx, routeID, storageKey, &retryCalls, nil)
		},
		RecordOnNoFallback: true,
	})
	recovered := &recordingMember{paused: true}
	require.NoError(t, controller.AddMember(ctx, 2, "recovered-key", recovered, nil))

	require.NoError(t, controller.OnMemberOpen(ctx, 2))
	assert.Equal(t, id.MemberID(2), pair.Current(ctx))
	assert.False(t, recovered.paused)
	require.NoError(t, controller.RetryTick(ctx))
	assert.Empty(t, retryCalls)
}

func TestRecreateRecoveryMarksCommittedRouteRecovered(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	var retryCalls []recreateCall[string]
	tracker := newTracker(t, func(_ context.Context, routeID id.RouteID, storageKey string) error {
		retryCalls = append(retryCalls, recreateCall[string]{routeID: routeID, storageKey: storageKey})
		return nil
	})
	recovery := fanin.RecreateRecovery[string, *recordingMember]{
		Pair:               pair,
		Tracker:            tracker,
		RecordOnNoFallback: true,
		Recreate: func(context.Context, id.RouteID, string) error {
			pair.Switch().CurrentValue.Store(5)
			return nil
		},
	}

	require.NoError(t, recovery.Recover(ctx, fanin.RecreateRequest[string]{
		Failure: fanin.Failure[string]{
			RouteID:    id.RouteID("input"),
			MemberID:   0,
			StorageKey: "key",
		},
		Trigger: fanin.RecreateTriggerNoFallback,
	}))
	pair.Switch().CurrentValue.Store(int32(id.NoMemberID))
	require.NoError(t, tracker.Tick(ctx, func(context.Context, id.RouteID) (id.MemberID, bool) {
		return id.NoMemberID, true
	}))
	assert.Empty(t, retryCalls)
}

func TestAddMemberDuplicateAndOpenWithoutPromotionDoNotMutate(t *testing.T) {
	ctx := context.Background()
	pair := newPair(t, 0)
	controller := newController(t, pair, nil)
	main := &recordingMember{}
	lowerPriority := &recordingMember{}
	require.NoError(t, controller.AddMember(ctx, 0, "main", main, nil))
	require.Error(t, controller.AddMember(ctx, 0, "duplicate-id", &recordingMember{}, nil))
	require.Error(t, controller.AddMember(ctx, 1, "main", &recordingMember{}, nil))
	require.NoError(t, controller.AddMember(ctx, 2, "lower", lowerPriority, nil))

	require.NoError(t, controller.OnMemberOpen(ctx, 2))
	assert.Equal(t, id.MemberID(0), pair.Current(ctx))
	assert.Zero(t, lowerPriority.unpauseCalls)
}

func TestRecreateRecoveryDisabledAndInvalidConfiguredRecovery(t *testing.T) {
	ctx := context.Background()
	var disabled fanin.RecreateRecovery[string, *recordingMember]
	require.NoError(t, disabled.Recover(ctx, fanin.RecreateRequest[string]{
		Failure: fanin.Failure[string]{
			RouteID:    id.RouteID("input"),
			MemberID:   0,
			StorageKey: "key",
		},
		Trigger: fanin.RecreateTriggerNoFallback,
	}))

	invalid := fanin.RecreateRecovery[string, *recordingMember]{
		RecordOnNoFallback: true,
	}
	err := invalid.Recover(ctx, fanin.RecreateRequest[string]{
		Failure: fanin.Failure[string]{
			RouteID:    id.RouteID("input"),
			MemberID:   0,
			StorageKey: "key",
		},
		Trigger: fanin.RecreateTriggerNoFallback,
	})
	require.ErrorIs(t, err, selectorerr.ErrInvalidConfig)
}

func TestControllerValidationWrapsInvalidConfig(t *testing.T) {
	ctx := context.Background()

	controller, err := fanin.New[string, *recordingMember](ctx, fanin.Config[string, *recordingMember]{})
	require.Nil(t, controller)
	require.ErrorIs(t, err, selectorerr.ErrInvalidConfig)

	pair := newPair(t, 0)
	validConfig := fanin.Config[string, *recordingMember]{
		RouteID:         id.RouteID("input"),
		Pair:            pair,
		Members:         member.NewRegistry[string, *recordingMember](),
		Gate:            &switchprogress.Gate{},
		AsyncErrors:     func(context.Context, error) {},
		PriorityPolicy:  &fanin.PriorityPolicy[string, *recordingMember]{},
		PausePlanner:    &fanin.PausePlanner[string, *recordingMember]{},
		OpenPromotion:   &fanin.OpenPromotion[string, *recordingMember]{},
		FallbackHandler: &fanin.FallbackHandler[string, *recordingMember]{},
	}
	controller, err = fanin.New[string, *recordingMember](ctx, validConfig)
	require.NoError(t, err)
	require.NotNil(t, controller)

	validConfig.Recreate = &fanin.RecreateConfig[string]{
		Recreate: func(context.Context, id.RouteID, string) error { return nil },
	}
	controller, err = fanin.New[string, *recordingMember](ctx, validConfig)
	require.Nil(t, controller)
	require.ErrorIs(t, err, selectorerr.ErrInvalidConfig)

	validConfig.Recreate = &fanin.RecreateConfig[string]{
		Policy: orphanretry.StreamMuxCompatibilityPolicy[string](),
	}
	controller, err = fanin.New[string, *recordingMember](ctx, validConfig)
	require.Nil(t, controller)
	require.ErrorIs(t, err, selectorerr.ErrInvalidConfig)
}

type recordingMember struct {
	paused       bool
	pauseCalls   int
	unpauseCalls int
	pauseErr     error
	unpauseErr   error
}

func (m *recordingMember) Pause(context.Context) error {
	m.pauseCalls++
	if m.pauseErr != nil {
		return m.pauseErr
	}
	m.paused = true
	return nil
}

func (m *recordingMember) Unpause(context.Context) error {
	m.unpauseCalls++
	if m.unpauseErr != nil {
		return m.unpauseErr
	}
	m.paused = false
	return nil
}

func (m *recordingMember) IsPaused(context.Context) bool {
	return m.paused
}

type availableSource bool

func (s availableSource) HasResources(context.Context) bool {
	return bool(s)
}

type asyncRecorder struct {
	errs []error
}

func newAsyncRecorder() *asyncRecorder {
	return &asyncRecorder{}
}

func (r *asyncRecorder) Handle(_ context.Context, err error) {
	r.errs = append(r.errs, err)
}

type recreateCall[K comparable] struct {
	routeID    id.RouteID
	storageKey K
}

func candidate[K comparable](
	priority id.MemberID,
	resource availability.Candidate,
) fanin.PriorityCandidate[K, *recordingMember] {
	return fanin.PriorityCandidate[K, *recordingMember]{
		Priority:     priority,
		Availability: resource,
	}
}

func newPair(
	t *testing.T,
	current id.MemberID,
) *switchpair.Pair {
	t.Helper()
	pair, err := switchpair.New(context.Background(), switchpair.Config{
		InitialValue: current,
	})
	require.NoError(t, err)
	return pair
}

func newFailingPair(
	t *testing.T,
	current id.MemberID,
	err error,
) *switchpair.Pair {
	t.Helper()
	pair, pairErr := switchpair.New(context.Background(), switchpair.Config{
		InitialValue: current,
		Hooks: switchpair.Hooks{
			OnSwitchRequest: func(context.Context, id.MemberID) error {
				return err
			},
		},
	})
	require.NoError(t, pairErr)
	return pair
}

func newController(
	t *testing.T,
	pair *switchpair.Pair,
	recreate *fanin.RecreateConfig[string],
) *testController {
	t.Helper()
	return newControllerWithAsync(t, pair, recreate, func(context.Context, error) {})
}

func newControllerWithAsync(
	t *testing.T,
	pair *switchpair.Pair,
	recreate *fanin.RecreateConfig[string],
	async fanin.AsyncErrorHandler,
) *testController {
	t.Helper()
	gate := &switchprogress.Gate{}
	controller, err := fanin.New[string, *recordingMember](context.Background(), fanin.Config[string, *recordingMember]{
		RouteID:     id.RouteID("input"),
		Pair:        pair,
		Members:     member.NewRegistry[string, *recordingMember](),
		Gate:        gate,
		AsyncErrors: async,
		Recreate:    recreate,
	})
	require.NoError(t, err)
	return &testController{
		Controller: controller,
		gate:       gate,
	}
}

type testController struct {
	*fanin.Controller[string, *recordingMember]
	gate *switchprogress.Gate
}

func newTracker(
	t *testing.T,
	recreate orphanretry.RecreateFunc[string],
) *orphanretry.Tracker[string] {
	t.Helper()
	tracker, err := orphanretry.NewTracker[string](
		orphanretry.StreamMuxCompatibilityPolicy[string](),
		func() time.Time { return time.Unix(0, 0) },
		recreate,
	)
	require.NoError(t, err)
	return tracker
}

func trackerRecreate(
	ctx context.Context,
	routeID id.RouteID,
	storageKey string,
	calls *[]recreateCall[string],
	err error,
) error {
	*calls = append(*calls, recreateCall[string]{
		routeID:    routeID,
		storageKey: storageKey,
	})
	return err
}
