package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolution_String(t *testing.T) {
	r := Resolution{Width: 1920, Height: 1080}
	assert.Equal(t, "1920x1080", r.String())
}

func TestResolution_Parse(t *testing.T) {
	var r Resolution
	err := r.Parse("1280x720")
	assert.NoError(t, err)
	assert.Equal(t, uint32(1280), r.Width)
	assert.Equal(t, uint32(720), r.Height)
}

func TestResolution_Parse_Invalid(t *testing.T) {
	var r Resolution
	err := r.Parse("invalid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unable to parse resolution")
}

func TestResolution_Parse_RoundTrip(t *testing.T) {
	original := Resolution{Width: 3840, Height: 2160}
	s := original.String()
	var parsed Resolution
	err := parsed.Parse(s)
	assert.NoError(t, err)
	assert.Equal(t, original, parsed)
}

func TestPixelFormat_String(t *testing.T) {
	assert.Equal(t, "yuv420p", PixelFormatYUV420P.String())
	assert.Equal(t, "nv12", PixelFormatNV12.String())
	assert.Equal(t, "unknown", PixelFormatUnknown.String())
}

func TestName_Constants(t *testing.T) {
	assert.Equal(t, Name("copy"), NameCopy)
	assert.Equal(t, Name("raw"), NameRaw)
}

func TestOptionLatest_Found(t *testing.T) {
	opts := Options{
		OptionOverrideHardwareDeviceType(1),
		OptionOverrideHardwareDeviceType(2),
	}
	v, ok := OptionLatest[OptionOverrideHardwareDeviceType](opts)
	assert.True(t, ok)
	assert.Equal(t, OptionOverrideHardwareDeviceType(2), v) // last one
}

func TestOptionLatest_NotFound(t *testing.T) {
	opts := Options{
		OptionOverrideHardwareDeviceType(1),
	}
	_, ok := OptionLatest[OptionOverrideCustomOptions](opts)
	assert.False(t, ok)
}

func TestOptionLatest_Empty(t *testing.T) {
	opts := Options{}
	_, ok := OptionLatest[OptionOverrideHardwareDeviceType](opts)
	assert.False(t, ok)
}

func TestOptionCommons_Implements(t *testing.T) {
	var o Option = OptionOverrideHardwareDeviceType(0)
	_ = o // compile check
}
