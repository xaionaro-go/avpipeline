// aac_asc_test.go provides tests for parsing AAC AudioSpecificConfig (ASC).

package extradata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildASC constructs a 2-byte AAC AudioSpecificConfig.
// Layout: [5 bits AOT][4 bits sampleRateIdx][4 bits channelCfg][3 bits padding]
func buildASC(aot, sfi, ch int) []byte {
	v := uint16(aot&0x1F)<<11 | uint16(sfi&0x0F)<<7 | uint16(ch&0x0F)<<3
	return []byte{byte(v >> 8), byte(v & 0xFF)}
}

func TestParseAACASC_AACLC_44100_Stereo(t *testing.T) {
	// AAC-LC (AOT=2), 44100 Hz (index=4), stereo (ch=2)
	data := buildASC(2, 4, 2)

	asc, err := ParseAACASC(data)
	require.NoError(t, err)
	require.NotNil(t, asc)

	assert.Equal(t, 2, asc.AudioObjectType)
	assert.Equal(t, 4, asc.SampleRateIndex)
	assert.Equal(t, 44100, asc.SampleRate)
	assert.Equal(t, 2, asc.ChannelConfig)
}

func TestParseAACASC_AACLC_48000_Mono(t *testing.T) {
	// AAC-LC (AOT=2), 48000 Hz (index=3), mono (ch=1)
	data := buildASC(2, 3, 1)

	asc, err := ParseAACASC(data)
	require.NoError(t, err)

	assert.Equal(t, 2, asc.AudioObjectType)
	assert.Equal(t, 3, asc.SampleRateIndex)
	assert.Equal(t, 48000, asc.SampleRate)
	assert.Equal(t, 1, asc.ChannelConfig)
}

func TestParseAACASC_AACMain(t *testing.T) {
	// AAC Main (AOT=1), 96000 Hz (index=0), 5.1 (ch=6)
	data := buildASC(1, 0, 6)

	asc, err := ParseAACASC(data)
	require.NoError(t, err)

	assert.Equal(t, 1, asc.AudioObjectType)
	assert.Equal(t, 0, asc.SampleRateIndex)
	assert.Equal(t, 96000, asc.SampleRate)
	assert.Equal(t, 6, asc.ChannelConfig)
}

func TestParseAACASC_AACSSR(t *testing.T) {
	// AAC SSR (AOT=3), 22050 Hz (index=7), 3 channels
	data := buildASC(3, 7, 3)

	asc, err := ParseAACASC(data)
	require.NoError(t, err)

	assert.Equal(t, 3, asc.AudioObjectType)
	assert.Equal(t, 7, asc.SampleRateIndex)
	assert.Equal(t, 22050, asc.SampleRate)
	assert.Equal(t, 3, asc.ChannelConfig)
}

func TestParseAACASC_AACLTP(t *testing.T) {
	// AAC LTP (AOT=4), 32000 Hz (index=5), 4 channels
	data := buildASC(4, 5, 4)

	asc, err := ParseAACASC(data)
	require.NoError(t, err)
	assert.Equal(t, 4, asc.AudioObjectType)
	assert.Equal(t, 32000, asc.SampleRate)
}

func TestParseAACASC_SBR(t *testing.T) {
	// SBR / HE-AAC (AOT=5), 24000 Hz (index=6), stereo
	data := buildASC(5, 6, 2)

	asc, err := ParseAACASC(data)
	require.NoError(t, err)
	assert.Equal(t, 5, asc.AudioObjectType)
	assert.Equal(t, 24000, asc.SampleRate)
}

func TestParseAACASC_ERAAC(t *testing.T) {
	// ER AAC LC (AOT=17), 16000 Hz (index=8), mono
	data := buildASC(17, 8, 1)

	asc, err := ParseAACASC(data)
	require.NoError(t, err)
	assert.Equal(t, 17, asc.AudioObjectType)
	assert.Equal(t, 16000, asc.SampleRate)
}

func TestParseAACASC_ExplicitSampleRate(t *testing.T) {
	// AOT=2, sampleRateIndex=0x0F (explicit), ch=2
	data := buildASC(2, 0x0F, 2)

	asc, err := ParseAACASC(data)
	require.NoError(t, err)

	assert.Equal(t, 0x0F, asc.SampleRateIndex)
	assert.Equal(t, 0, asc.SampleRate) // Explicit means no lookup
}

func TestParseAACASC_AllSampleRates(t *testing.T) {
	expectedRates := []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
	for idx, expectedRate := range expectedRates {
		data := buildASC(2, idx, 2)
		asc, err := ParseAACASC(data)
		require.NoError(t, err, "index %d", idx)
		assert.Equal(t, expectedRate, asc.SampleRate, "index %d", idx)
	}
}

func TestParseAACASC_TooShort(t *testing.T) {
	_, err := ParseAACASC(nil)
	assert.Error(t, err)

	_, err = ParseAACASC([]byte{})
	assert.Error(t, err)

	_, err = ParseAACASC([]byte{0x12})
	assert.Error(t, err)
}

func TestParseAACASC_UnsupportedAOT(t *testing.T) {
	// AOT=6 is not in the accepted set (1-5, 17)
	data := buildASC(6, 4, 2)
	_, err := ParseAACASC(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported audio object type")

	// AOT=0
	data = buildASC(0, 4, 2)
	_, err = ParseAACASC(data)
	assert.Error(t, err)

	// AOT=16
	data = buildASC(16, 4, 2)
	_, err = ParseAACASC(data)
	assert.Error(t, err)

	// AOT=31 (max 5-bit value)
	data = buildASC(31, 4, 2)
	_, err = ParseAACASC(data)
	assert.Error(t, err)
}

func TestParseAACASC_InvalidSampleRateIndex(t *testing.T) {
	// Indexes 13 and 14 are reserved (not 0x0F which means explicit)
	data := buildASC(2, 13, 2)
	_, err := ParseAACASC(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid sample rate index")

	data = buildASC(2, 14, 2)
	_, err = ParseAACASC(data)
	assert.Error(t, err)
}

func TestParseAACASC_RawPreserved(t *testing.T) {
	data := buildASC(2, 4, 2)
	asc, err := ParseAACASC(data)
	require.NoError(t, err)

	assert.Equal(t, data, asc.Raw)
	// Verify it's a copy
	data[0] = 0xFF
	assert.NotEqual(t, data[0], asc.Raw[0])
}

func TestParseAACASC_LongerInput(t *testing.T) {
	// Extra bytes after the first 2 should be fine (preserved in Raw)
	data := append(buildASC(2, 4, 2), 0x56, 0xE5, 0x00)

	asc, err := ParseAACASC(data)
	require.NoError(t, err)
	assert.Equal(t, 2, asc.AudioObjectType)
	assert.Len(t, asc.Raw, 5) // original 2 + 3 extra
}

func TestAACASC_String_AACLC(t *testing.T) {
	data := buildASC(2, 4, 2)
	asc, err := ParseAACASC(data)
	require.NoError(t, err)

	s := asc.String()
	assert.Contains(t, s, "MPEG-4 AAC AudioSpecificConfig (ASC)")
	assert.Contains(t, s, "AAC LC (Low Complexity)")
	assert.Contains(t, s, "44100 Hz")
	assert.Contains(t, s, "2 channels (stereo)")
}

func TestAACASC_String_AllObjectTypes(t *testing.T) {
	tests := []struct {
		aot      int
		expected string
	}{
		{1, "AAC Main"},
		{2, "AAC LC (Low Complexity)"},
		{3, "AAC SSR"},
		{4, "AAC LTP"},
		{5, "SBR (HE-AAC extension)"},
		{17, "ER AAC LC"},
	}
	for _, tc := range tests {
		data := buildASC(tc.aot, 4, 2)
		asc, err := ParseAACASC(data)
		require.NoError(t, err)
		assert.Contains(t, asc.String(), tc.expected)
	}
}

func TestAACASC_String_ChannelConfigs(t *testing.T) {
	tests := []struct {
		ch       int
		expected string
	}{
		{0, "Defined in bitstream (PCE)"},
		{1, "1 channel (mono)"},
		{2, "2 channels (stereo)"},
		{3, "3 channels"},
		{4, "4 channels"},
		{5, "5 channels"},
		{6, "6 channels (5.1)"},
		{7, "Reserved/unknown"},
	}
	for _, tc := range tests {
		data := buildASC(2, 4, tc.ch)
		asc, err := ParseAACASC(data)
		require.NoError(t, err)
		assert.Contains(t, asc.String(), tc.expected, "ch=%d", tc.ch)
	}
}

func TestAACASC_String_ExplicitSampleRate(t *testing.T) {
	data := buildASC(2, 0x0F, 2)
	asc, err := ParseAACASC(data)
	require.NoError(t, err)

	s := asc.String()
	assert.Contains(t, s, "explicit in bitstream")
}

func TestAACASC_String_ReservedSampleRateIndex(t *testing.T) {
	// SampleRateIndex=12 is valid (7350 Hz) and 13 is reserved
	// index 12 returns 7350 which is >0, so we get the Hz path
	data := buildASC(2, 12, 2)
	asc, err := ParseAACASC(data)
	require.NoError(t, err)
	assert.Contains(t, asc.String(), "7350 Hz")
}
