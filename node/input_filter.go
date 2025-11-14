package node

import (
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
)

func AppendInputPacketFilter(n Abstract, cond packetfiltercondition.Condition) {
	f := n.GetInputPacketFilter()
	if f == nil {
		n.SetInputPacketFilter(cond)
		return
	}

	if f, ok := f.(packetfiltercondition.And); ok {
		n.SetInputPacketFilter(append(f, cond))
		return
	}

	n.SetInputPacketFilter(&packetfiltercondition.And{f, cond})
}

func AppendInputFrameFilter(n Abstract, cond framefiltercondition.Condition) {
	f := n.GetInputFrameFilter()
	if f == nil {
		n.SetInputFrameFilter(cond)
		return
	}

	if f, ok := f.(framefiltercondition.And); ok {
		n.SetInputFrameFilter(append(f, cond))
		return
	}

	n.SetInputFrameFilter(&framefiltercondition.And{f, cond})
}
