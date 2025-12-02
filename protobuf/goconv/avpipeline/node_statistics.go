package avpipeline

import (
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipelinenolibav"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func NodeCountersToGRPC(
	nc *nodetypes.Counters,
	pc *processortypes.Counters,
) *avpipelinegrpc.NodeCounters {
	return avpipelinenolibav.NodeCountersToGRPC(nc, pc)
}

func CountersSectionToGRPC(
	counters *globaltypes.CountersSection,
) *avpipelinegrpc.NodeCountersSection {
	return avpipelinenolibav.CountersSectionToGRPC(counters)
}

func CountersSubSectionToGRPC(
	counters *globaltypes.CountersSubSection,
) *avpipelinegrpc.NodeCountersSubSection {
	return avpipelinenolibav.CountersSubSectionToGRPC(counters)
}

func CountersItemToGRPC(
	item *globaltypes.CountersItem,
) *avpipelinegrpc.NodeCountersItem {
	return avpipelinenolibav.CountersItemToGRPC(item)
}
