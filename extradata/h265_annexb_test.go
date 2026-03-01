// h265_annexb_test.go provides tests for parsing H.265 Annex-B sequences.

package extradata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// h265NALUHeader builds a 2-byte H.265 NALU header from a NAL unit type.
// H.265 NALU header is 2 bytes: forbidden_zero_bit(1) + nal_unit_type(6) + nuh_layer_id(6) + nuh_temporal_id_plus1(3)
// The type is in bits [1:6] of the first byte, i.e., (byte0 & 0x7E) >> 1
func h265NALUHeader(naluType H265NalUnitType) []byte {
	b0 := byte(naluType) << 1 // forbidden_zero_bit=0, type in bits 1-6
	b1 := byte(0x01)          // nuh_layer_id=0, nuh_temporal_id_plus1=1
	return []byte{b0, b1}
}

func TestParseH265AnnexB_VPS_SPS_PPS(t *testing.T) {
	vps := append(h265NALUHeader(H265NalUnitTypeVPS), 0x01, 0x02, 0x03)
	sps := append(h265NALUHeader(H265NalUnitTypeSPS), 0x04, 0x05, 0x06)
	pps := append(h265NALUHeader(H265NalUnitTypePPS), 0x07, 0x08)

	data := []byte{}
	data = append(data, 0x00, 0x00, 0x00, 0x01) // start code
	data = append(data, vps...)
	data = append(data, 0x00, 0x00, 0x00, 0x01) // start code
	data = append(data, sps...)
	data = append(data, 0x00, 0x00, 0x00, 0x01) // start code
	data = append(data, pps...)

	seq, err := ParseH265AnnexB(data)
	require.NoError(t, err)
	require.NotNil(t, seq)
	require.Len(t, seq.NALUs, 3)

	assert.Equal(t, H265NalUnitTypeVPS, seq.NALUs[0].Type)
	assert.Equal(t, H265NalUnitTypeSPS, seq.NALUs[1].Type)
	assert.Equal(t, H265NalUnitTypePPS, seq.NALUs[2].Type)
}

func TestParseH265AnnexB_IDR(t *testing.T) {
	idr := append(h265NALUHeader(H265NalUnitTypeIDRWISCL), 0x88, 0x84, 0x00)

	data := []byte{0x00, 0x00, 0x00, 0x01}
	data = append(data, idr...)

	seq, err := ParseH265AnnexB(data)
	require.NoError(t, err)
	require.Len(t, seq.NALUs, 1)

	assert.Equal(t, H265NalUnitTypeIDRWISCL, seq.NALUs[0].Type)
}

func TestParseH265AnnexB_IDRNLP(t *testing.T) {
	idr := append(h265NALUHeader(H265NalUnitTypeIDRNLP), 0x88, 0x84)

	data := []byte{0x00, 0x00, 0x01}
	data = append(data, idr...)

	seq, err := ParseH265AnnexB(data)
	require.NoError(t, err)
	require.Len(t, seq.NALUs, 1)

	assert.Equal(t, H265NalUnitTypeIDRNLP, seq.NALUs[0].Type)
}

func TestParseH265AnnexB_TrailR(t *testing.T) {
	trail := append(h265NALUHeader(H265NalUnitTypeTrailR), 0x9A, 0x24)

	data := []byte{0x00, 0x00, 0x00, 0x01}
	data = append(data, trail...)

	seq, err := ParseH265AnnexB(data)
	require.NoError(t, err)
	require.Len(t, seq.NALUs, 1)

	assert.Equal(t, H265NalUnitTypeTrailR, seq.NALUs[0].Type)
}

func TestParseH265AnnexB_SingleByteNALU_Skipped(t *testing.T) {
	// H.265 requires at least 2 bytes per NALU (2-byte header).
	// A single-byte NALU should be skipped.
	data := []byte{
		0x00, 0x00, 0x01, 0x40, // Only 1 byte NALU (0x40)
	}

	_, err := ParseH265AnnexB(data)
	// This should fail because the single-byte NALU is skipped
	// and no valid NALUs remain
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no valid NAL units parsed")
}

func TestParseH265AnnexB_SingleByteNALU_WithValidAfter(t *testing.T) {
	// First NALU is single-byte (skipped), second is valid VPS
	vps := append(h265NALUHeader(H265NalUnitTypeVPS), 0x01, 0x02)

	data := []byte{
		0x00, 0x00, 0x01, 0x40, // Single-byte NALU (skipped)
		0x00, 0x00, 0x00, 0x01, // start code for VPS
	}
	data = append(data, vps...)

	seq, err := ParseH265AnnexB(data)
	require.NoError(t, err)
	require.Len(t, seq.NALUs, 1)
	assert.Equal(t, H265NalUnitTypeVPS, seq.NALUs[0].Type)
}

func TestParseH265AnnexB_NoNALUs(t *testing.T) {
	data := []byte{0x40, 0x01, 0x02}
	_, err := ParseH265AnnexB(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no NAL units found")
}

func TestParseH265AnnexB_EmptyInput(t *testing.T) {
	_, err := ParseH265AnnexB(nil)
	assert.Error(t, err)

	_, err = ParseH265AnnexB([]byte{})
	assert.Error(t, err)
}

func TestParseH265AnnexB_RawPreserved(t *testing.T) {
	vps := append(h265NALUHeader(H265NalUnitTypeVPS), 0x01, 0x02)
	data := append([]byte{0x00, 0x00, 0x00, 0x01}, vps...)

	seq, err := ParseH265AnnexB(data)
	require.NoError(t, err)

	assert.Equal(t, data, seq.Raw)
	// Verify it's a copy
	data[4] = 0xFF
	assert.NotEqual(t, data[4], seq.Raw[4])
}

func TestParseH265AnnexB_NALURawIsCloned(t *testing.T) {
	vps := append(h265NALUHeader(H265NalUnitTypeVPS), 0x01, 0x02)
	data := append([]byte{0x00, 0x00, 0x00, 0x01}, vps...)

	seq, err := ParseH265AnnexB(data)
	require.NoError(t, err)

	origByte := seq.NALUs[0].Raw[0]
	data[4] = 0xFF
	assert.Equal(t, origByte, seq.NALUs[0].Raw[0])
}

func TestParseH265AnnexB_AllNamedTypes(t *testing.T) {
	tests := []struct {
		naluType H265NalUnitType
		expected string
	}{
		{H265NalUnitTypeTrailN, "TRAIL_N/TRAIL_R"},
		{H265NalUnitTypeTrailR, "TRAIL_N/TRAIL_R"},
		{H265NalUnitTypeTSAN, "TSA_N/TSA_R"},
		{H265NalUnitTypeTSAR, "TSA_N/TSA_R"},
		{H265NalUnitTypeSTSAN, "STSA_N/STSA_R"},
		{H265NalUnitTypeSTSAR, "STSA_N/STSA_R"},
		{H265NalUnitTypeRADLN, "RADL_N/RADL_R"},
		{H265NalUnitTypeRADLR, "RADL_N/RADL_R"},
		{H265NalUnitTypeRASLN, "RASL_N/RASL_R"},
		{H265NalUnitTypeRASLR, "RASL_N/RASL_R"},
		{H265NalUnitTypeBLAWLP, "BLA"},
		{H265NalUnitTypeBLAWRADL, "BLA"},
		{H265NalUnitTypeBLANLP, "BLA"},
		{H265NalUnitTypeIDRWISCL, "IDR"},
		{H265NalUnitTypeIDRNLP, "IDR"},
		{H265NalUnitTypeCRAWNUT, "CRA"},
		{H265NalUnitTypeVPS, "VPS"},
		{H265NalUnitTypeSPS, "SPS"},
		{H265NalUnitTypePPS, "PPS"},
		{H265NalUnitTypeAUD, "AUD"},
		{H265NalUnitTypeEOS, "EOS"},
		{H265NalUnitTypeEOB, "EOB"},
		{H265NalUnitTypeFD, "FD"},
		{H265NalUnitTypePrefixSEI, "SEI"},
		{H265NalUnitTypeSuffixSEI, "SEI"},
	}

	for _, tc := range tests {
		nalu := append(h265NALUHeader(tc.naluType), 0x01, 0x02)
		data := append([]byte{0x00, 0x00, 0x00, 0x01}, nalu...)

		seq, err := ParseH265AnnexB(data)
		require.NoError(t, err, "type=%d", tc.naluType)

		s := seq.String()
		assert.Contains(t, s, tc.expected, "type=%d", tc.naluType)
	}
}

func TestParseH265AnnexB_OtherType(t *testing.T) {
	// Use NAL unit type 10 which is between RASL_R(9) and BLA_W_LP(16)
	// and falls into the default "other" case
	nalu := append(h265NALUHeader(H265NalUnitType(10)), 0x01, 0x02)
	data := append([]byte{0x00, 0x00, 0x00, 0x01}, nalu...)

	seq, err := ParseH265AnnexB(data)
	require.NoError(t, err)

	s := seq.String()
	assert.Contains(t, s, "other")
}

func TestH265AnnexB_String(t *testing.T) {
	vps := append(h265NALUHeader(H265NalUnitTypeVPS), 0x01, 0x02, 0x03)
	sps := append(h265NALUHeader(H265NalUnitTypeSPS), 0x04, 0x05)

	data := []byte{}
	data = append(data, 0x00, 0x00, 0x00, 0x01)
	data = append(data, vps...)
	data = append(data, 0x00, 0x00, 0x00, 0x01)
	data = append(data, sps...)

	seq, err := ParseH265AnnexB(data)
	require.NoError(t, err)

	s := seq.String()
	assert.Contains(t, s, "H.265 Annex-B sequence (2 NAL units)")
	assert.Contains(t, s, "VPS")
	assert.Contains(t, s, "SPS")
}

func TestH265AnnexB_String_LongNALU(t *testing.T) {
	// Create a NALU longer than 16 bytes to trigger preview truncation
	nalu := make([]byte, 32)
	header := h265NALUHeader(H265NalUnitTypeIDRWISCL)
	copy(nalu, header)
	for i := 2; i < len(nalu); i++ {
		nalu[i] = byte(i)
	}
	data := append([]byte{0x00, 0x00, 0x00, 0x01}, nalu...)

	seq, err := ParseH265AnnexB(data)
	require.NoError(t, err)

	s := seq.String()
	assert.Contains(t, s, "len=32")
	assert.Contains(t, s, "first 16 bytes")
}
