package goconv

import (
	"github.com/xaionaro-go/avpipeline/node"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
)

func FramesStatisticsToGRPC(fs *node.ProcessingFramesOrPacketsStatistics) *avpipelinegrpc.FramesOrPacketsStatistics {
	if fs == nil {
		return nil
	}
	return &avpipelinegrpc.FramesOrPacketsStatistics{
		Read:   FramesStatisticsSectionToGRPC(&fs.Read),
		Missed: FramesStatisticsSectionToGRPC(&fs.Missed),
		Wrote:  FramesStatisticsSectionToGRPC(&fs.Wrote),
	}
}

func FramesStatisticsSectionToGRPC(
	stats *node.ProcessingFramesOrPacketsStatisticsSection,
) *avpipelinegrpc.FramesOrPacketsStatisticsSection {
	if stats == nil {
		return nil
	}
	return &avpipelinegrpc.FramesOrPacketsStatisticsSection{
		Unknown: stats.Unknown,
		Other:   stats.Other,
		Video:   stats.Video,
		Audio:   stats.Audio,
	}
}
