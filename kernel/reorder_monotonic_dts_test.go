// reorder_monotonic_dts_test.go contains tests for the reorder_monotonic_dts kernel.

package kernel

import (
	"context"
	"testing"
	"time"

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

// TestReorderMonotonicDTS_DesynchronizedEpochs documents the "received too old
// item" warning that fires when audio and video streams carry genuinely
// different wall-clock epochs (e.g., video on a CLOCK_MONOTONIC at ~55510s
// of uptime, audio started at 0). After normalizing DTS values to
// time.Duration via each stream's timebase, real-time comparisons are
// correct — so packets are only dropped when the wall-clock gap between
// streams actually exceeds MaxDTSDifference, not because raw integer
// ticks in different timebases diverged.
// Agent-generated test.
func TestReorderMonotonicDTS_DesynchronizedEpochs(t *testing.T) {
	l := logrus.Default().WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	// MaxDTSDifference is 1s: a real-time gap above that is treated as
	// too old or too new, regardless of the streams' raw-integer
	// timebases.
	k := NewReorderMonotonicDTS(ctx, nil, 100, 1*time.Second, true)

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
	videoStream.SetTimeBase(astiav.NewRational(1, 1000))
	audioStream := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDAac))
	audioStream.SetIndex(1)
	audioStream.SetTimeBase(astiav.NewRational(1, 48000))
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
	// Audio timebase is 1/48000, so raw DTS=0..84 is 0..1.75ms of real
	// time. Video is at 55510s of real time: a genuine 55509s gap that
	// must be discarded as "too old".
	for i := range 5 {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(audioStream.Index())
		pkt.SetDts(int64(i) * 1024) // ~48 packets/s in 1/48000 timebase
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: audioStream, Source: packetSource})
		err = k.SendInput(ctx,
			packetorframe.InputUnion{Packet: &input},
			chOut,
		)
		require.NoError(t, err)
	}

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

	// Audio packets are ~55509s behind video in real time — way beyond
	// the 1s MaxDTSDifference — so they must be dropped.
	audioPktsDiscarded := 5 - audioCount
	require.Greater(t, audioPktsDiscarded, 0,
		"audio packets ~55509s of wall-clock behind video must be discarded even after Duration normalization")
	t.Logf("epoch-desync: %d/%d audio packets discarded as 'too old'", audioPktsDiscarded, 5)

	// Now demonstrate the fix: if audio DTS starts from a value close to
	// video DTS (as it would when Microphone uses CLOCK_MONOTONIC), all
	// packets are accepted.
	k2 := NewReorderMonotonicDTS(ctx, nil, 100, 1*time.Second, true)
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

	// Audio DTS synchronized with video in real time (audio uses 1/48000,
	// so 55510s becomes 55510 * 48000 raw ticks).
	const audioDTSSynced = int64(55_510) * 48000
	for i := range 5 {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(audioStream.Index())
		pkt.SetDts(audioDTSSynced + int64(i)*1024)
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

	// With synchronized wall-clock timestamps, all audio packets must be
	// accepted — the streams' timebases differ but their real times line
	// up within MaxDTSDifference.
	require.Equal(t, 5, audioCount2,
		"with synchronized wall-clock DTS epochs, all audio packets must be accepted")
	t.Logf("fixed: %d/%d video and %d/%d audio packets accepted", videoCount2, 5, audioCount2, 5)
}

// TestReorderMonotonicDTS_PathologicalGapResetsQueue reproduces the
// FLV-uint32-wrap scenario: the first packet through the reorder has a
// poisoned DTS (~4.29e9), and the next legitimate packet is far below.
// Before the defensive reset path, every subsequent real packet would
// be discarded indefinitely. The reset threshold must flush the queue
// and accept the legitimate packet.
// Agent-generated test.
func TestReorderMonotonicDTS_PathologicalGapResetsQueue(t *testing.T) {
	l := logrus.Default().WithLevel(logger.LevelError)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	// MaxDTSDifference=1s → reset threshold = 1000s. With a 1/1000
	// millisecond timebase, the poisoned raw DTS 4_294_967_295
	// corresponds to ~4.29e6s of real time, far beyond the reset
	// threshold.
	k := NewReorderMonotonicDTS(ctx, nil, 100, 1*time.Second, true)

	chOut := make(chan packetorframe.OutputUnion, 100)

	out, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: globaltypes.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer out.Close(ctx)
	stream := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	stream.SetIndex(0)
	stream.SetTimeBase(astiav.NewRational(1, 1000))
	packetSource := &dummySource{
		FormatContext: out.FormatContext,
	}
	err = k.NotifyAboutPacketSource(ctx, packetSource)
	require.NoError(t, err)

	// Feed a poisoned packet first — this simulates an FLV uint32 wrap
	// of a negative int64 upstream.
	const poisonedDTS = int64(4_294_967_295)
	pktPoisoned := packet.Pool.Get()
	pktPoisoned.SetStreamIndex(stream.Index())
	pktPoisoned.SetDts(poisonedDTS)
	pktPoisoned.SetPts(poisonedDTS)
	inputPoisoned := packet.BuildInput(pktPoisoned, &packet.StreamInfo{Stream: stream, Source: packetSource})
	require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &inputPoisoned}, chOut))

	// Now feed legitimate packets at sane DTS values.
	for i := range 5 {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(stream.Index())
		pkt.SetDts(int64(i) * 33)
		pkt.SetPts(int64(i) * 33)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, chOut))
	}

	// Every legitimate packet must be accepted: the reorder buffer is
	// expected to reset its DTS floor once the pathological gap is
	// detected, rather than discarding every legitimate packet.
	acceptedLegit := 0
drain:
	for {
		select {
		case item := <-chOut:
			dts := item.GetDTS()
			if dts >= 0 && dts <= 4*33 {
				acceptedLegit++
			}
			continue
		default:
			break drain
		}
	}
	require.GreaterOrEqual(t, acceptedLegit, 4,
		"at least 4 of the 5 legitimate packets must be accepted after the pathological-gap reset (got %d)",
		acceptedLegit,
	)
}

// TestReorderMonotonicDTS_LegitFirstThenPoisonedFlushes verifies that
// when legitimate packets arrive first and poisoned (wrapped) packets
// come later, the reorder buffer flushes rather than discarding
// everything indefinitely. If it discarded the wrapped packets as "way
// too new", a stream whose DTS range IS the wrapped range would
// starve — its stream queue would never fill, blocking pullAndSend.
// Agent-generated test.
func TestReorderMonotonicDTS_LegitFirstThenPoisonedFlushes(t *testing.T) {
	l := logrus.Default().WithLevel(logger.LevelError)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	k := NewReorderMonotonicDTS(ctx, nil, 100, 1*time.Second, true)

	chOut := make(chan packetorframe.OutputUnion, 100)

	out, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: globaltypes.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer out.Close(ctx)
	stream := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	stream.SetIndex(0)
	stream.SetTimeBase(astiav.NewRational(1, 1000))
	packetSource := &dummySource{
		FormatContext: out.FormatContext,
	}
	err = k.NotifyAboutPacketSource(ctx, packetSource)
	require.NoError(t, err)

	// Legit first: small monotonic DTS values.
	for i := range 3 {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(stream.Index())
		pkt.SetDts(int64(i) * 33)
		pkt.SetPts(int64(i) * 33)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, chOut))
	}

	// Poisoned: the raw DTS 4.29e9 in a 1/1000 timebase is ~4.29e6s of
	// real time, vastly exceeding the 1000s reset threshold.
	const poisonedDTS = int64(4_294_967_295)
	pktPoisoned := packet.Pool.Get()
	pktPoisoned.SetStreamIndex(stream.Index())
	pktPoisoned.SetDts(poisonedDTS)
	pktPoisoned.SetPts(poisonedDTS)
	inputPoisoned := packet.BuildInput(pktPoisoned, &packet.StreamInfo{Stream: stream, Source: packetSource})
	require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &inputPoisoned}, chOut))

	// Flushing must have emitted the queued legitimate packets and
	// accepted the poisoned one — total 4 items drained to chOut.
	totalEmitted := 0
drainLegitThenPoisoned:
	for {
		select {
		case <-chOut:
			totalEmitted++
			continue
		default:
			break drainLegitThenPoisoned
		}
	}
	require.GreaterOrEqual(t, totalEmitted, 4,
		"flushing the pathological gap must leave no packet stranded in the queue (got %d)",
		totalEmitted,
	)
}
