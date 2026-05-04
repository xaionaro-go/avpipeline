package switchpair

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type Demotion struct {
	SwitchDemoted bool
	SyncerDemoted bool
}

func (p *Pair) DemoteIfCurrent(
	_ context.Context,
	dead id.MemberID,
) Demotion {
	return Demotion{
		SwitchDemoted: p.switcher.CurrentValue.CompareAndSwap(int32(dead), int32(id.NoMemberID)),
		SyncerDemoted: p.syncer.CurrentValue.CompareAndSwap(int32(dead), int32(id.NoMemberID)),
	}
}
