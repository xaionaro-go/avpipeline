package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMediaType_String(t *testing.T) {
	tests := []struct {
		mt       MediaType
		expected string
	}{
		{MediaTypeVideo, "video"},
		{MediaTypeAudio, "audio"},
		{MediaTypeData, "data"},
		{MediaTypeSubtitle, "subtitle"},
		{MediaTypeAttachment, "attachment"},
		{MediaTypeUnknown, "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.mt.String())
		})
	}
}

func TestMediaType_String_OutOfRange(t *testing.T) {
	s := MediaType(99).String()
	assert.Contains(t, s, "99")
}

func TestMediaTypes(t *testing.T) {
	types := MediaTypes()
	assert.Contains(t, types, MediaTypeVideo)
	assert.Contains(t, types, MediaTypeAudio)
	assert.Contains(t, types, MediaTypeSubtitle)
	assert.Contains(t, types, MediaTypeData)
	assert.Contains(t, types, MediaTypeUnknown)
	// MediaTypeAttachment and MediaTypeNb are not in MediaTypes()
	assert.NotContains(t, types, MediaTypeAttachment)
	assert.NotContains(t, types, MediaTypeNb)
}
