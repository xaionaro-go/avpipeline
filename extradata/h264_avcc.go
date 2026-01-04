// h264_avcc.go provides functionality for parsing and representing H.264 AVCC configuration records.

package extradata

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

type H264AVCC struct {
	Raw           []byte
	Profile       uint8
	Compatibility uint8
	Level         uint8
	NalLengthSize int      // 1..4 bytes
	SPS           [][]byte // raw SPS NALUs (without length prefixes)
	PPS           [][]byte // raw PPS NALUs
	Trailing      []byte   // anything left after PPS list
}

func ParseH264AVCC(b []byte) (*H264AVCC, error) {
	if len(b) < 7 {
		return nil, fmt.Errorf("data too short (%d bytes)", len(b))
	}
	// configurationVersion must be 1
	if b[0] != 1 {
		return nil, fmt.Errorf("unsupported configurationVersion (%d)", b[0])
	}
	// reserved bits must be all 1s for AVCC
	if b[4]&0xFC != 0xFC {
		return nil, fmt.Errorf("invalid reserved bits in byte 4 (0x%02X)", b[4])
	}

	cfg := &H264AVCC{
		Raw:           append([]byte(nil), b...),
		Profile:       b[1],
		Compatibility: b[2],
		Level:         b[3],
		NalLengthSize: int(b[4]&0x03) + 1,
	}

	numSPS := int(b[5] & 0x1F)
	offset := 6

	// SPS list
	for i := 0; i < numSPS && offset+2 <= len(b); i++ {
		if offset+2 > len(b) {
			break
		}
		spsLen := int(binary.BigEndian.Uint16(b[offset:]))
		offset += 2
		if offset >= len(b) {
			break
		}
		if offset+spsLen > len(b) {
			spsLen = len(b) - offset
		}
		if spsLen <= 0 {
			break
		}
		sps := make([]byte, spsLen)
		copy(sps, b[offset:offset+spsLen])
		cfg.SPS = append(cfg.SPS, sps)
		offset += spsLen
	}

	if offset >= len(b) {
		return cfg, nil
	}

	// PPS count
	if offset+1 > len(b) {
		cfg.Trailing = append([]byte(nil), b[offset:]...)
		return cfg, nil
	}
	numPPS := int(b[offset])
	offset++

	for i := 0; i < numPPS && offset+2 <= len(b); i++ {
		if offset+2 > len(b) {
			break
		}
		ppsLen := int(binary.BigEndian.Uint16(b[offset:]))
		offset += 2
		if offset >= len(b) {
			break
		}
		if offset+ppsLen > len(b) {
			ppsLen = len(b) - offset
		}
		if ppsLen <= 0 {
			break
		}
		pps := make([]byte, ppsLen)
		copy(pps, b[offset:offset+ppsLen])
		cfg.PPS = append(cfg.PPS, pps)
		offset += ppsLen
	}

	if offset < len(b) {
		cfg.Trailing = append([]byte(nil), b[offset:]...)
	}

	return cfg, nil
}

func (c *H264AVCC) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "H.264 AVCDecoderConfigurationRecord (AVCC)\n")
	fmt.Fprintf(&sb, "  Profile:         0x%02X\n", c.Profile)
	fmt.Fprintf(&sb, "  Compatibility:   0x%02X\n", c.Compatibility)
	fmt.Fprintf(&sb, "  Level:           0x%02X\n", c.Level)
	fmt.Fprintf(&sb, "  NAL length size: %d bytes\n", c.NalLengthSize)

	fmt.Fprintf(&sb, "  SPS count: %d\n", len(c.SPS))
	for i, sps := range c.SPS {
		preview := len(sps)
		if preview > 16 {
			preview = 16
		}
		fmt.Fprintf(&sb, "    SPS[%d]: %d bytes, first %d bytes: % X\n",
			i, len(sps), preview, sps[:preview])
	}

	fmt.Fprintf(&sb, "  PPS count: %d\n", len(c.PPS))
	for i, pps := range c.PPS {
		preview := len(pps)
		if preview > 16 {
			preview = 16
		}
		fmt.Fprintf(&sb, "    PPS[%d]: %d bytes, first %d bytes: % X\n",
			i, len(pps), preview, pps[:preview])
	}

	if len(c.Trailing) > 0 {
		sb.WriteString("  Trailing bytes:\n")
		sb.WriteString(indent(hex.Dump(c.Trailing), "    "))
	}

	return sb.String()
}
