package switchpair_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

func TestNewConfiguresInitialValuesAndFlags(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name      string
		syncFlags barrierstategetter.SwitchFlags
	}{
		{
			name:      "next output state block",
			syncFlags: barrierstategetter.SwitchFlagNextOutputStateBlock,
		},
		{
			name:      "inactive block",
			syncFlags: barrierstategetter.SwitchFlagInactiveBlock,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair, err := switchpair.New(ctx, switchpair.Config{
				InitialValue:     id.MemberID(7),
				SwitchKeepUnless: packetorframecondition.Static(true),
				SyncerKeepUnless: packetorframecondition.Static(false),
				SwitchFlags:      barrierstategetter.SwitchFlagFirstPacketAfterSwitchPassBothOutputs,
				SyncerFlags:      tc.syncFlags,
			})
			require.NoError(t, err)

			assert.Equal(t, id.MemberID(7), pair.Current(ctx))
			assert.Equal(t, id.MemberID(7), pair.SyncerCurrent(ctx))
			assert.Equal(t, id.NoMemberID, pair.Next(ctx))
			assert.True(t, pair.Switch().Flags.HasAny(barrierstategetter.SwitchFlagFirstPacketAfterSwitchPassBothOutputs))
			assert.Equal(t, tc.syncFlags, pair.Syncer().Flags)
			assert.NotNil(t, pair.Switch().GetKeepUnless())
			assert.NotNil(t, pair.Syncer().GetKeepUnless())
		})
	}
}

func TestSetValueForwardsBarrierHooksAndAllowsRouteHookToAdvanceSyncer(t *testing.T) {
	ctx := context.Background()
	var pair *switchpair.Pair
	var events []string

	var err error
	pair, err = switchpair.New(ctx, switchpair.Config{
		InitialValue:     id.MemberID(1),
		SwitchKeepUnless: packetorframecondition.Static(true),
		Hooks: switchpair.Hooks{
			OnSwitchRequest: func(ctx context.Context, to id.MemberID) error {
				events = append(events, "request")
				assert.Equal(t, id.MemberID(2), to)
				return nil
			},
			OnBeforeSwitch: func(ctx context.Context, in packetorframe.InputUnion, from id.MemberID, to id.MemberID) {
				events = append(events, "before")
				assert.Equal(t, id.MemberID(1), from)
				assert.Equal(t, id.MemberID(2), to)
			},
			OnInterruptedSwitch: func(ctx context.Context, in packetorframe.InputUnion, from id.MemberID, to id.MemberID) {
				events = append(events, "interrupted")
			},
			OnAfterSwitch: func(ctx context.Context, in packetorframe.InputUnion, from id.MemberID, to id.MemberID) {
				events = append(events, "after")
				require.NoError(t, pair.Syncer().SetValue(ctx, int32(to)))
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, pair.SetValue(ctx, id.MemberID(2)))
	assert.Equal(t, id.MemberID(1), pair.Current(ctx))
	assert.Equal(t, id.MemberID(2), pair.Next(ctx))
	assert.Equal(t, id.MemberID(1), pair.SyncerCurrent(ctx))
	assert.Equal(t, []string{"request"}, events)

	state, _ := pair.Switch().Output(2).GetState(ctx, packetorframe.InputUnion{})
	assert.Equal(t, barrierstategetter.StatePass, state)
	assert.Equal(t, id.MemberID(2), pair.Current(ctx))
	assert.Equal(t, id.NoMemberID, pair.Next(ctx))
	assert.Equal(t, id.MemberID(2), pair.SyncerCurrent(ctx))
	assert.Equal(t, []string{"request", "before", "after"}, events)
}

func TestSetValueForwardsInterruptedSwitchHookOnCurrentValue(t *testing.T) {
	ctx := context.Background()
	var interrupted bool
	var afterCalled bool

	pair, err := switchpair.New(ctx, switchpair.Config{
		InitialValue: id.MemberID(3),
		Hooks: switchpair.Hooks{
			OnInterruptedSwitch: func(ctx context.Context, in packetorframe.InputUnion, from id.MemberID, to id.MemberID) {
				interrupted = true
				assert.Nil(t, in.Get())
				assert.Equal(t, id.MemberID(3), from)
				assert.Equal(t, id.MemberID(3), to)
			},
			OnAfterSwitch: func(ctx context.Context, in packetorframe.InputUnion, from id.MemberID, to id.MemberID) {
				afterCalled = true
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, pair.SetValue(ctx, id.MemberID(3)))
	assert.True(t, interrupted)
	assert.False(t, afterCalled)
	assert.Equal(t, id.MemberID(3), pair.Current(ctx))
	assert.Equal(t, id.NoMemberID, pair.Next(ctx))
}

func TestSetValueReturnsSwitchRequestError(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("blocked")

	pair, err := switchpair.New(ctx, switchpair.Config{
		InitialValue: id.MemberID(1),
		Hooks: switchpair.Hooks{
			OnSwitchRequest: func(ctx context.Context, to id.MemberID) error {
				return expectedErr
			},
		},
	})
	require.NoError(t, err)

	err = pair.SetValue(ctx, id.MemberID(2))
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
	assert.Equal(t, id.MemberID(1), pair.Current(ctx))
	assert.Equal(t, id.NoMemberID, pair.Next(ctx))
}

func TestDemoteIfCurrentOnlyDemotesMatchingCurrentValues(t *testing.T) {
	ctx := context.Background()

	pair, err := switchpair.New(ctx, switchpair.Config{InitialValue: id.MemberID(5)})
	require.NoError(t, err)

	demotion := pair.DemoteIfCurrent(ctx, id.MemberID(5))
	assert.True(t, demotion.SwitchDemoted)
	assert.True(t, demotion.SyncerDemoted)
	assert.Equal(t, id.NoMemberID, pair.Current(ctx))
	assert.Equal(t, id.NoMemberID, pair.SyncerCurrent(ctx))

	demotion = pair.DemoteIfCurrent(ctx, id.MemberID(5))
	assert.False(t, demotion.SwitchDemoted)
	assert.False(t, demotion.SyncerDemoted)

	pair.Switch().CurrentValue.Store(6)
	pair.Syncer().CurrentValue.Store(7)
	demotion = pair.DemoteIfCurrent(ctx, id.MemberID(6))
	assert.True(t, demotion.SwitchDemoted)
	assert.False(t, demotion.SyncerDemoted)
	assert.Equal(t, id.NoMemberID, pair.Current(ctx))
	assert.Equal(t, id.MemberID(7), pair.SyncerCurrent(ctx))
}

func TestWithKeepUnlessDisabledRestoresPredicatesOnFailure(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("switch failed")

	pair, err := switchpair.New(ctx, switchpair.Config{
		InitialValue:     id.MemberID(1),
		SwitchKeepUnless: packetorframecondition.Static(false),
		SyncerKeepUnless: packetorframecondition.Static(true),
	})
	require.NoError(t, err)

	err = pair.WithKeepUnlessDisabled(ctx, func(ctx context.Context) error {
		assert.Nil(t, pair.Switch().GetKeepUnless())
		assert.Nil(t, pair.Syncer().GetKeepUnless())
		return expectedErr
	})
	require.ErrorIs(t, err, expectedErr)

	require.NotNil(t, pair.Switch().GetKeepUnless())
	require.NotNil(t, pair.Syncer().GetKeepUnless())
	assert.False(t, pair.Switch().GetKeepUnless().Match(ctx, packetorframe.InputUnion{}))
	assert.True(t, pair.Syncer().GetKeepUnless().Match(ctx, packetorframe.InputUnion{}))
}
