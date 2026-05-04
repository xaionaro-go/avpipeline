package orphanretry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/orphanretry"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

func TestNewTrackerRejectsNilDependencies(t *testing.T) {
	clock := newManualClock(time.Date(2026, 5, 4, 19, 0, 0, 0, time.UTC))
	policy := orphanretry.StreamMuxCompatibilityPolicy[string]()
	recreate := func(context.Context, id.RouteID, string) error {
		return nil
	}

	testCases := []struct {
		name     string
		policy   orphanretry.Policy[string]
		now      orphanretry.NowFunc
		recreate orphanretry.RecreateFunc[string]
	}{
		{
			name:     "policy",
			now:      clock.Now,
			recreate: recreate,
		},
		{
			name:     "now",
			policy:   policy,
			recreate: recreate,
		},
		{
			name:   "recreate",
			policy: policy,
			now:    clock.Now,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tracker, err := orphanretry.NewTracker[string](
				testCase.policy,
				testCase.now,
				testCase.recreate,
			)
			require.Nil(t, tracker)
			require.ErrorIs(t, err, selectorerr.ErrInvalidConfig)
		})
	}
}

func TestStreamMuxCompatibilityPolicyRetriesEveryTickUntilRecovery(t *testing.T) {
	ctx := context.Background()
	clock := newManualClock(time.Date(2026, 5, 4, 20, 0, 0, 0, time.UTC))

	var calls []recreateCall[string]
	tracker, err := orphanretry.NewTracker[string](
		orphanretry.StreamMuxCompatibilityPolicy[string](),
		clock.Now,
		recordingRecreate(&calls, errors.New("destination down")),
	)
	require.NoError(t, err)
	tracker.RecordDemotion(ctx, id.RouteID("video"), "dead-key")

	currentValue := id.NoMemberID
	current := func(context.Context, id.RouteID) (id.MemberID, bool) {
		return currentValue, true
	}

	for range 5 {
		require.Error(t, tracker.Tick(ctx, current))
	}
	require.Len(t, calls, 5, "streammux compatibility must not suppress retry ticks by hidden wall clock state")

	currentValue = id.MemberID(99)
	require.NoError(t, tracker.Tick(ctx, current))
	require.Len(t, calls, 5, "recovery cleanup must silence future retry hooks")

	currentValue = id.NoMemberID
	require.NoError(t, tracker.Tick(ctx, current))
	require.Len(t, calls, 5, "stale-key cleanup must remove recovered route retry state")
}

func TestStreamMuxCompatibilityPolicyNeverRetires(t *testing.T) {
	ctx := context.Background()
	clock := newManualClock(time.Date(2026, 5, 4, 21, 0, 0, 0, time.UTC))

	var calls []recreateCall[string]
	tracker, err := orphanretry.NewTracker[string](
		orphanretry.StreamMuxCompatibilityPolicy[string](),
		clock.Now,
		recordingRecreate(&calls, errors.New("persistent destination fault")),
	)
	require.NoError(t, err)
	tracker.RecordDemotion(ctx, id.RouteID("video"), "dead-key")

	for range 1000 {
		require.Error(t, tracker.Tick(ctx, fixedCurrent(id.NoMemberID, true)))
	}
	require.Len(t, calls, 1000, "streammux compatibility must have no max-attempt retirement")
}

func TestTrackerDeletesStaleRouteWhenCurrentRouteIsMissing(t *testing.T) {
	ctx := context.Background()
	clock := newManualClock(time.Date(2026, 5, 4, 22, 0, 0, 0, time.UTC))

	var calls []recreateCall[string]
	tracker, err := orphanretry.NewTracker[string](
		orphanretry.StreamMuxCompatibilityPolicy[string](),
		clock.Now,
		recordingRecreate(&calls, nil),
	)
	require.NoError(t, err)
	tracker.RecordDemotion(ctx, id.RouteID("gone"), "dead-key")

	require.NoError(t, tracker.Tick(ctx, fixedCurrent(id.NoMemberID, false)))
	require.Empty(t, calls)

	require.NoError(t, tracker.Tick(ctx, fixedCurrent(id.NoMemberID, true)))
	require.Empty(t, calls, "a later matching route must not inherit a stale orphan key")
}

func TestTrackerCallsRecreateOutsideInternalLock(t *testing.T) {
	ctx := context.Background()
	clock := newManualClock(time.Date(2026, 5, 4, 23, 0, 0, 0, time.UTC))
	var tracker *orphanretry.Tracker[string]
	var err error
	tracker, err = orphanretry.NewTracker[string](
		orphanretry.StreamMuxCompatibilityPolicy[string](),
		clock.Now,
		func(ctx context.Context, routeID id.RouteID, _ string) error {
			tracker.MarkRecovered(ctx, routeID)
			return nil
		},
	)
	require.NoError(t, err)
	tracker.RecordDemotion(ctx, id.RouteID("video"), "dead-key")

	require.NoError(t, tracker.Tick(ctx, fixedCurrent(id.NoMemberID, true)))
	require.NoError(t, tracker.Tick(ctx, fixedCurrent(id.NoMemberID, true)))
}

func TestTrackerStaleSnapshotDoesNotDeleteNewDemotionAtSameTime(t *testing.T) {
	ctx := context.Background()
	clock := newManualClock(time.Date(2026, 5, 4, 23, 30, 0, 0, time.UTC))

	var calls []recreateCall[string]
	tracker, err := orphanretry.NewTracker[string](
		orphanretry.StreamMuxCompatibilityPolicy[string](),
		clock.Now,
		recordingRecreate(&calls, nil),
	)
	require.NoError(t, err)
	tracker.RecordDemotion(ctx, id.RouteID("video"), "dead-key")

	redemoted := false
	current := func(ctx context.Context, routeID id.RouteID) (id.MemberID, bool) {
		require.Equal(t, id.RouteID("video"), routeID)
		if !redemoted {
			redemoted = true
			tracker.RecordDemotion(ctx, routeID, "dead-key")
			return id.MemberID(7), true
		}

		return id.NoMemberID, true
	}

	require.NoError(t, tracker.Tick(ctx, current))
	require.Empty(t, calls)

	require.NoError(t, tracker.Tick(ctx, current))
	require.Equal(t, []recreateCall[string]{
		{routeID: id.RouteID("video"), storageKey: "dead-key"},
	}, calls)
}

func TestTrackerTicksRoutesInDeterministicOrder(t *testing.T) {
	ctx := context.Background()
	clock := newManualClock(time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC))

	var calls []recreateCall[string]
	tracker, err := orphanretry.NewTracker[string](
		orphanretry.StreamMuxCompatibilityPolicy[string](),
		clock.Now,
		recordingRecreate(&calls, nil),
	)
	require.NoError(t, err)
	tracker.RecordDemotion(ctx, id.RouteID("video"), "video-key")
	tracker.RecordDemotion(ctx, id.RouteID("audio"), "audio-key")
	tracker.RecordDemotion(ctx, id.RouteID("all"), "all-key")

	require.NoError(t, tracker.Tick(ctx, fixedCurrent(id.NoMemberID, true)))
	require.Equal(t, []recreateCall[string]{
		{routeID: id.RouteID("all"), storageKey: "all-key"},
		{routeID: id.RouteID("audio"), storageKey: "audio-key"},
		{routeID: id.RouteID("video"), storageKey: "video-key"},
	}, calls)
}

func TestTrackerReturnsJoinedRecreateErrors(t *testing.T) {
	ctx := context.Background()
	clock := newManualClock(time.Date(2026, 5, 5, 1, 0, 0, 0, time.UTC))
	audioErr := errors.New("audio recreate failed")
	videoErr := errors.New("video recreate failed")

	tracker, err := orphanretry.NewTracker[string](
		orphanretry.StreamMuxCompatibilityPolicy[string](),
		clock.Now,
		func(_ context.Context, routeID id.RouteID, _ string) error {
			switch routeID {
			case id.RouteID("audio"):
				return audioErr
			case id.RouteID("video"):
				return videoErr
			default:
				return nil
			}
		},
	)
	require.NoError(t, err)
	tracker.RecordDemotion(ctx, id.RouteID("audio"), "audio-key")
	tracker.RecordDemotion(ctx, id.RouteID("video"), "video-key")

	err = tracker.Tick(ctx, fixedCurrent(id.NoMemberID, true))
	require.ErrorIs(t, err, audioErr)
	require.ErrorIs(t, err, videoErr)
}

type manualClock struct {
	now time.Time
}

func newManualClock(
	now time.Time,
) *manualClock {
	return &manualClock{now: now}
}

func (c *manualClock) Now() time.Time {
	return c.now
}

func (c *manualClock) Advance(
	d time.Duration,
) {
	c.now = c.now.Add(d)
}

type recreateCall[K comparable] struct {
	routeID    id.RouteID
	storageKey K
}

func recordingRecreate[K comparable](
	calls *[]recreateCall[K],
	err error,
) orphanretry.RecreateFunc[K] {
	return func(_ context.Context, routeID id.RouteID, storageKey K) error {
		*calls = append(*calls, recreateCall[K]{
			routeID:    routeID,
			storageKey: storageKey,
		})
		return err
	}
}

func fixedCurrent(
	memberID id.MemberID,
	ok bool,
) func(context.Context, id.RouteID) (id.MemberID, bool) {
	return func(context.Context, id.RouteID) (id.MemberID, bool) {
		return memberID, ok
	}
}
