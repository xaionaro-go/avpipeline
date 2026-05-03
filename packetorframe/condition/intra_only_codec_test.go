// intra_only_codec_test.go covers the IsIntraOnlyCodec Condition: it
// must delegate to codec.IsIntraOnlyCodec for the codec-id decision and
// it must report no-match when GetCodecParameters returns nil (the
// nil guard at intra_only_codec.go).

package condition

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

// makeIntraOnlyTestInput builds an InputUnion backed by a packet whose
// StreamInfo.CodecParameters has the requested codec id. CodecParameters
// is freed via t.Cleanup to keep the test allocation-balanced.
func makeIntraOnlyTestInput(t *testing.T, codecID astiav.CodecID) packetorframe.InputUnion {
	t.Helper()
	cp := astiav.AllocCodecParameters()
	require.NotNil(t, cp)
	t.Cleanup(cp.Free)
	cp.SetCodecID(codecID)
	si := &packetorframetypes.StreamInfo{CodecParameters: cp}
	pktInput := packet.Input{StreamInfo: si}
	return packetorframe.InputUnion{Packet: &pktInput}
}

func TestIsIntraOnlyCodec_Match(t *testing.T) {
	ctx := context.Background()
	cond := IsIntraOnlyCodec{}

	for _, tc := range []struct {
		name    string
		codecID astiav.CodecID
		want    bool
	}{
		// Positive cases: SSOT delegate must return true.
		{"rawvideo", astiav.CodecIDRawvideo, true},
		{"wrapped_avframe", astiav.CodecIDWrappedAvframe, true},
		// Negative case: inter-frame codec must not match (would otherwise
		// let non-key h264 packets pass switch-anchor gates and break
		// keep-unless logic).
		{"h264", astiav.CodecIDH264, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := makeIntraOnlyTestInput(t, tc.codecID)
			assert.Equal(t, tc.want, cond.Match(ctx, in))
		})
	}
}

// TestIsIntraOnlyCodec_Match_NilCodecParameters pins the nil guard at
// intra_only_codec.go: when StreamInfo carries no CodecParameters (e.g.
// pre-NotifyAboutPacketSource probes), Match must return false rather
// than nil-deref or default-true. Falsifier: removing the `if cp == nil`
// guard would crash this test.
func TestIsIntraOnlyCodec_Match_NilCodecParameters(t *testing.T) {
	ctx := context.Background()
	cond := IsIntraOnlyCodec{}

	si := &packetorframetypes.StreamInfo{} // Stream nil, CodecParameters nil
	pktInput := packet.Input{StreamInfo: si}
	in := packetorframe.InputUnion{Packet: &pktInput}

	assert.False(t, cond.Match(ctx, in))
}

func TestIsIntraOnlyCodec_String(t *testing.T) {
	assert.Equal(t, "IsIntraOnlyCodec", IsIntraOnlyCodec{}.String())
}
