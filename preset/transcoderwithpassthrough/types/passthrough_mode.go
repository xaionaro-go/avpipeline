package types

import (
	"fmt"
	"strings"
)

type PassthroughMode int

const (
	UndefinedPassthroughMode = PassthroughMode(iota)
	PassthroughModeNever
	PassthroughModeSameTracks
	PassthroughModeSameConnection
	PassthroughModeNextOutput
	EndOfPassthroughMode
)

func (m PassthroughMode) String() string {
	switch m {
	case UndefinedPassthroughMode:
		return "<undefined>"
	case PassthroughModeNever:
		return "never"
	case PassthroughModeSameTracks:
		return "same_tracks"
	case PassthroughModeSameConnection:
		return "same_connection"
	case PassthroughModeNextOutput:
		return "next_output"
	default:
		return fmt.Sprintf("<unknown_mode_%d>", int(m))
	}
}

func PassthroughModeFromString(s string) PassthroughMode {
	sanitizeString := func(s string) string {
		return strings.Trim(strings.ToLower(s), " \n\r\t")
	}
	s = sanitizeString(s)
	for i := PassthroughMode(0); i < EndOfPassthroughMode; i++ {
		c := sanitizeString(i.String())
		if s == c {
			return i
		}
	}
	return UndefinedPassthroughMode
}
