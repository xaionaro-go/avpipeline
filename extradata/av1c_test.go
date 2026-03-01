// av1c_test.go provides tests for parsing AV1CodecConfigurationRecord (AV1C).

package extradata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildAV1C constructs a 4-byte AV1C header from the given parameters.
// Byte 0: marker(1) + version(7)
// Byte 1: seq_profile(3) + seq_level_idx_0(5)
// Byte 2: seq_tier_0(1) + high_bitdepth(1) + twelve_bit(1) + monochrome(1)
//
//	+ chroma_subsampling_x(1) + chroma_subsampling_y(1) + chroma_sample_position(2)
//
// Byte 3: reserved(3) + initial_presentation_delay_present(1)
//
//	+ initial_presentation_delay_minus_one(4) OR reserved(4)
func buildAV1CHeader(
	marker, version uint8,
	seqProfile, seqLevelIdx0 uint8,
	seqTier0 uint8,
	highBitDepth, twelveBit, monochrome bool,
	chromaSubX, chromaSubY, chromaSamplePos uint8,
	initialPresent bool, initialDelayM1 uint8,
) []byte {
	b0 := (marker&0x01)<<7 | (version & 0x7F)
	b1 := (seqProfile&0x07)<<5 | (seqLevelIdx0 & 0x1F)

	var b2 uint8
	b2 |= (seqTier0 & 0x01) << 7
	if highBitDepth {
		b2 |= 1 << 6
	}
	if twelveBit {
		b2 |= 1 << 5
	}
	if monochrome {
		b2 |= 1 << 4
	}
	b2 |= (chromaSubX & 0x01) << 3
	b2 |= (chromaSubY & 0x01) << 2
	b2 |= chromaSamplePos & 0x03

	var b3 uint8
	b3 = 0xE0 // reserved 3 bits all set
	if initialPresent {
		b3 |= 1 << 4
		b3 |= initialDelayM1 & 0x0F
	}

	return []byte{b0, b1, b2, b3}
}

func TestParseAV1C_ValidBasic(t *testing.T) {
	// marker=1, version=1, profile=0, level=8, tier=0, 8-bit, 4:2:0
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)

	rec, err := ParseAV1C(header)
	require.NoError(t, err)
	require.NotNil(t, rec)

	assert.Equal(t, uint8(1), rec.Marker)
	assert.Equal(t, uint8(1), rec.Version)
	assert.Equal(t, uint8(0), rec.SeqProfile)
	assert.Equal(t, uint8(8), rec.SeqLevelIdx0)
	assert.Equal(t, uint8(0), rec.SeqTier0)
	assert.False(t, rec.HighBitDepth)
	assert.False(t, rec.TwelveBit)
	assert.False(t, rec.Monochrome)
	assert.Equal(t, uint8(1), rec.ChromaSubsamplingX)
	assert.Equal(t, uint8(1), rec.ChromaSubsamplingY)
	assert.Equal(t, uint8(0), rec.ChromaSamplePosition)
	assert.False(t, rec.InitialPresentationDelayPresent)
	assert.Equal(t, uint8(0), rec.InitialPresentationDelayMinus1)
	assert.Empty(t, rec.ConfigOBUs)
}

func TestParseAV1C_Profile1_10bit(t *testing.T) {
	// marker=1, version=1, profile=1, level=12, tier=1, 10-bit (high_bitdepth=true, twelve_bit=false)
	header := buildAV1CHeader(1, 1, 1, 12, 1, true, false, false, 0, 0, 1, false, 0)

	rec, err := ParseAV1C(header)
	require.NoError(t, err)

	assert.Equal(t, uint8(1), rec.SeqProfile)
	assert.Equal(t, uint8(12), rec.SeqLevelIdx0)
	assert.Equal(t, uint8(1), rec.SeqTier0)
	assert.True(t, rec.HighBitDepth)
	assert.False(t, rec.TwelveBit)
	assert.Equal(t, 10, rec.BitDepth())
}

func TestParseAV1C_Profile2_12bit(t *testing.T) {
	// marker=1, version=1, profile=2, level=5, tier=0, 12-bit (high_bitdepth=true, twelve_bit=true, profile=2)
	header := buildAV1CHeader(1, 1, 2, 5, 0, true, true, false, 1, 1, 2, false, 0)

	rec, err := ParseAV1C(header)
	require.NoError(t, err)

	assert.Equal(t, uint8(2), rec.SeqProfile)
	assert.True(t, rec.HighBitDepth)
	assert.True(t, rec.TwelveBit)
	assert.Equal(t, 12, rec.BitDepth())
}

func TestParseAV1C_8bit(t *testing.T) {
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)

	rec, err := ParseAV1C(header)
	require.NoError(t, err)
	assert.Equal(t, 8, rec.BitDepth())
}

func TestParseAV1C_10bit_Profile2_TwelveBitFalse(t *testing.T) {
	// Profile=2 but twelve_bit=false -> 10-bit
	header := buildAV1CHeader(1, 1, 2, 8, 0, true, false, false, 1, 1, 0, false, 0)

	rec, err := ParseAV1C(header)
	require.NoError(t, err)
	assert.Equal(t, 10, rec.BitDepth())
}

func TestParseAV1C_10bit_NonProfile2_TwelveBitTrue(t *testing.T) {
	// Profile=1 with twelve_bit=true (but not profile 2, so still 10-bit)
	header := buildAV1CHeader(1, 1, 1, 8, 0, true, true, false, 0, 0, 0, false, 0)

	rec, err := ParseAV1C(header)
	require.NoError(t, err)
	assert.Equal(t, 10, rec.BitDepth()) // only profile 2 + twelve_bit gives 12
}

func TestParseAV1C_Monochrome(t *testing.T) {
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, true, 0, 0, 0, false, 0)

	rec, err := ParseAV1C(header)
	require.NoError(t, err)
	assert.True(t, rec.Monochrome)
}

func TestParseAV1C_InitialPresentationDelay(t *testing.T) {
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, true, 9)

	rec, err := ParseAV1C(header)
	require.NoError(t, err)
	assert.True(t, rec.InitialPresentationDelayPresent)
	assert.Equal(t, uint8(9), rec.InitialPresentationDelayMinus1)
}

func TestParseAV1C_InitialPresentationDelayAbsent(t *testing.T) {
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)

	rec, err := ParseAV1C(header)
	require.NoError(t, err)
	assert.False(t, rec.InitialPresentationDelayPresent)
	assert.Equal(t, uint8(0), rec.InitialPresentationDelayMinus1)
}

func TestParseAV1C_WithConfigOBUs(t *testing.T) {
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)
	configOBUs := []byte{0x0A, 0x0B, 0x00, 0x00, 0x00, 0x04, 0x47, 0x7E, 0x1A, 0xFF}
	data := append(header, configOBUs...)

	rec, err := ParseAV1C(data)
	require.NoError(t, err)
	assert.Equal(t, configOBUs, rec.ConfigOBUs)
}

func TestParseAV1C_TooShort(t *testing.T) {
	_, err := ParseAV1C(nil)
	assert.Error(t, err)

	_, err = ParseAV1C([]byte{})
	assert.Error(t, err)

	_, err = ParseAV1C([]byte{0x81})
	assert.Error(t, err)

	_, err = ParseAV1C([]byte{0x81, 0x08})
	assert.Error(t, err)

	_, err = ParseAV1C([]byte{0x81, 0x08, 0x00})
	assert.Error(t, err)
}

func TestParseAV1C_InvalidMarker(t *testing.T) {
	// marker=0 instead of 1
	header := buildAV1CHeader(0, 1, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)

	_, err := ParseAV1C(header)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid marker bit")
}

func TestParseAV1C_NonStandardVersion(t *testing.T) {
	// Version=2 (non-standard but still parsed - spec says SHOULD be 1)
	header := buildAV1CHeader(1, 2, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)

	rec, err := ParseAV1C(header)
	require.NoError(t, err)
	assert.Equal(t, uint8(2), rec.Version)
}

func TestParseAV1C_ChromaSamplePositions(t *testing.T) {
	for pos := uint8(0); pos <= 3; pos++ {
		header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, pos, false, 0)
		rec, err := ParseAV1C(header)
		require.NoError(t, err)
		assert.Equal(t, pos, rec.ChromaSamplePosition, "pos=%d", pos)
	}
}

func TestParseAV1C_AllSeqProfiles(t *testing.T) {
	for profile := uint8(0); profile <= 2; profile++ {
		header := buildAV1CHeader(1, 1, profile, 8, 0, false, false, false, 1, 1, 0, false, 0)
		rec, err := ParseAV1C(header)
		require.NoError(t, err)
		assert.Equal(t, profile, rec.SeqProfile, "profile=%d", profile)
	}
}

func TestAV1C_String_Basic(t *testing.T) {
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)
	rec, err := ParseAV1C(header)
	require.NoError(t, err)

	s := rec.String()
	assert.Contains(t, s, "AV1CodecConfigurationRecord (AV1C)")
	assert.Contains(t, s, "marker:                        1")
	assert.Contains(t, s, "version:                       1")
	assert.Contains(t, s, "seq_profile:                   0")
	assert.Contains(t, s, "seq_level_idx_0:               8")
	assert.Contains(t, s, "derived_bit_depth:             8")
	assert.Contains(t, s, "configOBUs:                    <empty>")
}

func TestAV1C_String_WithConfigOBUs(t *testing.T) {
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)
	configOBUs := []byte{0x0A, 0x0B, 0x0C, 0x0D, 0x0E}
	data := append(header, configOBUs...)

	rec, err := ParseAV1C(data)
	require.NoError(t, err)

	s := rec.String()
	assert.Contains(t, s, "configOBUs length:")
	assert.Contains(t, s, "5 bytes")
	assert.Contains(t, s, "configOBUs (first bytes):")
}

func TestAV1C_String_WithInitialDelay(t *testing.T) {
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, true, 3)
	rec, err := ParseAV1C(header)
	require.NoError(t, err)

	s := rec.String()
	assert.Contains(t, s, "initial_presentation_delay_present: true")
	assert.Contains(t, s, "initial_presentation_delay_minus_one: 3")
}

func TestAV1C_String_WithoutInitialDelay(t *testing.T) {
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)
	rec, err := ParseAV1C(header)
	require.NoError(t, err)

	s := rec.String()
	assert.Contains(t, s, "initial_presentation_delay_present: false")
	assert.NotContains(t, s, "initial_presentation_delay_minus_one")
}

func TestAV1C_String_Nil(t *testing.T) {
	var rec *AV1C
	s := rec.String()
	assert.Equal(t, "<nil AV1CodecConfigurationRecord>", s)
}

func TestAV1C_String_LongConfigOBUs(t *testing.T) {
	// ConfigOBUs longer than 64 bytes to trigger preview truncation
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)
	configOBUs := make([]byte, 100)
	for i := range configOBUs {
		configOBUs[i] = byte(i)
	}
	data := append(header, configOBUs...)

	rec, err := ParseAV1C(data)
	require.NoError(t, err)

	s := rec.String()
	assert.Contains(t, s, "100 bytes")
	assert.Contains(t, s, "configOBUs (first bytes):")
}

func TestAV1C_BitDepth_AllCombinations(t *testing.T) {
	tests := []struct {
		name         string
		profile      uint8
		highBitDepth bool
		twelveBit    bool
		expected     int
	}{
		{"8-bit: highBitDepth=false", 0, false, false, 8},
		{"8-bit: highBitDepth=false, twelveBit=true (ignored)", 0, false, true, 8},
		{"10-bit: profile=0, highBitDepth=true", 0, true, false, 10},
		{"10-bit: profile=1, highBitDepth=true", 1, true, false, 10},
		{"10-bit: profile=2, highBitDepth=true, twelveBit=false", 2, true, false, 10},
		{"12-bit: profile=2, highBitDepth=true, twelveBit=true", 2, true, true, 12},
		{"10-bit: profile=1, highBitDepth=true, twelveBit=true (not profile 2)", 1, true, true, 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &AV1C{
				SeqProfile:   tc.profile,
				HighBitDepth: tc.highBitDepth,
				TwelveBit:    tc.twelveBit,
			}
			assert.Equal(t, tc.expected, rec.BitDepth())
		})
	}
}
