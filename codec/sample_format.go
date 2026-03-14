// sample_format.go provides string-to-astiav.SampleFormat conversion logic.

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

// sampleFormatQuality returns a quality score for the given sample format.
// Higher values indicate better quality/precision.
func sampleFormatQuality(f astiav.SampleFormat) int {
	switch f {
	case astiav.SampleFormatDblp:
		return 6
	case astiav.SampleFormatDbl:
		return 5
	case astiav.SampleFormatFltp:
		return 4
	case astiav.SampleFormatFlt:
		return 3
	case astiav.SampleFormatS32P:
		return 2
	case astiav.SampleFormatS32:
		return 2
	case astiav.SampleFormatS16P:
		return 1
	case astiav.SampleFormatS16:
		return 1
	default:
		return 0
	}
}

// bestSampleFormat picks the highest-quality format from the given list.
func bestSampleFormat(formats []astiav.SampleFormat) astiav.SampleFormat {
	best := formats[0]
	bestScore := sampleFormatQuality(best)
	for _, f := range formats[1:] {
		if score := sampleFormatQuality(f); score > bestScore {
			best = f
			bestScore = score
		}
	}
	return best
}
