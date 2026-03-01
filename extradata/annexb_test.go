// annexb_test.go provides tests for SplitAnnexB and FindStartCode functions.

package extradata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindStartCode_ThreeByteStartCode(t *testing.T) {
	// 00 00 01
	data := []byte{0x00, 0x00, 0x01, 0x65}
	pos := FindStartCode(data, 0)
	assert.Equal(t, 0, pos)
}

func TestFindStartCode_FourByteStartCode(t *testing.T) {
	// 00 00 00 01
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x65}
	pos := FindStartCode(data, 0)
	assert.Equal(t, 0, pos)
}

func TestFindStartCode_OffsetInMiddle(t *testing.T) {
	// Start code at byte 3
	data := []byte{0xAA, 0xBB, 0xCC, 0x00, 0x00, 0x01, 0x65}
	pos := FindStartCode(data, 0)
	assert.Equal(t, 3, pos)
}

func TestFindStartCode_StartAfterOffset(t *testing.T) {
	// Two start codes; search starting after the first
	data := []byte{0x00, 0x00, 0x01, 0x67, 0x00, 0x00, 0x01, 0x68}
	pos := FindStartCode(data, 3)
	assert.Equal(t, 4, pos)
}

func TestFindStartCode_NoStartCode(t *testing.T) {
	data := []byte{0x00, 0x00, 0x02, 0x65} // 00 00 02 is not a start code
	pos := FindStartCode(data, 0)
	assert.Equal(t, -1, pos)
}

func TestFindStartCode_EmptyInput(t *testing.T) {
	pos := FindStartCode(nil, 0)
	assert.Equal(t, -1, pos)

	pos = FindStartCode([]byte{}, 0)
	assert.Equal(t, -1, pos)
}

func TestFindStartCode_TooShort(t *testing.T) {
	pos := FindStartCode([]byte{0x00, 0x00}, 0)
	assert.Equal(t, -1, pos)
}

func TestFindStartCode_StartOffsetBeyondData(t *testing.T) {
	data := []byte{0x00, 0x00, 0x01, 0x65}
	pos := FindStartCode(data, 10)
	assert.Equal(t, -1, pos)
}

func TestFindStartCode_FourBytePreferredOverThree(t *testing.T) {
	// 00 00 00 01 should be found at the beginning
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x67}
	pos := FindStartCode(data, 0)
	assert.Equal(t, 0, pos)
}

func TestSplitAnnexB_SingleNALU_ThreeByte(t *testing.T) {
	// 00 00 01 <NALU data>
	data := []byte{0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E}
	nalus := SplitAnnexB(data)
	require.Len(t, nalus, 1)
	assert.Equal(t, []byte{0x67, 0x42, 0x00, 0x1E}, nalus[0])
}

func TestSplitAnnexB_SingleNALU_FourByte(t *testing.T) {
	// 00 00 00 01 <NALU data>
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E}
	nalus := SplitAnnexB(data)
	require.Len(t, nalus, 1)
	assert.Equal(t, []byte{0x67, 0x42, 0x00, 0x1E}, nalus[0])
}

func TestSplitAnnexB_MultipleNALUs(t *testing.T) {
	// SPS (00 00 00 01 67 ...) + PPS (00 00 00 01 68 ...)
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, // PPS
	}
	nalus := SplitAnnexB(data)
	require.Len(t, nalus, 2)
	assert.Equal(t, []byte{0x67, 0x42, 0x00, 0x1E}, nalus[0])
	assert.Equal(t, []byte{0x68, 0xCE, 0x38, 0x80}, nalus[1])
}

func TestSplitAnnexB_MixedStartCodes(t *testing.T) {
	// First NALU with 4-byte, second with 3-byte
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, // 4-byte start code + SPS
		0x00, 0x00, 0x01, 0x68, 0xCE,        // 3-byte start code + PPS
	}
	nalus := SplitAnnexB(data)
	require.Len(t, nalus, 2)
	assert.Equal(t, []byte{0x67, 0x42}, nalus[0])
	assert.Equal(t, []byte{0x68, 0xCE}, nalus[1])
}

func TestSplitAnnexB_EmptyInput(t *testing.T) {
	nalus := SplitAnnexB(nil)
	assert.Empty(t, nalus)

	nalus = SplitAnnexB([]byte{})
	assert.Empty(t, nalus)
}

func TestSplitAnnexB_NoStartCodes(t *testing.T) {
	data := []byte{0x67, 0x42, 0x00, 0x1E, 0x68, 0xCE}
	nalus := SplitAnnexB(data)
	assert.Empty(t, nalus)
}

func TestSplitAnnexB_ConsecutiveStartCodes(t *testing.T) {
	// Two consecutive 4-byte start codes with empty NALU between them,
	// then actual data after the second
	data := []byte{
		0x00, 0x00, 0x00, 0x01, // start code 1 (empty NALU follows)
		0x00, 0x00, 0x00, 0x01, // start code 2
		0x67, 0x42,             // actual NALU data
	}
	nalus := SplitAnnexB(data)
	// The first start code has no data before the next start code, so it should be skipped
	require.Len(t, nalus, 1)
	assert.Equal(t, []byte{0x67, 0x42}, nalus[0])
}

func TestSplitAnnexB_ThreeNALUs(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x01, 0x67, 0x42, // SPS
		0x00, 0x00, 0x01, 0x68, 0xCE, // PPS
		0x00, 0x00, 0x01, 0x65, 0x88, 0x84, // IDR
	}
	nalus := SplitAnnexB(data)
	require.Len(t, nalus, 3)
	assert.Equal(t, byte(0x67), nalus[0][0]) // SPS NAL type
	assert.Equal(t, byte(0x68), nalus[1][0]) // PPS NAL type
	assert.Equal(t, byte(0x65), nalus[2][0]) // IDR NAL type
}

func TestSplitAnnexB_LeadingGarbage(t *testing.T) {
	// Random bytes before the first start code
	data := []byte{
		0xAA, 0xBB, 0xCC,              // leading garbage
		0x00, 0x00, 0x01, 0x67, 0x42, // NALU
	}
	nalus := SplitAnnexB(data)
	require.Len(t, nalus, 1)
	assert.Equal(t, []byte{0x67, 0x42}, nalus[0])
}

func TestSplitAnnexB_ResultIsCloned(t *testing.T) {
	data := []byte{0x00, 0x00, 0x01, 0x67, 0x42, 0x00}
	nalus := SplitAnnexB(data)
	require.Len(t, nalus, 1)

	// Modify original data; cloned NALU should be unaffected
	data[3] = 0xFF
	assert.Equal(t, byte(0x67), nalus[0][0])
}

func TestSplitAnnexB_OnlyStartCode(t *testing.T) {
	// Just a start code with no data after it
	data := []byte{0x00, 0x00, 0x01}
	nalus := SplitAnnexB(data)
	assert.Empty(t, nalus)

	data = []byte{0x00, 0x00, 0x00, 0x01}
	nalus = SplitAnnexB(data)
	assert.Empty(t, nalus)
}

func TestSplitAnnexB_SingleByteNALU(t *testing.T) {
	data := []byte{0x00, 0x00, 0x01, 0x09} // AUD with just the type byte
	nalus := SplitAnnexB(data)
	require.Len(t, nalus, 1)
	assert.Equal(t, []byte{0x09}, nalus[0])
}
