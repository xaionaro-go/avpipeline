package fanout

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

func TestCurrentAndSyncerReadsSyncerFirst(t *testing.T) {
	ctx := context.Background()
	snapshotter := &recordingRouteStateSnapshotter{
		current: id.MemberID(1),
		syncer:  id.MemberID(2),
	}

	current, syncer := currentAndSyncer(ctx, snapshotter)

	require.Equal(t, []string{"syncer", "current"}, snapshotter.calls)
	require.Equal(t, id.MemberID(1), current)
	require.Equal(t, id.MemberID(2), syncer)
	require.NotEqual(t, current, syncer)
}

type recordingRouteStateSnapshotter struct {
	calls   []string
	current id.MemberID
	syncer  id.MemberID
}

func (s *recordingRouteStateSnapshotter) Current(
	context.Context,
) id.MemberID {
	s.calls = append(s.calls, "current")
	return s.current
}

func (s *recordingRouteStateSnapshotter) SyncerCurrent(
	context.Context,
) id.MemberID {
	s.calls = append(s.calls, "syncer")
	return s.syncer
}
