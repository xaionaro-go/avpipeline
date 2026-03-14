// reorder_monotonic_dts_test.go contains tests for the reorder_monotonic_dts kernel.

package kernel

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

type dummySource struct {
	FormatContext *astiav.FormatContext
}

var _ packet.Source = (*dummySource)(nil)

func (d *dummySource) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	callback(d.FormatContext)
}

func (d *dummySource) String() string {
	return "dummySource"
}

func TestReorderMonotonicDTS(t *testing.T) {
	loggerLevel := logger.LevelTrace

	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	k := NewReorderMonotonicDTS(ctx, nil, 100, 1000, true)

	chOut := make(chan packetorframe.OutputUnion, 100)

	out, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: globaltypes.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	stream0 := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	stream0.SetIndex(0)
	stream1 := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	stream1.SetIndex(1)
	packetSource := &dummySource{
		FormatContext: out.FormatContext,
	}

	err = k.NotifyAboutPacketSource(ctx, packetSource)
	require.NoError(t, err)

	for i := range 10 {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(stream0.Index())
		pkt.SetDts(5 + int64(i))
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream0, Source: packetSource})
		err = k.SendInput(ctx,
			packetorframe.InputUnion{Packet: &input},
			chOut,
		)
		require.NoError(t, err)
	}

	for i := range 10 {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(stream1.Index())
		pkt.SetDts(0 + int64(i))
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream1, Source: packetSource})
		err = k.SendInput(ctx,
			packetorframe.InputUnion{Packet: &input},
			chOut,
		)
		require.NoError(t, err)
	}

	pktCount := 0
	for {
		select {
		case out := <-chOut:
			outPkt := out.Packet
			expectedDTS := int64(pktCount)
			if expectedDTS > 5 {
				expectedDTS = 5 + int64(pktCount-5)/2
			}
			require.Equal(t, int64(expectedDTS), outPkt.Packet.Dts(), pktCount)
			pktCount++
			continue
		default:
		}
		break
	}
	require.Equal(t, 15, pktCount)

	select {
	case out := <-chOut:
		t.Fatalf("unexpected output: %v", out)
	default:
	}
}

// TestReorderMonotonicDTS_DesynchronizedEpochs reproduces the "received too old
// item" error that occurs when audio and video streams have different timestamp
// epochs (e.g., video uses device monotonic clock at ~55510s of uptime, while
// audio starts from 0). The reorder buffer discards audio packets whose DTS
// is far below the video DTS.
// Agent-generated test.
func TestReorderMonotonicDTS_DesynchronizedEpochs(t *testing.T) {
	l := logrus.Default().WithLevel(logger.LevelWarning)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	// maxDTSDifference=1_000_000: any DTS gap larger than this triggers
	// "too old" or "way newer" handling.
	k := NewReorderMonotonicDTS(ctx, nil, 100, 1_000_000, true)

	chOut := make(chan packetorframe.OutputUnion, 100)

	out, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: globaltypes.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer out.Close(ctx)
	videoStream := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	videoStream.SetIndex(0)
	audioStream := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDAac))
	audioStream.SetIndex(1)
	packetSource := &dummySource{
		FormatContext: out.FormatContext,
	}

	err = k.NotifyAboutPacketSource(ctx, packetSource)
	require.NoError(t, err)

	// Send video packets with DTS starting from a large value (simulating
	// 55510 seconds of device uptime in millisecond time_base).
	const videoDTSBase = int64(55_510_000)
	for i := range 5 {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(videoStream.Index())
		pkt.SetDts(videoDTSBase + int64(i)*33) // ~30fps in ms
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: videoStream, Source: packetSource})
		err = k.SendInput(ctx,
			packetorframe.InputUnion{Packet: &input},
			chOut,
		)
		require.NoError(t, err)
	}

	// Send audio packets with DTS starting from 0 (simulating Microphone
	// that starts PTS from 0 instead of using the device monotonic clock).
	audioPktsDiscarded := 0
	for i := range 5 {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(audioStream.Index())
		pkt.SetDts(int64(i) * 21) // ~48kHz in ms
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: audioStream, Source: packetSource})
		err = k.SendInput(ctx,
			packetorframe.InputUnion{Packet: &input},
			chOut,
		)
		require.NoError(t, err)
	}

	// Count outputs: audio packets should have been discarded as "too old"
	// because their DTS (0..84) is far below video DTS (55510000..55510132).
	videoCount := 0
	audioCount := 0
drainDesync:
	for {
		select {
		case item := <-chOut:
			switch item.GetStreamIndex() {
			case videoStream.Index():
				videoCount++
			case audioStream.Index():
				audioCount++
			}
			continue
		default:
			break drainDesync
		}
	}

	// The audio packets should be discarded because their DTS is way below
	// the video DTS. This reproduces the "received too old item" error.
	audioPktsDiscarded = 5 - audioCount
	require.Greater(t, audioPktsDiscarded, 0,
		"audio packets with DTS~0 should be discarded when video DTS is at ~55510000 (reproduces 'received too old item' bug)")
	t.Logf("reproduced: %d/%d audio packets discarded as 'too old'", audioPktsDiscarded, 5)

	// Now demonstrate the fix: if audio DTS starts from a value close to
	// video DTS (as it would when Microphone uses CLOCK_MONOTONIC), all
	// packets are accepted.
	k2 := NewReorderMonotonicDTS(ctx, nil, 100, 1_000_000, true)
	chOut2 := make(chan packetorframe.OutputUnion, 100)
	err = k2.NotifyAboutPacketSource(ctx, packetSource)
	require.NoError(t, err)

	for i := range 5 {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(videoStream.Index())
		pkt.SetDts(videoDTSBase + int64(i)*33)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: videoStream, Source: packetSource})
		err = k2.SendInput(ctx,
			packetorframe.InputUnion{Packet: &input},
			chOut2,
		)
		require.NoError(t, err)
	}

	// Audio DTS synchronized with video (using CLOCK_MONOTONIC epoch).
	const audioDTSSynced = int64(55_510_000)
	for i := range 5 {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(audioStream.Index())
		pkt.SetDts(audioDTSSynced + int64(i)*21)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: audioStream, Source: packetSource})
		err = k2.SendInput(ctx,
			packetorframe.InputUnion{Packet: &input},
			chOut2,
		)
		require.NoError(t, err)
	}

	videoCount2 := 0
	audioCount2 := 0
drainFixed:
	for {
		select {
		case item := <-chOut2:
			switch item.GetStreamIndex() {
			case videoStream.Index():
				videoCount2++
			case audioStream.Index():
				audioCount2++
			}
			continue
		default:
			break drainFixed
		}
	}

	// With synchronized timestamps, all audio packets should be accepted.
	require.Equal(t, 5, audioCount2,
		"with synchronized DTS epochs, all audio packets should be accepted")
	t.Logf("fixed: %d/%d video and %d/%d audio packets accepted", videoCount2, 5, audioCount2, 5)
}
