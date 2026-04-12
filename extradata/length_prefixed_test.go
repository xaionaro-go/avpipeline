package extradata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAnnexB_FourByteStartCode(t *testing.T) {
	assert.True(t, IsAnnexB([]byte{0x00, 0x00, 0x00, 0x01, 0x65}))
}

func TestIsAnnexB_ThreeByteStartCode(t *testing.T) {
	assert.True(t, IsAnnexB([]byte{0x00, 0x00, 0x01, 0x65}))
}

func TestIsAnnexB_LengthPrefixed(t *testing.T) {
	// 4-byte length prefix: length=5
	assert.False(t, IsAnnexB([]byte{0x00, 0x00, 0x00, 0x05, 0x65, 0x88, 0x84, 0x00, 0x2F}))
}

func TestIsAnnexB_TooShort(t *testing.T) {
	assert.False(t, IsAnnexB(nil))
	assert.False(t, IsAnnexB([]byte{}))
	assert.False(t, IsAnnexB([]byte{0x00}))
	assert.False(t, IsAnnexB([]byte{0x00, 0x00}))
}

func TestIsAnnexB_NotStartCode(t *testing.T) {
	assert.False(t, IsAnnexB([]byte{0x00, 0x00, 0x02, 0x01}))
	assert.False(t, IsAnnexB([]byte{0x01, 0x00, 0x00, 0x01}))
}

func TestSplitLengthPrefixed_SingleNALU(t *testing.T) {
	// 4-byte prefix, NAL length = 3
	data := []byte{0x00, 0x00, 0x00, 0x03, 0xAA, 0xBB, 0xCC}
	nalus, err := SplitLengthPrefixed(data, 4)
	require.NoError(t, err)
	require.Len(t, nalus, 1)
	assert.Equal(t, []byte{0xAA, 0xBB, 0xCC}, nalus[0])
}

func TestSplitLengthPrefixed_TwoNALUs(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x02, 0xAA, 0xBB, // NAL 1: length=2
		0x00, 0x00, 0x00, 0x03, 0xCC, 0xDD, 0xEE, // NAL 2: length=3
	}
	nalus, err := SplitLengthPrefixed(data, 4)
	require.NoError(t, err)
	require.Len(t, nalus, 2)
	assert.Equal(t, []byte{0xAA, 0xBB}, nalus[0])
	assert.Equal(t, []byte{0xCC, 0xDD, 0xEE}, nalus[1])
}

func TestSplitLengthPrefixed_TwoBytePrefix(t *testing.T) {
	data := []byte{
		0x00, 0x02, 0xAA, 0xBB, // NAL 1: length=2
		0x00, 0x01, 0xCC, // NAL 2: length=1
	}
	nalus, err := SplitLengthPrefixed(data, 2)
	require.NoError(t, err)
	require.Len(t, nalus, 2)
	assert.Equal(t, []byte{0xAA, 0xBB}, nalus[0])
	assert.Equal(t, []byte{0xCC}, nalus[1])
}

func TestSplitLengthPrefixed_OneBytePrefix(t *testing.T) {
	data := []byte{0x03, 0xAA, 0xBB, 0xCC}
	nalus, err := SplitLengthPrefixed(data, 1)
	require.NoError(t, err)
	require.Len(t, nalus, 1)
	assert.Equal(t, []byte{0xAA, 0xBB, 0xCC}, nalus[0])
}

func TestSplitLengthPrefixed_ThreeBytePrefix(t *testing.T) {
	data := []byte{0x00, 0x00, 0x02, 0xAA, 0xBB}
	nalus, err := SplitLengthPrefixed(data, 3)
	require.NoError(t, err)
	require.Len(t, nalus, 1)
	assert.Equal(t, []byte{0xAA, 0xBB}, nalus[0])
}

func TestSplitLengthPrefixed_TruncatedPrefix(t *testing.T) {
	data := []byte{0x00, 0x00} // only 2 bytes, need 4
	_, err := SplitLengthPrefixed(data, 4)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "truncated")
}

func TestSplitLengthPrefixed_TruncatedNALU(t *testing.T) {
	// Claims length=10 but only 2 bytes follow
	data := []byte{0x00, 0x00, 0x00, 0x0A, 0xAA, 0xBB}
	_, err := SplitLengthPrefixed(data, 4)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds data bounds")
}

func TestSplitLengthPrefixed_ZeroLength(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x00}
	_, err := SplitLengthPrefixed(data, 4)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zero-length")
}

func TestSplitLengthPrefixed_UnsupportedSize(t *testing.T) {
	_, err := SplitLengthPrefixed([]byte{0x00}, 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestSplitLengthPrefixed_Empty(t *testing.T) {
	nalus, err := SplitLengthPrefixed(nil, 4)
	require.NoError(t, err)
	assert.Empty(t, nalus)

	nalus, err = SplitLengthPrefixed([]byte{}, 4)
	require.NoError(t, err)
	assert.Empty(t, nalus)
}

func TestSplitLengthPrefixed_NALUsAreCloned(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x02, 0xAA, 0xBB}
	nalus, err := SplitLengthPrefixed(data, 4)
	require.NoError(t, err)
	require.Len(t, nalus, 1)

	// Mutate original data; NAL slice must be independent.
	data[4] = 0xFF
	assert.Equal(t, byte(0xAA), nalus[0][0])
}

func TestJoinLengthPrefixed_SingleNALU(t *testing.T) {
	nalus := [][]byte{{0xAA, 0xBB, 0xCC}}
	result := JoinLengthPrefixed(nalus, 4)
	expected := []byte{0x00, 0x00, 0x00, 0x03, 0xAA, 0xBB, 0xCC}
	assert.Equal(t, expected, result)
}

func TestJoinLengthPrefixed_TwoNALUs(t *testing.T) {
	nalus := [][]byte{{0xAA, 0xBB}, {0xCC, 0xDD, 0xEE}}
	result := JoinLengthPrefixed(nalus, 4)
	expected := []byte{
		0x00, 0x00, 0x00, 0x02, 0xAA, 0xBB,
		0x00, 0x00, 0x00, 0x03, 0xCC, 0xDD, 0xEE,
	}
	assert.Equal(t, expected, result)
}

func TestJoinLengthPrefixed_TwoBytePrefix(t *testing.T) {
	nalus := [][]byte{{0xAA}}
	result := JoinLengthPrefixed(nalus, 2)
	expected := []byte{0x00, 0x01, 0xAA}
	assert.Equal(t, expected, result)
}

func TestSplitJoinLengthPrefixed_RoundTrip(t *testing.T) {
	original := []byte{
		0x00, 0x00, 0x00, 0x04, 0x40, 0x01, 0x0C, 0x01, // VPS-ish
		0x00, 0x00, 0x00, 0x03, 0x42, 0x01, 0x01, // SPS-ish
		0x00, 0x00, 0x00, 0x02, 0x44, 0x01, // PPS-ish
	}
	nalus, err := SplitLengthPrefixed(original, 4)
	require.NoError(t, err)
	require.Len(t, nalus, 3)

	rejoined := JoinLengthPrefixed(nalus, 4)
	assert.Equal(t, original, rejoined)
}

// TestSplitLengthPrefixed_DataWithFalseStartCodes verifies that
// length-prefixed splitting correctly handles NAL payloads that contain
// byte sequences resembling Annex-B start codes (00 00 00 01).
// This is the exact scenario that caused HEVC NAL corruption.
func TestSplitLengthPrefixed_DataWithFalseStartCodes(t *testing.T) {
	// NAL payload deliberately contains 00 00 00 01 inside it.
	payload := []byte{0x40, 0x01, 0x00, 0x00, 0x00, 0x01, 0xFF, 0xFE}
	data := []byte{0x00, 0x00, 0x00, byte(len(payload))}
	data = append(data, payload...)

	nalus, err := SplitLengthPrefixed(data, 4)
	require.NoError(t, err)
	require.Len(t, nalus, 1)
	assert.Equal(t, payload, nalus[0])
}
