package types

import (
	"fmt"
	"strings"
)

type MuxMode int

const (
	UndefinedMuxMode = MuxMode(iota)
	MuxModeForbid
	MuxModeSameOutputSameTracks
	MuxModeSameOutputDifferentTracks
	MuxModeDifferentOutputsSameTracks
	EndOfPassthroughMode
)

func (m MuxMode) String() string {
	switch m {
	case UndefinedMuxMode:
		return "<undefined>"
	case MuxModeForbid:
		return "forbid"
	case MuxModeSameOutputSameTracks:
		return "same_output_same_tracks"
	case MuxModeSameOutputDifferentTracks:
		return "same_output_different_tracks"
	case MuxModeDifferentOutputsSameTracks:
		return "different_outputs_same_tracks"
	default:
		return fmt.Sprintf("<unknown_mode_%d>", int(m))
	}
}

func MuxModeFromString(s string) MuxMode {
	sanitizeString := func(s string) string {
		return strings.Trim(strings.ToLower(s), " \n\r\t")
	}
	s = sanitizeString(s)
	for i := range EndOfPassthroughMode {
		c := sanitizeString(i.String())
		if s == c {
			return i
		}
	}
	return UndefinedMuxMode
}
