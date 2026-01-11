// contains_filler_test.go tests the ContainsFiller packet condition.

package condition

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
)

func TestContainsFiller(t *testing.T) {
	ctx := context.Background()
	cond := ContainsFiller(true)

	t.Run("H264_Filler", func(t *testing.T) {
		// NAL type 12 (Filler Data)
		fillerData := []byte{0, 0, 0, 1, 12, 1, 2, 3}
		p := createPacket(t, astiav.CodecIDH264, fillerData)
		require.True(t, cond.Match(ctx, *p), "H.264 filler packet should match")
	})

	t.Run("H264_Data", func(t *testing.T) {
		// NAL type 1 (Non-IDR slice)
		data := []byte{0, 0, 0, 1, 1, 5, 6, 7}
		p := createPacket(t, astiav.CodecIDH264, data)
		require.False(t, cond.Match(ctx, *p), "H.264 data packet should NOT match")
	})

	t.Run("H265_Filler", func(t *testing.T) {
		// NAL type 38 (FD_NUT). (38 << 1) = 76 = 0x4C
		fillerData := []byte{0, 0, 0, 1, 0x4C, 1, 1, 2, 3}
		p := createPacket(t, astiav.CodecIDHevc, fillerData)
		require.True(t, cond.Match(ctx, *p), "H.265 filler packet should match")
	})

	t.Run("H265_Data", func(t *testing.T) {
		// NAL type 1 (TRAIL_R). (1 << 1) = 2
		data := []byte{0, 0, 0, 1, 2, 1, 5, 6, 7}
		p := createPacket(t, astiav.CodecIDHevc, data)
		require.False(t, cond.Match(ctx, *p), "H.265 data packet should NOT match")
	})

	t.Run("Mixed_H264", func(t *testing.T) {
		// NAL type 1 followed by NAL type 12
		mixedData := []byte{0, 0, 0, 1, 1, 1, 2, 3, 0, 0, 0, 1, 12, 4, 5, 6}
		p := createPacket(t, astiav.CodecIDH264, mixedData)
		require.False(t, cond.Match(ctx, *p), "Mixed H.264 packet should NOT match (not purely filler)")
	})
}

func createPacket(t *testing.T, codecID astiav.CodecID, data []byte) *packet.Input {
	pkt := astiav.AllocPacket()
	err := pkt.FromData(data)
	require.NoError(t, err)

	cp := astiav.AllocCodecParameters()
	cp.SetCodecID(codecID)

	si := &packet.StreamInfo{
		CodecParameters: cp,
	}

	p := packet.BuildInput(pkt, si)
	return &p
}
