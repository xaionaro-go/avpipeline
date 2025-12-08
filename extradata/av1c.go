package extradata

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// AV1C is the parsed AV1CodecConfigurationRecord ("AV1C").
type AV1C struct {
	// Raw header fields (see AV1-ISOBMFF spec §2.3.3 AV1CodecConfigurationRecord).
	Marker  uint8 // 1 bit, SHOULD be 1
	Version uint8 // 7 bits, SHOULD be 1

	SeqProfile   uint8 // 3 bits
	SeqLevelIdx0 uint8 // 5 bits

	SeqTier0 uint8 // 1 bit

	HighBitDepth bool // 1 bit
	TwelveBit    bool // 1 bit
	Monochrome   bool // 1 bit

	ChromaSubsamplingX   uint8 // 1 bit
	ChromaSubsamplingY   uint8 // 1 bit
	ChromaSamplePosition uint8 // 2 bits

	InitialPresentationDelayPresent bool  // 1 bit
	InitialPresentationDelayMinus1  uint8 // 4 bits (valid only if Present==true)

	// Remaining bytes (configOBUs[]).
	// Sequence header OBU + optional metadata OBUs in low-overhead format.
	ConfigOBUs []byte
}

// ParseAV1C parses an AV1CodecConfigurationRecord ("AV1C") from raw extradata.
//
// Returns (rec, nil) if it looks like a valid AV1C; (nil, error) otherwise.
func ParseAV1C(b []byte) (*AV1C, error) {
	// Need at least the fixed 4-byte header.
	if len(b) < 4 {
		return nil, fmt.Errorf("data too short, need at least 4 bytes")
	}

	// Byte 0: marker(1) + version(7)
	marker := (b[0] >> 7) & 0x01
	version := b[0] & 0x7F
	if marker != 1 {
		// Spec says marker SHALL be 1; if not, bail.
		return nil, fmt.Errorf("invalid marker bit (must be 1)")
	}
	// version SHOULD be 1, but we still parse even if not == 1.

	// Byte 1: seq_profile(3) + seq_level_idx_0(5)
	seqProfile := (b[1] >> 5) & 0x07
	seqLevelIdx0 := b[1] & 0x1F

	// Byte 2: seq_tier_0(1), high_bitdepth(1), twelve_bit(1),
	//         monochrome(1), chroma_subsampling_x(1),
	//         chroma_subsampling_y(1), chroma_sample_position(2)
	seqTier0 := (b[2] >> 7) & 0x01
	highBitDepth := ((b[2] >> 6) & 0x01) != 0
	twelveBit := ((b[2] >> 5) & 0x01) != 0
	monochrome := ((b[2] >> 4) & 0x01) != 0
	chromaSubX := (b[2] >> 3) & 0x01
	chromaSubY := (b[2] >> 2) & 0x01
	chromaSamplePos := b[2] & 0x03

	// Byte 3: reserved(3), initial_presentation_delay_present(1),
	//         then either initial_presentation_delay_minus_one(4) or reserved(4).
	initialPresent := ((b[3] >> 4) & 0x01) != 0
	var initialDelayMinus1 uint8
	if initialPresent {
		initialDelayMinus1 = b[3] & 0x0F
	} else {
		// Lower 4 bits SHOULD be 0; we ignore them even if not.
		initialDelayMinus1 = 0
	}

	rec := &AV1C{
		Marker:  marker,
		Version: version,

		SeqProfile:   seqProfile,
		SeqLevelIdx0: seqLevelIdx0,
		SeqTier0:     seqTier0,

		HighBitDepth: highBitDepth,
		TwelveBit:    twelveBit,
		Monochrome:   monochrome,

		ChromaSubsamplingX:   chromaSubX,
		ChromaSubsamplingY:   chromaSubY,
		ChromaSamplePosition: chromaSamplePos,

		InitialPresentationDelayPresent: initialPresent,
		InitialPresentationDelayMinus1:  initialDelayMinus1,

		ConfigOBUs: b[4:], // rest of buffer
	}

	return rec, nil
}

// BitDepth returns the actual bit depth derived from high_bitdepth/twelve_bit/profile,
// following AV1 spec rules.
func (c *AV1C) BitDepth() int {
	// AV1 rules:
	// - if high_bitdepth == 0: 8-bit
	// - else if seq_profile == 2 and twelve_bit == 1: 12-bit
	// - else: 10-bit
	if !c.HighBitDepth {
		return 8
	}
	if c.SeqProfile == 2 && c.TwelveBit {
		return 12
	}
	return 10
}

func (c *AV1C) String() string {
	if c == nil {
		return "<nil AV1CodecConfigurationRecord>"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "AV1CodecConfigurationRecord (AV1C)\n")
	fmt.Fprintf(&sb, "  marker:                        %d\n", c.Marker)
	fmt.Fprintf(&sb, "  version:                       %d\n", c.Version)
	fmt.Fprintf(&sb, "  seq_profile:                   %d\n", c.SeqProfile)
	fmt.Fprintf(&sb, "  seq_level_idx_0:               %d\n", c.SeqLevelIdx0)
	fmt.Fprintf(&sb, "  seq_tier_0:                    %d\n", c.SeqTier0)
	fmt.Fprintf(&sb, "  high_bitdepth:                 %v\n", c.HighBitDepth)
	fmt.Fprintf(&sb, "  twelve_bit:                    %v\n", c.TwelveBit)
	fmt.Fprintf(&sb, "  monochrome:                    %v\n", c.Monochrome)
	fmt.Fprintf(&sb, "  chroma_subsampling_x:          %d\n", c.ChromaSubsamplingX)
	fmt.Fprintf(&sb, "  chroma_subsampling_y:          %d\n", c.ChromaSubsamplingY)
	fmt.Fprintf(&sb, "  chroma_sample_position:        %d\n", c.ChromaSamplePosition)
	fmt.Fprintf(&sb, "  derived_bit_depth:             %d\n", c.BitDepth())
	fmt.Fprintf(&sb, "  initial_presentation_delay_present: %v\n", c.InitialPresentationDelayPresent)
	if c.InitialPresentationDelayPresent {
		fmt.Fprintf(&sb, "  initial_presentation_delay_minus_one: %d\n", c.InitialPresentationDelayMinus1)
	}

	if len(c.ConfigOBUs) > 0 {
		fmt.Fprintf(&sb, "  configOBUs length:             %d bytes\n", len(c.ConfigOBUs))
		preview := c.ConfigOBUs
		if len(preview) > 64 {
			preview = preview[:64]
		}
		sb.WriteString("  configOBUs (first bytes):\n")
		sb.WriteString(indent(hex.Dump(preview), "    "))
	} else {
		fmt.Fprintf(&sb, "  configOBUs:                    <empty>\n")
	}

	return sb.String()
}
