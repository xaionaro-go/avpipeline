// intra_only_test.go pins the SSOT case list of IsIntraOnlyCodec so any
// future divergence (intra-only set drift, accidental rename, etc.) flips
// a unit test red instead of silently changing pipeline behaviour at the
// keep-unless / switch-anchor call sites that fan out from this helper.

package codec

import (
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
)

func TestIsIntraOnlyCodec(t *testing.T) {
	for _, tc := range []struct {
		name    string
		codecID astiav.CodecID
		want    bool
	}{
		// Positive cases: every packet self-contained, libav demuxers may
		// legitimately omit AV_PKT_FLAG_KEY on every packet.
		{"rawvideo", astiav.CodecIDRawvideo, true},
		{"wrapped_avframe", astiav.CodecIDWrappedAvframe, true},
		// Negative cases: inter-frame codecs — non-key packets must NOT be
		// treated as switch anchors.
		{"h264", astiav.CodecIDH264, false},
		{"av1", astiav.CodecIDAv1, false},
		{"aac", astiav.CodecIDAac, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsIntraOnlyCodec(tc.codecID))
		})
	}
}
