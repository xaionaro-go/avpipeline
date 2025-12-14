package codec

import (
	"fmt"
	"strings"

	"github.com/asticode/go-astiav"
)

func sampleFormatFromString(s string) (astiav.SampleFormat, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "u8":
		return astiav.SampleFormatU8, nil
	case "u8p":
		return astiav.SampleFormatU8P, nil
	case "s16":
		return astiav.SampleFormatS16, nil
	case "s16p":
		return astiav.SampleFormatS16P, nil
	case "s32":
		return astiav.SampleFormatS32, nil
	case "s32p":
		return astiav.SampleFormatS32P, nil
	case "s64":
		return astiav.SampleFormatS64, nil
	case "s64p":
		return astiav.SampleFormatS64P, nil
	case "flt":
		return astiav.SampleFormatFlt, nil
	case "fltp":
		return astiav.SampleFormatFltp, nil
	case "dbl":
		return astiav.SampleFormatDbl, nil
	case "dblp":
		return astiav.SampleFormatDblp, nil
	}

	return astiav.SampleFormatNone, fmt.Errorf("unsupported sample format '%s'", s)
}
