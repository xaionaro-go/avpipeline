// extra_data_test.go provides tests for the Raw type and its Parse/String/Equal methods.

package extradata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRaw_Equal(t *testing.T) {
	a := Raw{0x01, 0x02, 0x03}
	b := Raw{0x01, 0x02, 0x03}
	c := Raw{0x01, 0x02, 0x04}

	assert.True(t, a.Equal(b))
	assert.False(t, a.Equal(c))
}

func TestRaw_Equal_Empty(t *testing.T) {
	var a Raw
	var b Raw
	assert.True(t, a.Equal(b))
	assert.True(t, Raw{}.Equal(Raw{}))
	assert.True(t, Raw(nil).Equal(Raw(nil)))
}

func TestRaw_Equal_NilVsEmpty(t *testing.T) {
	// bytes.Equal treats nil and empty as equal
	assert.True(t, Raw(nil).Equal(Raw{}))
	assert.True(t, Raw{}.Equal(Raw(nil)))
}

func TestRaw_Equal_DifferentLengths(t *testing.T) {
	a := Raw{0x01, 0x02}
	b := Raw{0x01, 0x02, 0x03}
	assert.False(t, a.Equal(b))
}

func TestRaw_String_Empty(t *testing.T) {
	var r Raw
	assert.Equal(t, "<empty>", r.String())

	r = Raw{}
	assert.Equal(t, "<empty>", r.String())
}

func TestRaw_Parse_Empty(t *testing.T) {
	var r Raw
	assert.Nil(t, r.Parse())

	r = Raw{}
	assert.Nil(t, r.Parse())
}

func TestRaw_Parse_H264AVCC(t *testing.T) {
	// Build valid AVCC data
	sps := []byte{0x67, 0x42, 0x00, 0x1E}
	pps := []byte{0x68, 0xCE, 0x38, 0x80}
	data := buildAVCC(0x42, 0x00, 0x1E, 0x03, [][]byte{sps}, [][]byte{pps})

	r := Raw(data)
	parsed := r.Parse()
	require.NotNil(t, parsed)

	avcc, ok := parsed.(*H264AVCC)
	require.True(t, ok, "expected *H264AVCC, got %T", parsed)
	assert.Equal(t, uint8(0x42), avcc.Profile)
}

func TestRaw_Parse_AACASC(t *testing.T) {
	// AAC-LC, 44100 Hz, stereo
	// This must NOT be parseable as AVCC (version byte != 1)
	data := buildASC(2, 4, 2)

	r := Raw(data)
	parsed := r.Parse()
	require.NotNil(t, parsed)

	asc, ok := parsed.(*AACASC)
	require.True(t, ok, "expected *AACASC, got %T", parsed)
	assert.Equal(t, 2, asc.AudioObjectType)
	assert.Equal(t, 44100, asc.SampleRate)
}

func TestRaw_Parse_H264AnnexB(t *testing.T) {
	// Annex-B data that won't parse as AVCC or AAC
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, // PPS
	}

	r := Raw(data)
	parsed := r.Parse()
	require.NotNil(t, parsed)

	// This might parse as AVCC if the first byte happens to be 0x00 (version != 1),
	// or as AAC if AOT check fails. Since data[0]=0x00, AVCC will fail (version=0),
	// and AAC parsing with data[0]=0x00, data[1]=0x00 gives AOT=0 which is unsupported.
	// So it should fall through to H264AnnexB.
	seq, ok := parsed.(*H264AnnexB)
	require.True(t, ok, "expected *H264AnnexB, got %T", parsed)
	require.Len(t, seq.NALUs, 2)
	assert.Equal(t, H264NalUnitTypeSPS, seq.NALUs[0].Type)
}

func TestRaw_Parse_AV1C(t *testing.T) {
	// Build AV1C data: marker=1, version=1
	// First byte = 0x81 which won't be AVCC (version != 1 for value 0x81... wait, 0x81 = 129 != 1)
	// Also won't be valid AAC since AOT from 0x81 = (0x81<<8 | next) >> 11 -- depends on second byte
	// Let's carefully construct AV1C that won't parse as anything else
	header := buildAV1CHeader(1, 1, 0, 8, 0, false, false, false, 1, 1, 0, false, 0)
	// header[0] = 0x81 (marker=1, version=1)
	// This won't be AVCC: b[0]=0x81 != 1
	// AOT check for AAC: v = (0x81<<8 | header[1]) = 0x8108, aot = (0x8108>>11)&0x1F = 0x10 = 16, which is unsupported
	// So it won't be AAC. H264AnnexB won't find start codes in 4 bytes.
	// So it should parse as AV1C.

	r := Raw(header)
	parsed := r.Parse()
	require.NotNil(t, parsed)

	rec, ok := parsed.(*AV1C)
	require.True(t, ok, "expected *AV1C, got %T", parsed)
	assert.Equal(t, uint8(0), rec.SeqProfile)
}

func TestRaw_Parse_Unknown(t *testing.T) {
	// Data that doesn't match any known format:
	// - Not AVCC: first byte != 1
	// - Not AAC: AOT not in {1-5, 17}
	// - Not H264AnnexB: no start codes
	// - Not AV1C: marker bit != 1
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x00} // All zeros: marker=0 for AV1C, version=0 for AVCC, AOT=0 for AAC, no start codes

	r := Raw(data)
	parsed := r.Parse()
	require.NotNil(t, parsed)

	_, ok := parsed.(Unknown)
	assert.True(t, ok, "expected Unknown, got %T", parsed)
}

func TestRaw_String_Parsed(t *testing.T) {
	// Test that String() calls Parse() and returns the parsed string
	sps := []byte{0x67, 0x42, 0x00, 0x1E}
	pps := []byte{0x68, 0xCE}
	data := buildAVCC(0x42, 0x00, 0x1E, 0x03, [][]byte{sps}, [][]byte{pps})

	r := Raw(data)
	s := r.String()
	assert.Contains(t, s, "H.264 AVCDecoderConfigurationRecord (AVCC)")
}

func TestRaw_String_Unknown(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x00}
	r := Raw(data)
	s := r.String()
	assert.Contains(t, s, "<unknown_type")
}

func TestUnknown_String(t *testing.T) {
	u := Unknown([]byte{0x01, 0x02, 0x03})
	s := u.String()
	assert.Contains(t, s, "<unknown_type")
	assert.Contains(t, s, "3") // length = 3
}

func TestUnknown_String_Empty(t *testing.T) {
	u := Unknown([]byte{})
	s := u.String()
	assert.Contains(t, s, "<unknown_type")
}

func TestRaw_Parse_PriorityOrder(t *testing.T) {
	// Verify that AVCC is tried before AAC, AAC before AnnexB, AnnexB before AV1C.
	// If data is valid AVCC, it should be returned as AVCC even if it might also
	// be parseable as something else.

	// Build valid AVCC
	sps := []byte{0x67, 0x42}
	pps := []byte{0x68, 0xCE}
	avccData := buildAVCC(0x42, 0x00, 0x1E, 0x03, [][]byte{sps}, [][]byte{pps})

	r := Raw(avccData)
	parsed := r.Parse()
	_, ok := parsed.(*H264AVCC)
	assert.True(t, ok, "AVCC data should parse as *H264AVCC, got %T", parsed)
}
