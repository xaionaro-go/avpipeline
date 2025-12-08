package extradata

import (
	"fmt"
	"strings"
)

type AACASC struct {
	Raw             []byte
	AudioObjectType int
	SampleRateIndex int
	SampleRate      int
	ChannelConfig   int
}

func ParseAACASC(b []byte) (*AACASC, error) {
	if len(b) < 2 {
		return nil, fmt.Errorf("input too short (%d bytes)", len(b))
	}

	// [5 bits AOT][4 bits sampleRateIdx][4 bits channelCfg]...
	v := uint16(b[0])<<8 | uint16(b[1])
	aot := int((v >> 11) & 0x1F)
	sfi := int((v >> 7) & 0x0F)
	ch := int((v >> 3) & 0x0F)

	// Tiny heuristic to not mis-detect random data:
	if !(aot >= 1 && aot <= 4 || aot == 5 || aot == 17) {
		return nil, fmt.Errorf("unsupported audio object type %d", aot)
	}
	if sfi > 12 && sfi != 0x0F { // 13–14 reserved, 15=explicit
		return nil, fmt.Errorf("invalid sample rate index %d", sfi)
	}

	asc := &AACASC{
		Raw:             append([]byte(nil), b...),
		AudioObjectType: aot,
		SampleRateIndex: sfi,
		ChannelConfig:   ch,
	}
	if sfi != 0x0F {
		asc.SampleRate = aacSampleRateFromIndex(sfi)
	}
	return asc, nil
}

func (a *AACASC) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "MPEG-4 AAC AudioSpecificConfig (ASC)\n")
	fmt.Fprintf(&sb, "  Audio Object Type: %d (%s)\n",
		a.AudioObjectType, aacObjectTypeName(a.AudioObjectType))

	if a.SampleRateIndex == 0x0F {
		fmt.Fprintf(&sb, "  Sample rate index: 0xF (explicit in bitstream)\n")
	} else if a.SampleRate > 0 {
		fmt.Fprintf(&sb, "  Sample rate index: %d (%d Hz)\n",
			a.SampleRateIndex, a.SampleRate)
	} else {
		fmt.Fprintf(&sb, "  Sample rate index: %d (reserved/unknown)\n",
			a.SampleRateIndex)
	}

	fmt.Fprintf(&sb, "  Channel configuration: %d (%s)\n",
		a.ChannelConfig, aacChannelConfigName(a.ChannelConfig))

	fmt.Fprintf(&sb, "  Raw bytes: % X\n", a.Raw)
	return sb.String()
}

func aacObjectTypeName(aot int) string {
	switch aot {
	case 1:
		return "AAC Main"
	case 2:
		return "AAC LC (Low Complexity)"
	case 3:
		return "AAC SSR"
	case 4:
		return "AAC LTP"
	case 5:
		return "SBR (HE-AAC extension)"
	case 17:
		return "ER AAC LC"
	default:
		return "Reserved/unknown"
	}
}

func aacSampleRateFromIndex(idx int) int {
	rates := []int{
		96000,
		88200,
		64000,
		48000,
		44100,
		32000,
		24000,
		22050,
		16000,
		12000,
		11025,
		8000,
		7350,
	}
	if idx >= 0 && idx < len(rates) {
		return rates[idx]
	}
	return 0
}

func aacChannelConfigName(cfg int) string {
	switch cfg {
	case 0:
		return "Defined in bitstream (PCE)"
	case 1:
		return "1 channel (mono)"
	case 2:
		return "2 channels (stereo)"
	case 3:
		return "3 channels"
	case 4:
		return "4 channels"
	case 5:
		return "5 channels"
	case 6:
		return "6 channels (5.1)"
	default:
		return "Reserved/unknown"
	}
}
