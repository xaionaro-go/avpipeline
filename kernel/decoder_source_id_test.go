// decoder_source_id_test.go verifies the source-identity logic that
// guards Decoder against reusing a stale av1_cuvid AVCodecContext when a
// publisher takes over an existing route.

package kernel

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
)

// fakeSource implements packet.Source for tests. Only the fields we
// rely on (String + identity by pointer) need to be real.
type fakeSource struct{ name string }

func (s *fakeSource) String() string { return s.name }
func (s *fakeSource) WithOutputFormatContext(ctx context.Context, cb func(*astiav.FormatContext)) {
}

func TestStreamDecoderSourceID_NilIsEmpty(t *testing.T) {
	require.Equal(t, "", streamDecoderSourceID(nil))
}

func TestStreamDecoderSourceID_DifferentInstancesSameName(t *testing.T) {
	a := &fakeSource{name: "rtmp://x/y"}
	b := &fakeSource{name: "rtmp://x/y"}
	idA := streamDecoderSourceID(a)
	idB := streamDecoderSourceID(b)
	require.NotEqual(t, idA, idB,
		"two distinct source instances with the same String() must yield distinct IDs (publisher takeover case)")
}

func TestStreamDecoderSourceID_SameInstanceStable(t *testing.T) {
	a := &fakeSource{name: "rtmp://x/y"}
	require.Equal(t, streamDecoderSourceID(a), streamDecoderSourceID(a))
}
