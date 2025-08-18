package goconv

import (
	"github.com/xaionaro-go/avpipeline/node"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
)

func NodeStatisticsToGRPC(s *node.Statistics) *avpipelinegrpc.NodeStatistics {
	if s == nil {
		return nil
	}
	return ProcessingStatisticsToGRPC(ptr(s.Convert()))
}

func ProcessingStatisticsToGRPC(s *node.ProcessingStatistics) *avpipelinegrpc.NodeStatistics {
	return &avpipelinegrpc.NodeStatistics{
		BytesCountRead:  s.BytesCountRead,
		BytesCountWrote: s.BytesCountWrote,
		Packets:         FramesStatisticsToGRPC(&s.Packets),
		Frames:          FramesStatisticsToGRPC(&s.Frames),
	}
}
