// h264_avcc_test.go provides tests for parsing H.264 AVCC configuration records.

package extradata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildAVCC constructs a minimal valid AVCC byte sequence.
// Fields: version(1)=1, profile, compat, level, reservedFC|nalLenSizeM1,
//
//	reservedE0|numSPS, then SPS entries, numPPS, then PPS entries.
func buildAVCC(profile, compat, level uint8, nalLenSizeM1 uint8, spsList [][]byte, ppsList [][]byte) []byte {
	b := []byte{
		1,       // configurationVersion
		profile, // AVCProfileIndication
		compat,  // profile_compatibility
		level,   // AVCLevelIndication
		0xFC | (nalLenSizeM1 & 0x03), // reserved(6 bits all 1) + lengthSizeMinusOne
		0xE0 | uint8(len(spsList)),    // reserved(3 bits all 1) + numOfSequenceParameterSets
	}
	for _, sps := range spsList {
		spsLen := uint16(len(sps))
		b = append(b, byte(spsLen>>8), byte(spsLen&0xFF))
		b = append(b, sps...)
	}
	b = append(b, uint8(len(ppsList)))
	for _, pps := range ppsList {
		ppsLen := uint16(len(pps))
		b = append(b, byte(ppsLen>>8), byte(ppsLen&0xFF))
		b = append(b, pps...)
	}
	return b
}

func TestParseH264AVCC_ValidMinimal(t *testing.T) {
	// Minimal valid AVCC: High profile, level 4.0, NAL length size=4 (0x03+1)
	sps := []byte{0x67, 0x64, 0x00, 0x28} // Fake SPS starting with SPS NAL type
	pps := []byte{0x68, 0xEE, 0x3C, 0x80} // Fake PPS starting with PPS NAL type
	data := buildAVCC(0x64, 0x00, 0x28, 0x03, [][]byte{sps}, [][]byte{pps})

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)
	require.NotNil(t, avcc)

	assert.Equal(t, uint8(0x64), avcc.Profile)
	assert.Equal(t, uint8(0x00), avcc.Compatibility)
	assert.Equal(t, uint8(0x28), avcc.Level)
	assert.Equal(t, 4, avcc.NalLengthSize)
	require.Len(t, avcc.SPS, 1)
	assert.Equal(t, sps, avcc.SPS[0])
	require.Len(t, avcc.PPS, 1)
	assert.Equal(t, pps, avcc.PPS[0])
	assert.Empty(t, avcc.Trailing)
}

func TestParseH264AVCC_MultipleSPSAndPPS(t *testing.T) {
	sps1 := []byte{0x67, 0x42, 0x00, 0x1E, 0xAB}
	sps2 := []byte{0x67, 0x64, 0x00, 0x28, 0xCD, 0xEF}
	pps1 := []byte{0x68, 0xCE, 0x38}
	pps2 := []byte{0x68, 0xDE, 0x48, 0x01}
	data := buildAVCC(0x42, 0x00, 0x1E, 0x01, [][]byte{sps1, sps2}, [][]byte{pps1, pps2})

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)
	require.NotNil(t, avcc)

	assert.Equal(t, uint8(0x42), avcc.Profile)
	assert.Equal(t, 2, avcc.NalLengthSize) // 0x01 + 1
	require.Len(t, avcc.SPS, 2)
	assert.Equal(t, sps1, avcc.SPS[0])
	assert.Equal(t, sps2, avcc.SPS[1])
	require.Len(t, avcc.PPS, 2)
	assert.Equal(t, pps1, avcc.PPS[0])
	assert.Equal(t, pps2, avcc.PPS[1])
}

func TestParseH264AVCC_NalLengthSizes(t *testing.T) {
	for _, tc := range []struct {
		nalLenSizeM1 uint8
		expected     int
	}{
		{0x00, 1},
		{0x01, 2},
		{0x02, 3},
		{0x03, 4},
	} {
		data := buildAVCC(0x42, 0x00, 0x0A, tc.nalLenSizeM1, [][]byte{{0x67, 0x42}}, [][]byte{{0x68}})
		avcc, err := ParseH264AVCC(data)
		require.NoError(t, err)
		assert.Equal(t, tc.expected, avcc.NalLengthSize)
	}
}

func TestParseH264AVCC_TooShort(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		{},
		{1},
		{1, 0x64},
		{1, 0x64, 0x00},
		{1, 0x64, 0x00, 0x28},
		{1, 0x64, 0x00, 0x28, 0xFF},
		{1, 0x64, 0x00, 0x28, 0xFF, 0xE0},
	} {
		_, err := ParseH264AVCC(data)
		assert.Error(t, err, "expected error for data of len %d", len(data))
	}
}

func TestParseH264AVCC_WrongVersion(t *testing.T) {
	data := buildAVCC(0x64, 0x00, 0x28, 0x03, [][]byte{{0x67}}, [][]byte{{0x68}})
	data[0] = 0 // wrong version
	_, err := ParseH264AVCC(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configurationVersion")

	data[0] = 2 // also wrong
	_, err = ParseH264AVCC(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configurationVersion")
}

func TestParseH264AVCC_InvalidReservedBits(t *testing.T) {
	data := buildAVCC(0x64, 0x00, 0x28, 0x03, [][]byte{{0x67}}, [][]byte{{0x68}})
	// Byte 4 should have upper 6 bits all set (0xFC mask). Clear some.
	data[4] = 0x03 // Reserved bits are 0, only nalLenSizeM1 is set
	_, err := ParseH264AVCC(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved bits")
}

func TestParseH264AVCC_ZeroSPSZeroPPS(t *testing.T) {
	// Build manually: version=1, profile=0x42, compat=0, level=0x0A,
	// 0xFF (reserved + nalLen=4), 0xE0 (reserved + 0 SPS), 0x00 (0 PPS)
	data := []byte{1, 0x42, 0x00, 0x0A, 0xFF, 0xE0, 0x00}

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)
	assert.Empty(t, avcc.SPS)
	assert.Empty(t, avcc.PPS)
}

func TestParseH264AVCC_TrailingBytes(t *testing.T) {
	sps := []byte{0x67, 0x42, 0x00, 0x1E}
	pps := []byte{0x68, 0xCE}
	data := buildAVCC(0x42, 0x00, 0x1E, 0x03, [][]byte{sps}, [][]byte{pps})
	trailing := []byte{0xFD, 0xF8, 0xF8, 0x00} // ext bytes
	data = append(data, trailing...)

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)
	assert.Equal(t, trailing, avcc.Trailing)
}

func TestParseH264AVCC_TruncatedSPS(t *testing.T) {
	// Build a valid header that claims 1 SPS of length 100 but only has 4 bytes of actual data
	data := []byte{
		1,    // version
		0x64, // profile
		0x00, // compat
		0x28, // level
		0xFF, // reserved + nalLenSize=4
		0xE1, // reserved + 1 SPS
		0x00, 0x64, // SPS length = 100 (but we only provide 4 bytes)
		0x67, 0x64, 0x00, 0x28, // 4 bytes of SPS data
	}

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)
	// Should truncate SPS to available data
	require.Len(t, avcc.SPS, 1)
	assert.Equal(t, 4, len(avcc.SPS[0]))
}

func TestParseH264AVCC_TruncatedPPS(t *testing.T) {
	// Valid header with 0 SPS, 1 PPS that claims length 50 but only has 2 bytes
	data := []byte{
		1,    // version
		0x42, // profile
		0x00, // compat
		0x1E, // level
		0xFF, // reserved + nalLenSize=4
		0xE0, // reserved + 0 SPS
		0x01, // 1 PPS
		0x00, 0x32, // PPS length = 50 (only 2 bytes available)
		0x68, 0xCE, // 2 bytes of PPS data
	}

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)
	require.Len(t, avcc.PPS, 1)
	assert.Equal(t, 2, len(avcc.PPS[0]))
}

func TestParseH264AVCC_EndAfterSPS(t *testing.T) {
	// Data ends right after SPS list with no PPS count byte
	sps := []byte{0x67, 0x42}
	data := []byte{
		1,    // version
		0x42, // profile
		0x00, // compat
		0x1E, // level
		0xFF, // reserved + nalLenSize=4
		0xE1, // reserved + 1 SPS
		0x00, byte(len(sps)),
	}
	data = append(data, sps...)

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)
	require.Len(t, avcc.SPS, 1)
	assert.Equal(t, sps, avcc.SPS[0])
	// No PPS expected since data ends here
	assert.Empty(t, avcc.PPS)
}

func TestParseH264AVCC_RawPreserved(t *testing.T) {
	sps := []byte{0x67, 0x64}
	pps := []byte{0x68, 0xEE}
	data := buildAVCC(0x64, 0x00, 0x28, 0x03, [][]byte{sps}, [][]byte{pps})

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)
	assert.Equal(t, data, avcc.Raw)

	// Verify it's a copy, not the same slice
	data[0] = 99
	assert.NotEqual(t, data[0], avcc.Raw[0])
}

func TestH264AVCC_String(t *testing.T) {
	sps := []byte{0x67, 0x64, 0x00, 0x28, 0xAC, 0xD1}
	pps := []byte{0x68, 0xEE, 0x3C, 0x80}
	data := buildAVCC(0x64, 0x00, 0x28, 0x03, [][]byte{sps}, [][]byte{pps})

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)

	s := avcc.String()
	assert.Contains(t, s, "H.264 AVCDecoderConfigurationRecord (AVCC)")
	assert.Contains(t, s, "Profile")
	assert.Contains(t, s, "0x64")
	assert.Contains(t, s, "NAL length size: 4 bytes")
	assert.Contains(t, s, "SPS count: 1")
	assert.Contains(t, s, "PPS count: 1")
}

func TestH264AVCC_StringLongSPS(t *testing.T) {
	// SPS longer than 16 bytes to trigger preview truncation
	sps := make([]byte, 32)
	sps[0] = 0x67
	for i := 1; i < len(sps); i++ {
		sps[i] = byte(i)
	}
	pps := []byte{0x68}
	data := buildAVCC(0x64, 0x00, 0x28, 0x03, [][]byte{sps}, [][]byte{pps})

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)

	s := avcc.String()
	assert.Contains(t, s, "32 bytes")
	assert.Contains(t, s, "first 16 bytes")
}

func TestH264AVCC_StringWithTrailing(t *testing.T) {
	sps := []byte{0x67, 0x42}
	pps := []byte{0x68, 0xCE}
	data := buildAVCC(0x42, 0x00, 0x1E, 0x03, [][]byte{sps}, [][]byte{pps})
	data = append(data, 0xFD, 0xF8, 0x00)

	avcc, err := ParseH264AVCC(data)
	require.NoError(t, err)

	s := avcc.String()
	assert.Contains(t, s, "Trailing bytes")
}
