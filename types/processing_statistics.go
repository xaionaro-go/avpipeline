package types

type ProcessingFramesStatistics struct {
	Unknown uint64 `json:",omitempty"`
	Other   uint64 `json:",omitempty"`
	Video   uint64 `json:",omitempty"`
	Audio   uint64 `json:",omitempty"`
}

type ProcessingStatistics struct {
	BytesCountRead  uint64 `json:",omitempty"`
	BytesCountWrote uint64 `json:",omitempty"`
	FramesRead      ProcessingFramesStatistics
	FramesMissed    ProcessingFramesStatistics
	FramesWrote     ProcessingFramesStatistics
}
