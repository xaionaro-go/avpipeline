package types

type ProcessingFramesOrPacketsStatisticsSection struct {
	Unknown uint64 `json:",omitempty"`
	Other   uint64 `json:",omitempty"`
	Video   uint64 `json:",omitempty"`
	Audio   uint64 `json:",omitempty"`
}

type ProcessingFramesOrPacketsStatistics struct {
	Read   ProcessingFramesOrPacketsStatisticsSection
	Missed ProcessingFramesOrPacketsStatisticsSection
	Wrote  ProcessingFramesOrPacketsStatisticsSection
}

type ProcessingStatistics struct {
	BytesCountRead  uint64 `json:",omitempty"`
	BytesCountWrote uint64 `json:",omitempty"`
	Packets         ProcessingFramesOrPacketsStatistics
	Frames          ProcessingFramesOrPacketsStatistics
}
