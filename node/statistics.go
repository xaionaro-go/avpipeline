package node

import (
	"github.com/xaionaro-go/avpipeline/node/types"
)

type Statistics = types.Statistics
type FramesOrPacketsStatistics = types.FramesOrPacketsStatistics
type FramesOrPacketsStatisticsSection = types.FramesOrPacketsStatisticsSection
type ProcessingStatistics = types.ProcessingStatistics
type ProcessingFramesOrPacketsStatistics = types.ProcessingFramesOrPacketsStatistics
type ProcessingFramesOrPacketsStatisticsSection = types.ProcessingFramesOrPacketsStatisticsSection

func FromProcessingStatistics(s *ProcessingStatistics) *Statistics {
	return types.FromProcessingStatistics(s)
}
