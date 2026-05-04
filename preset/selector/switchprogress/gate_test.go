package switchprogress_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchprogress"
)

func TestStartRequestRejectsConcurrentSwitchAndReleaseIsOneShot(t *testing.T) {
	var gate switchprogress.Gate

	work, err := gate.StartRequest(id.MemberID(2))
	require.NoError(t, err)
	assert.Equal(t, int64(1), gate.InFlight())

	_, err = gate.StartRequest(id.MemberID(3))
	require.Error(t, err)
	assert.ErrorIs(t, err, switchprogress.ErrSwitchInProgress{})

	var inProgress switchprogress.ErrSwitchInProgress
	require.True(t, errors.As(err, &inProgress))
	assert.Equal(t, int64(1), inProgress.ProcN)
	assert.Equal(t, id.MemberID(3), inProgress.To)
	assert.Equal(t, int64(1), gate.InFlight())

	work.Release()
	work.Release()
	assert.Equal(t, int64(0), gate.InFlight())

	nextWork, err := gate.StartRequest(id.MemberID(4))
	require.NoError(t, err)
	nextWork.Release()
	assert.Equal(t, int64(0), gate.InFlight())
}

func TestRequestWorkAsyncReservationsConvergeAndZeroValueNoOps(t *testing.T) {
	var gate switchprogress.Gate
	var zero switchprogress.RequestWork

	zero.Release()
	zeroRelease := zero.ReserveAsyncWork()
	zeroRelease()
	assert.Equal(t, int64(0), gate.InFlight())

	work, err := gate.StartRequest(id.MemberID(4))
	require.NoError(t, err)

	targetRelease := work.ReserveAsyncWork()
	previousPendingTargetRelease := work.ReserveAsyncWork()
	assert.Equal(t, int64(3), gate.InFlight())

	work.Release()
	assert.Equal(t, int64(2), gate.InFlight())

	previousPendingTargetRelease()
	previousPendingTargetRelease()
	assert.Equal(t, int64(1), gate.InFlight())

	targetRelease()
	assert.Equal(t, int64(0), gate.InFlight())
}

func TestMissingTargetPathReleasesOnlyRequestReservation(t *testing.T) {
	var gate switchprogress.Gate

	work, err := gate.StartRequest(id.MemberID(99))
	require.NoError(t, err)
	assert.Equal(t, int64(1), gate.InFlight())

	work.Release()
	assert.Equal(t, int64(0), gate.InFlight())
}

func TestSyncerCycleReservationsAndStaleCycleSupersession(t *testing.T) {
	var gate switchprogress.Gate

	firstCycle := gate.BeginSyncerCycle()
	assert.Equal(t, int64(1), gate.InFlight())

	secondCycle := gate.BeginSyncerCycle()
	assert.Equal(t, int64(1), gate.InFlight())

	assert.False(t, firstCycle.Release())
	assert.Equal(t, int64(1), gate.InFlight())

	assert.True(t, secondCycle.Release())
	assert.Equal(t, int64(0), gate.InFlight())

	gate.BeginSyncerCycle()
	assert.Equal(t, int64(1), gate.InFlight())

	gate.InterruptedSwitch()
	gate.InterruptedSwitch()
	assert.Equal(t, int64(0), gate.InFlight())

	gate.BeginSyncerCycle()
	assert.Equal(t, int64(1), gate.InFlight())

	gate.SyncerReleased()
	gate.SyncerReleased()
	assert.Equal(t, int64(0), gate.InFlight())
}

func TestSupersedeStuckCycleAllowsFreshRequest(t *testing.T) {
	var gate switchprogress.Gate

	gate.BeginSyncerCycle()
	assert.Equal(t, int64(1), gate.InFlight())

	_, err := gate.StartRequest(id.MemberID(5))
	require.ErrorIs(t, err, switchprogress.ErrSwitchInProgress{})
	assert.Equal(t, int64(1), gate.InFlight())

	assert.True(t, gate.SupersedeStuckCycle())
	assert.Equal(t, int64(0), gate.InFlight())
	assert.False(t, gate.SupersedeStuckCycle())

	work, err := gate.StartRequest(id.MemberID(6))
	require.NoError(t, err)
	work.Release()
	assert.Equal(t, int64(0), gate.InFlight())
}

func TestFullLifecycleConvergesToZeroAfterAsyncWorkAndSyncerRelease(t *testing.T) {
	var gate switchprogress.Gate

	work, err := gate.StartRequest(id.MemberID(10))
	require.NoError(t, err)
	targetRelease := work.ReserveAsyncWork()
	previousPendingTargetRelease := work.ReserveAsyncWork()
	gate.BeginSyncerCycle()
	assert.Equal(t, int64(4), gate.InFlight())

	work.Release()
	targetRelease()
	previousPendingTargetRelease()
	assert.Equal(t, int64(1), gate.InFlight())

	gate.SyncerReleased()
	assert.Equal(t, int64(0), gate.InFlight())
}
