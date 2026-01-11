// filter_test.go tests the removefiller packet filter.

package removefiller

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
)

func TestRemoveFiller(t *testing.T) {
	ctx := context.Background()
	filter := New()

	t.Run("H264_Filler", func(t *testing.T) {
		// NAL type 12 (Filler Data)
		fillerData := []byte{0, 0, 0, 1, 12, 1, 2, 3}
		p := createPacket(t, astiav.CodecIDH264, fillerData)
		require.False(t, filter.Match(ctx, *p), "H.264 filler packet should be DROPPED (Match returns false)")
	})

	t.Run("H264_Data", func(t *testing.T) {
		// NAL type 1 (Non-IDR slice)
		data := []byte{0, 0, 0, 1, 1, 5, 6, 7}
		p := createPacket(t, astiav.CodecIDH264, data)
		require.True(t, filter.Match(ctx, *p), "H.264 data packet should be KEPT (Match returns true)")
		require.Equal(t, data, p.Data())
	})

	t.Run("H264_Mixed", func(t *testing.T) {
		// SPS (7), Filler (12), PPS (8)
		sps := []byte{0, 0, 0, 1, 7, 1, 2, 3}
		filler := []byte{0, 0, 0, 1, 12, 4, 5, 6}
		pps := []byte{0, 0, 0, 1, 8, 7, 8, 9}
		mixedData := append(append([]byte(nil), sps...), append(filler, pps...)...)
		p := createPacket(t, astiav.CodecIDH264, mixedData)
		p.SetPTS(100)
		p.SetDTS(90)
		p.SetDuration(10)

		require.True(t, filter.Match(ctx, *p), "Mixed H.264 packet should be KEPT")
		expectedData := append(append([]byte(nil), sps...), pps...)
		require.Equal(t, expectedData, p.Data())
		require.Equal(t, int64(100), p.GetPTS())
		require.Equal(t, int64(90), p.GetDTS())
		require.Equal(t, int64(10), p.GetDuration())
	})

	t.Run("H265_Filler", func(t *testing.T) {
		// NAL type 38 (FD_NUT). (38 << 1) = 76 = 0x4C
		fillerData := []byte{0, 0, 0, 1, 0x4C, 1, 1, 2, 3}
		p := createPacket(t, astiav.CodecIDHevc, fillerData)
		require.False(t, filter.Match(ctx, *p), "H.265 filler packet should be DROPPED (Match returns false)")
	})

	t.Run("H265_Mixed", func(t *testing.T) {
		// VPS (32), SPS (33), Filler (38), PPS (34)
		vps := []byte{0, 0, 0, 1, 32 << 1, 0, 1}
		sps := []byte{0, 0, 0, 1, 33 << 1, 0, 2}
		filler := []byte{0, 0, 0, 1, 38 << 1, 0, 3}
		pps := []byte{0, 0, 0, 1, 34 << 1, 0, 4}
		mixedData := append(append(append([]byte(nil), vps...), sps...), append(filler, pps...)...)
		p := createPacket(t, astiav.CodecIDHevc, mixedData)
		p.SetPTS(200)
		p.SetDTS(190)
		p.SetDuration(10)

		require.True(t, filter.Match(ctx, *p), "Mixed H.265 packet should be KEPT")
		expectedData := append(append(append([]byte(nil), vps...), sps...), pps...)
		require.Equal(t, expectedData, p.Data())
		require.Equal(t, int64(200), p.GetPTS())
		require.Equal(t, int64(190), p.GetDTS())
		require.Equal(t, int64(10), p.GetDuration())
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
