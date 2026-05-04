package fanout

type Mode uint8

const (
	ModeForbid Mode = iota + 1
	ModeSameOutputSameTracks
	ModeSameOutputDifferentTracks
	ModeDifferentOutputsSameTracks
	ModeDifferentOutputsSameTracksSplitAV
)

func (m Mode) String() string {
	switch m {
	case ModeForbid:
		return "forbid"
	case ModeSameOutputSameTracks:
		return "same-output-same-tracks"
	case ModeSameOutputDifferentTracks:
		return "same-output-different-tracks"
	case ModeDifferentOutputsSameTracks:
		return "different-outputs-same-tracks"
	case ModeDifferentOutputsSameTracksSplitAV:
		return "different-outputs-same-tracks-split-av"
	default:
		return "unknown"
	}
}
