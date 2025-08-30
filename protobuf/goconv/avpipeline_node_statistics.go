package goconv

import (
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	"github.com/xaionaro-go/avpipeline/types"
)

func NodeCountersToGRPC(
	nc *nodetypes.Counters,
	pc *processortypes.Counters,
) *avpipelinegrpc.NodeCounters {
	if nc == nil || pc == nil {
		return nil
	}
	return &avpipelinegrpc.NodeCounters{
		Packets: CountersSectionToGRPC(&nc.Packets, &pc.Packets),
		Frames:  CountersSectionToGRPC(&nc.Frames, &pc.Frames),
	}
}

func CountersSectionToGRPC(
	nc *nodetypes.CountersSection,
	pc *processortypes.CountersSection,
) *avpipelinegrpc.NodeCountersSection {
	if nc == nil {
		return nil
	}
	return &avpipelinegrpc.NodeCountersSection{
		Received:  CountersSubsectionToGRPC(&nc.Received),
		Processed: CountersSubsectionToGRPC(&pc.Processed),
		Missed:    CountersSubsectionToGRPC(&nc.Missed),
		Generated: CountersSubsectionToGRPC(&pc.Generated),
		Sent:      CountersSubsectionToGRPC(&nc.Sent),
	}
}

func CountersSubsectionToGRPC(
	counters *types.CountersSubSection,
) *avpipelinegrpc.NodeCountersSubsection {
	if counters == nil {
		return nil
	}
	return &avpipelinegrpc.NodeCountersSubsection{
		Unknown: CountersItemToGRPC(counters.Unknown),
		Other:   CountersItemToGRPC(counters.Other),
		Video:   CountersItemToGRPC(counters.Video),
		Audio:   CountersItemToGRPC(counters.Audio),
	}
}

func CountersItemToGRPC(item *types.CountersItem) *avpipelinegrpc.NodeCountersItem {
	if item == nil {
		return nil
	}
	return &avpipelinegrpc.NodeCountersItem{
		Count: item.Count.Load(),
		Bytes: item.Bytes.Load(),
	}
}
