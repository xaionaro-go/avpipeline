// stream_measurements_test.go provides tests for stream measurements.

package quality

import (
	"context"
	"math"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestNewMeasurements(t *testing.T) {
	m := NewMeasurements()
	require.NotNil(t, m)
	require.NotNil(t, m.StreamQualityInfo)
}

func TestMeasurements_ObserveAndGetQuality(t *testing.T) {
	ctx := context.Background()
	m := NewMeasurements()

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

	// Observe multiple packets
	for i := range int64(50) {
		pkt.SetDts(i * 10)
		pkt.SetDuration(10)
		m.ObservePacketOrFrame(ctx, packetorframe.InputUnion{Packet: &pkt})
	}

	q, err := m.GetQuality(ctx)
	require.NoError(t, err)
	require.NotNil(t, q)
	require.Len(t, *q, 1) // one stream
}

func TestQuality_Aggregate_VideoOnly(t *testing.T) {
	q := Quality{
		{
			MediaType: astiav.MediaTypeVideo,
			StreamQuality: StreamQuality{
				Continuity: 0.9,
				Overlap:    0.1,
				FrameRate:  30.0,
			},
		},
	}
	agg := q.Aggregate()
	require.NotNil(t, agg)
	require.InDelta(t, 0.9, agg.Video.Continuity, 0.001)
	require.Equal(t, 0.0, agg.Video.Overlap) // overlap sum is not accumulated in Aggregate
	require.InDelta(t, 30.0, agg.Video.FrameRate, 0.001)
	require.Equal(t, 0.0, agg.Audio.Continuity)
}

func TestQuality_Aggregate_AudioOnly(t *testing.T) {
	q := Quality{
		{
			MediaType: astiav.MediaTypeAudio,
			StreamQuality: StreamQuality{
				Continuity: 0.95,
				Overlap:    0.05,
				FrameRate:  44.1,
			},
		},
	}
	agg := q.Aggregate()
	require.NotNil(t, agg)
	require.InDelta(t, 0.95, agg.Audio.Continuity, 0.001)
	require.Equal(t, 0.0, agg.Audio.Overlap) // overlap sum is not accumulated in Aggregate
	require.Equal(t, 0.0, agg.Video.Continuity)
}

func TestQuality_Aggregate_Mixed(t *testing.T) {
	q := Quality{
		{
			MediaType: astiav.MediaTypeVideo,
			StreamQuality: StreamQuality{
				Continuity: 0.8,
				FrameRate:  30.0,
			},
		},
		{
			MediaType: astiav.MediaTypeAudio,
			StreamQuality: StreamQuality{
				Continuity: 0.95,
				FrameRate:  44.1,
			},
		},
	}
	agg := q.Aggregate()
	require.InDelta(t, 0.8, agg.Video.Continuity, 0.001)
	require.InDelta(t, 0.95, agg.Audio.Continuity, 0.001)
}

func TestQuality_Aggregate_NaN_Sanitization(t *testing.T) {
	q := Quality{
		{
			MediaType: astiav.MediaTypeVideo,
			StreamQuality: StreamQuality{
				Continuity: math.NaN(),
				Overlap:    math.Inf(1),
				FrameRate:  math.Inf(-1),
			},
		},
	}
	agg := q.Aggregate()
	require.Equal(t, 0.0, agg.Video.Continuity)
	require.Equal(t, 0.0, agg.Video.Overlap)
	require.Equal(t, 0.0, agg.Video.FrameRate)
}

func TestQuality_Aggregate_Empty(t *testing.T) {
	q := Quality{}
	agg := q.Aggregate()
	require.NotNil(t, agg)
	require.Equal(t, 0.0, agg.Video.Continuity)
	require.Equal(t, 0.0, agg.Audio.Continuity)
}

func TestStreamMeasurements_EmptyQuality(t *testing.T) {
	ctx := context.Background()
	sm := newStreamMeasurements(astiav.MediaTypeVideo, astiav.NewRational(1, 100))
	sq, err := sm.getStreamQuality(ctx)
	require.NoError(t, err)
	require.Equal(t, 0.0, sq.Continuity)
	require.Equal(t, 0.0, sq.FrameRate)
}

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
		// 10 frames spanning 0.99s → 10/0.99 ≈ 10.1
		require.InDelta(t, 10.0, sq.FrameRate, 0.2)
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
		// 9 frames spanning 0.91s → 9/0.91 ≈ 9.9
		require.InDelta(t, 10.0, sq.FrameRate, 0.2)
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
		// 2 valid frames spanning 0.2s → 2/0.2 = 10.0
		// (InvalidDTS=98 already signals the stream is broken)
		require.InDelta(t, 10.0, sq.FrameRate, 0.1)
	})
	t.Run("short_stream_30fps", func(t *testing.T) {
		// Less than 1 second of data at 30 FPS — should still report ~30 FPS
		sm = newStreamMeasurements(astiav.MediaTypeVideo, astiav.NewRational(1, 1000))
		for i := range int64(10) { // 10 frames at 30fps = 0.33s
			observePacket(i*33, 33) // 33ms per frame at 30fps
		}
		sq, err := sm.getStreamQuality(ctx)
		require.NoError(t, err)
		require.InDelta(t, 30.0, sq.FrameRate, 1.0,
			"short stream at 30 FPS should report ~30, not %v", sq.FrameRate)
	})
}
