package switchpair

import (
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type Config struct {
	InitialValue     id.MemberID
	SwitchKeepUnless packetorframecondition.Condition
	SyncerKeepUnless packetorframecondition.Condition
	SwitchFlags      barrierstategetter.SwitchFlags
	SyncerFlags      barrierstategetter.SwitchFlags
	Hooks            Hooks
}
