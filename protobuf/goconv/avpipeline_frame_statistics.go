package goconv

import (
	"github.com/xaionaro-go/avpipeline/node"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
)

func FramesStatisticsToGRPC(fs *node.ProcessingFramesStatistics) *avpipelinegrpc.FramesStatistics {
	if fs == nil {
		return nil
	}
	return &avpipelinegrpc.FramesStatistics{
		Unknown: fs.Unknown,
		Other:   fs.Other,
		Video:   fs.Video,
		Audio:   fs.Audio,
	}
}
