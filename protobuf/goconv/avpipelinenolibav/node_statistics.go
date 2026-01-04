// node_statistics.go provides conversion functions for node statistics between Protobuf and Go.

package avpipelinenolibav

import (
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func NodeCountersToGRPC(
	nc *nodetypes.Counters,
	pc *processortypes.Counters,
) *avpipelinegrpc.NodeCounters {
	if nc == nil || pc == nil {
		return nil
	}
	return &avpipelinegrpc.NodeCounters{
		Received:  CountersSectionToGRPC(&nc.Received),
		Processed: CountersSectionToGRPC(&pc.Processed),
		Missed:    CountersSectionToGRPC(&nc.Missed),
		Generated: CountersSectionToGRPC(&pc.Generated),
		Omitted:   CountersSectionToGRPC(&pc.Omitted),
		Sent:      CountersSectionToGRPC(&nc.Sent),
	}
}

func CountersSectionToGRPC(
	counters *globaltypes.CountersSection,
) *avpipelinegrpc.NodeCountersSection {
	if counters == nil {
		return nil
	}
	return &avpipelinegrpc.NodeCountersSection{
		Packets: CountersSubSectionToGRPC(&counters.Packets),
		Frames:  CountersSubSectionToGRPC(&counters.Frames),
	}
}

func CountersSubSectionToGRPC(
	counters *globaltypes.CountersSubSection,
) *avpipelinegrpc.NodeCountersSubSection {
	if counters == nil {
		return nil
	}
	return &avpipelinegrpc.NodeCountersSubSection{
		Unknown: CountersItemToGRPC(counters.Unknown),
		Other:   CountersItemToGRPC(counters.Other),
		Video:   CountersItemToGRPC(counters.Video),
		Audio:   CountersItemToGRPC(counters.Audio),
	}
}

func CountersItemToGRPC(
	item *globaltypes.CountersItem,
) *avpipelinegrpc.NodeCountersItem {
	if item == nil {
		return nil
	}
	return &avpipelinegrpc.NodeCountersItem{
		Count: item.Count.Load(),
		Bytes: item.Bytes.Load(),
	}
}
