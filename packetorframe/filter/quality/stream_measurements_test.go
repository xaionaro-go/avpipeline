// stream_measurements_test.go provides tests for stream measurements.

package quality

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestStreamMeasurementGetStreamQuality(t *testing.T) {
	ctx := context.Background()

	fmtCtx := astiav.AllocFormatContext()
	defer fmtCtx.Free()
	codec := astiav.FindEncoderByName("libx264")
	stream := fmtCtx.NewStream(codec)
	stream.SetTimeBase(astiav.NewRational(1, 100))

	pkt := packet.BuildInput(
		astiav.AllocPacket(),
		&packet.StreamInfo{
			Stream: stream,
		},
	)

	var sm *StreamMeasurements
	observePacket := func(dts, dur int64) {
		pkt.SetDts(dts)
		pkt.SetDuration(dur)
		sm.observePacketOrFrameLocked(ctx, packetorframe.InputUnion{
			Packet: &pkt,
		})
	}

	t.Run("basic", func(t *testing.T) {
		sm = newStreamMeasurements(astiav.MediaTypeVideo, stream.TimeBase())
		for i := range int64(50) {
			observePacket(i*10, 10)
		}
		sq, err := sm.getStreamQuality(ctx)
		require.NoError(t, err)
		require.Equal(t, 1.0, sq.Continuity)
		require.Equal(t, 0.0, sq.Overlap)
		require.Equal(t, uint(0), sq.InvalidDTS)
		require.Equal(t, 10.0, sq.FrameRate)
	})
	t.Run("with_gaps", func(t *testing.T) {
		sm = newStreamMeasurements(astiav.MediaTypeVideo, stream.TimeBase())
		for i := range int64(50) {
			observePacket(i*10, 9)
		}
		sq, err := sm.getStreamQuality(ctx)
		require.NoError(t, err)
		require.InDelta(t, 0.9, sq.Continuity, 0.01)
		require.Equal(t, 0.0, sq.Overlap)
		require.Equal(t, uint(0), sq.InvalidDTS)
		require.Equal(t, 10.0, sq.FrameRate)
	})
	t.Run("with_overlap", func(t *testing.T) {
		sm = newStreamMeasurements(astiav.MediaTypeVideo, stream.TimeBase())
		for i := range int64(50) {
			observePacket(i*10, 11)
		}
		sq, err := sm.getStreamQuality(ctx)
		require.NoError(t, err)
		require.Equal(t, 1.0, sq.Continuity)
		require.InDelta(t, 0.1, sq.Overlap, 0.02)
		require.Equal(t, uint(0), sq.InvalidDTS)
		require.Equal(t, 9.0, sq.FrameRate)
	})
	t.Run("wrong_order", func(t *testing.T) {
		sm = newStreamMeasurements(astiav.MediaTypeVideo, stream.TimeBase())
		for i := range int64(50) {
			observePacket(i*10, 10)
			observePacket(10, 10)
		}
		sq, err := sm.getStreamQuality(ctx)
		require.NoError(t, err)
		require.Equal(t, 1.0, sq.Continuity)
		require.Equal(t, 0.0, sq.Overlap)
		require.Equal(t, uint(98), sq.InvalidDTS)
		require.Equal(t, 2.0, sq.FrameRate)
	})
}
