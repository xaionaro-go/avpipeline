// h264_annexb_test.go provides tests for parsing H.264 Annex-B sequences.

package extradata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseH264AnnexB_SPS_PPS(t *testing.T) {
	// SPS NAL type = 7 (0x67 = forbidden_zero_bit(0) + NRI(3) + type(7))
	// PPS NAL type = 8 (0x68 = forbidden_zero_bit(0) + NRI(3) + type(8))
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, 0xAB, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80,       // PPS
	}

	seq, err := ParseH264AnnexB(data)
	require.NoError(t, err)
	require.NotNil(t, seq)

	require.Len(t, seq.NALUs, 2)

	assert.Equal(t, H264NalUnitTypeSPS, seq.NALUs[0].Type)
	assert.Equal(t, 3, seq.NALUs[0].NRI) // (0x67 >> 5) & 0x03 = 3
	assert.Equal(t, []byte{0x67, 0x42, 0x00, 0x1E, 0xAB}, seq.NALUs[0].Raw)

	assert.Equal(t, H264NalUnitTypePPS, seq.NALUs[1].Type)
	assert.Equal(t, 3, seq.NALUs[1].NRI)
	assert.Equal(t, []byte{0x68, 0xCE, 0x38, 0x80}, seq.NALUs[1].Raw)
}

func TestParseH264AnnexB_IDRSlice(t *testing.T) {
	// IDR NAL type = 5 (0x65 = NRI=3, type=5)
	data := []byte{
		0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00, 0x33,
	}

	seq, err := ParseH264AnnexB(data)
	require.NoError(t, err)
	require.Len(t, seq.NALUs, 1)

	assert.Equal(t, H264NalUnitTypeIDR, seq.NALUs[0].Type)
	assert.Equal(t, 3, seq.NALUs[0].NRI)
}

func TestParseH264AnnexB_NonIDRSlice(t *testing.T) {
	// Non-IDR NAL type = 1 (0x41 = NRI=2, type=1)
	data := []byte{
		0x00, 0x00, 0x01, 0x41, 0x9A, 0x24,
	}

	seq, err := ParseH264AnnexB(data)
	require.NoError(t, err)
	require.Len(t, seq.NALUs, 1)

	assert.Equal(t, H264NalUnitTypeNonIDR, seq.NALUs[0].Type)
	assert.Equal(t, 2, seq.NALUs[0].NRI)
}

func TestParseH264AnnexB_SEI(t *testing.T) {
	// SEI NAL type = 6 (0x06 = NRI=0, type=6)
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x06, 0x05, 0x04, 0x48, 0x44,
	}

	seq, err := ParseH264AnnexB(data)
	require.NoError(t, err)
	require.Len(t, seq.NALUs, 1)

	assert.Equal(t, H264NalUnitTypeSEI, seq.NALUs[0].Type)
	assert.Equal(t, 0, seq.NALUs[0].NRI)
}

func TestParseH264AnnexB_AUD(t *testing.T) {
	// AUD NAL type = 9 (0x09 = NRI=0, type=9)
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x09, 0xF0,
	}

	seq, err := ParseH264AnnexB(data)
	require.NoError(t, err)
	require.Len(t, seq.NALUs, 1)

	assert.Equal(t, H264NalUnitTypeAUD, seq.NALUs[0].Type)
}

func TestParseH264AnnexB_SPS_PPS_IDR(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, // PPS
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00, // IDR
	}

	seq, err := ParseH264AnnexB(data)
	require.NoError(t, err)
	require.Len(t, seq.NALUs, 3)

	assert.Equal(t, H264NalUnitTypeSPS, seq.NALUs[0].Type)
	assert.Equal(t, H264NalUnitTypePPS, seq.NALUs[1].Type)
	assert.Equal(t, H264NalUnitTypeIDR, seq.NALUs[2].Type)
}

func TestParseH264AnnexB_NoNALUs(t *testing.T) {
	// No start codes
	data := []byte{0x67, 0x42, 0x00, 0x1E}
	_, err := ParseH264AnnexB(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no NAL units found")
}

func TestParseH264AnnexB_EmptyInput(t *testing.T) {
	_, err := ParseH264AnnexB(nil)
	assert.Error(t, err)

	_, err = ParseH264AnnexB([]byte{})
	assert.Error(t, err)
}

func TestParseH264AnnexB_RawPreserved(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x01, 0x67, 0x42,
	}

	seq, err := ParseH264AnnexB(data)
	require.NoError(t, err)

	assert.Equal(t, data, seq.Raw)
	// Verify it's a copy
	data[3] = 0xFF
	assert.NotEqual(t, data[3], seq.Raw[3])
}

func TestParseH264AnnexB_NALURawIsCloned(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x01, 0x67, 0x42,
	}

	seq, err := ParseH264AnnexB(data)
	require.NoError(t, err)

	// Modify original
	data[3] = 0xFF
	assert.Equal(t, byte(0x67), seq.NALUs[0].Raw[0])
}

func TestParseH264AnnexB_NRIValues(t *testing.T) {
	// Test different NRI values
	// NRI is bits 5-6 of the NALU header byte: (h >> 5) & 0x03
	tests := []struct {
		headerByte byte
		nri        int
		nalType    H264NalUnitType
	}{
		{0x67, 3, H264NalUnitTypeSPS},  // 0110 0111 -> NRI=3, type=7
		{0x47, 2, H264NalUnitTypeSPS},  // 0100 0111 -> NRI=2, type=7
		{0x27, 1, H264NalUnitTypeSPS},  // 0010 0111 -> NRI=1, type=7
		{0x07, 0, H264NalUnitTypeSPS},  // 0000 0111 -> NRI=0, type=7
		{0x65, 3, H264NalUnitTypeIDR},  // 0110 0101 -> NRI=3, type=5
		{0x01, 0, H264NalUnitTypeNonIDR}, // 0000 0001 -> NRI=0, type=1
	}
	for _, tc := range tests {
		data := []byte{0x00, 0x00, 0x01, tc.headerByte, 0x42}
		seq, err := ParseH264AnnexB(data)
		require.NoError(t, err)
		require.Len(t, seq.NALUs, 1)
		assert.Equal(t, tc.nri, seq.NALUs[0].NRI, "header=0x%02X", tc.headerByte)
		assert.Equal(t, tc.nalType, seq.NALUs[0].Type, "header=0x%02X", tc.headerByte)
	}
}

func TestH264AnnexB_String(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, // PPS
	}

	seq, err := ParseH264AnnexB(data)
	require.NoError(t, err)

	s := seq.String()
	assert.Contains(t, s, "H.264 Annex-B sequence (2 NAL units)")
	assert.Contains(t, s, "type=7 (SPS)")
	assert.Contains(t, s, "type=8 (PPS)")
	assert.Contains(t, s, "NRI=3")
}

func TestH264AnnexB_String_LongNALU(t *testing.T) {
	// Create a NALU longer than 16 bytes to trigger preview truncation
	nalu := make([]byte, 32)
	nalu[0] = 0x65 // IDR
	for i := 1; i < len(nalu); i++ {
		nalu[i] = byte(i)
	}
	data := append([]byte{0x00, 0x00, 0x00, 0x01}, nalu...)

	seq, err := ParseH264AnnexB(data)
	require.NoError(t, err)

	s := seq.String()
	assert.Contains(t, s, "len=32")
	assert.Contains(t, s, "first 16 bytes")
}

func TestH264AnnexB_StringNalTypeNames(t *testing.T) {
	// Verify all named types through String() output
	tests := []struct {
		headerByte byte
		expected   string
	}{
		{0x01, "non-IDR slice"},      // type 1
		{0x02, "slice data A"},       // type 2
		{0x03, "slice data B"},       // type 3
		{0x04, "slice data C"},       // type 4
		{0x65, "IDR slice"},          // type 5
		{0x06, "SEI"},               // type 6
		{0x67, "SPS"},               // type 7
		{0x68, "PPS"},               // type 8
		{0x09, "AUD"},               // type 9
		{0x0A, "end of sequence"},    // type 10
		{0x0B, "end of stream"},     // type 11
		{0x0C, "filler"},            // type 12
		{0x0F, "reserved/unknown"},  // type 15
	}
	for _, tc := range tests {
		data := []byte{0x00, 0x00, 0x01, tc.headerByte, 0x42}
		seq, err := ParseH264AnnexB(data)
		require.NoError(t, err, "header=0x%02X", tc.headerByte)
		s := seq.String()
		assert.Contains(t, s, tc.expected, "header=0x%02X", tc.headerByte)
	}
}
