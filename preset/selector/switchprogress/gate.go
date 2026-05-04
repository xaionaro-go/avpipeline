package switchprogress

import (
	"sync/atomic"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type Gate struct {
	inFlight             atomic.Int64
	syncerGeneration     atomic.Uint64
	nextSyncerGeneration atomic.Uint64
}

func (g *Gate) StartRequest(
	to id.MemberID,
) (RequestWork, error) {
	inFlight := g.inFlight.Add(1)
	if inFlight == 1 {
		return RequestWork{
			gate:  g,
			token: &releaseToken{},
		}, nil
	}

	g.inFlight.Add(-1)
	return RequestWork{}, ErrSwitchInProgress{
		ProcN: inFlight - 1,
		To:    to,
	}
}

func (g *Gate) SupersedeStuckCycle() bool {
	return g.releaseSyncerCycle(0)
}

func (g *Gate) InterruptedSwitch() {
	g.releaseSyncerCycle(0)
}

func (g *Gate) SyncerReleased() {
	g.releaseSyncerCycle(0)
}

func (g *Gate) InFlight() int64 {
	return g.inFlight.Load()
}
