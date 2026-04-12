// reorder_monotonic_dts_test.go contains tests for the reorder_monotonic_dts kernel.

package kernel

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	sirupsenlogrus "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	kernelcondition "github.com/xaionaro-go/avpipeline/kernel/condition"
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

// TestReorderMonotonicDTS_LargeForwardGapAccepted feeds good packets,
// then a packet whose DTS is far beyond MaxDTSDifference from the
// current frontier (simulating a consumer connecting mid-stream).
// The gap packet must be accepted with PrevDTS reset; good packets
// must still flow through normally.
// Agent-generated test.
func TestReorderMonotonicDTS_LargeForwardGapAccepted(t *testing.T) {
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
	packetSource := &dummySource{FormatContext: out.FormatContext}
	err = k.NotifyAboutPacketSource(ctx, packetSource)
	require.NoError(t, err)

	// Good packets first, small monotonic DTS values.
	const goodDTSStride = int64(33)
	const goodCount = 3
	for i := range goodCount {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(stream.Index())
		pkt.SetDts(int64(i) * goodDTSStride)
		pkt.SetPts(int64(i) * goodDTSStride)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, chOut))
	}

	// Pathological: raw DTS 4.29e9 in a 1/1000 timebase is ~4.29e6s of
	// real time, well beyond the 1s MaxDTSDifference ceiling.
	const poisonedDTS = int64(4_294_967_295)
	pktPoisoned := packet.Pool.Get()
	pktPoisoned.SetStreamIndex(stream.Index())
	pktPoisoned.SetDts(poisonedDTS)
	pktPoisoned.SetPts(poisonedDTS)
	inputPoisoned := packet.BuildInput(pktPoisoned, &packet.StreamInfo{Stream: stream, Source: packetSource})
	require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &inputPoisoned}, chOut))

	// With the forward-gap-accept fix, the large-DTS packet is treated
	// as a legitimate mid-stream connect and accepted (PrevDTS reset).
	// Drain what's available and assert.
	sawLargeDTS := false
	emitted := 0
drain:
	for {
		select {
		case item := <-chOut:
			emitted++
			if item.GetDTS() == poisonedDTS {
				sawLargeDTS = true
			}
			continue
		default:
			break drain
		}
	}
	require.True(t, sawLargeDTS, "large forward-gap packet must be accepted (mid-stream connect)")
	require.GreaterOrEqual(t, emitted, goodCount,
		"all packets (good + gap) must flow through (got %d emitted)", emitted,
	)
}

// TestReorderMonotonicDTS_DeepBufferNoFalseWarning verifies that steadily
// arriving items do not trigger a "large forward DTS gap" warning when the
// reorder buffer is deep.
//
// Scenario: a start condition that never fires keeps items buffered without
// emission. After 25 items at 21ms intervals the queue front sits at DTS=0
// while the newest item has DTS=504ms. The old code compared new arrivals
// against the queue front, producing a false gap of 504ms (> 250ms threshold).
// The fix uses MaxDTSSeen (the highest DTS ever pushed) as reference, so the
// gap between consecutive items is only 21ms — well within the threshold.
func TestReorderMonotonicDTS_DeepBufferNoFalseWarning(t *testing.T) {
	var logBuf bytes.Buffer
	rawLogger := sirupsenlogrus.New()
	rawLogger.SetOutput(&logBuf)
	rawLogger.SetLevel(sirupsenlogrus.TraceLevel)
	l := logrus.New(rawLogger).WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	// Start condition that never triggers, so items accumulate in the buffer.
	neverStart := kernelcondition.Function[*ReorderMonotonicDTS](
		func(context.Context, *ReorderMonotonicDTS) bool { return false },
	)

	k := NewReorderMonotonicDTS(ctx, neverStart, 100, 250*time.Millisecond, true)
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
	packetSource := &dummySource{FormatContext: out.FormatContext}
	err = k.NotifyAboutPacketSource(ctx, packetSource)
	require.NoError(t, err)

	// Push 25 items at 21ms intervals (simulating ~48fps video).
	// DTS values: 0, 21, 42, ..., 504 (in a 1/1000 timebase each unit = 1ms).
	const itemCount = 25
	const dtsStride = int64(21)
	for i := range itemCount {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(stream.Index())
		pkt.SetDts(int64(i) * dtsStride)
		pkt.SetPts(int64(i) * dtsStride)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, chOut))
	}

	belt.Flush(ctx)
	logs := logBuf.String()
	require.False(t,
		strings.Contains(logs, "large forward DTS gap"),
		"expected no 'large forward DTS gap' warning, but found one in logs:\n%s", logs,
	)
}

// TestReorderMonotonicDTS_NoPTSPacketNotDiscarded verifies that a packet with
// DTS = astiav.NoPtsValue (AV_NOPTS_VALUE / math.MinInt64) passes through
// without being discarded and without spurious backward-DTS warnings.
//
// Bug: doSendItem converts NoPtsValue via avconv.Duration to time.Duration(math.MinInt64).
// The comparison r.PrevDTS > dts is then true for any non-negative PrevDTS.
// The overflow in r.PrevDTS - dts produces a negative result, so the "large
// backward" reset branch is skipped, falling through to the DiscardUnorderedItems
// branch which silently drops the packet.
func TestReorderMonotonicDTS_NoPTSPacketNotDiscarded(t *testing.T) {
	var logBuf bytes.Buffer
	rawLogger := sirupsenlogrus.New()
	rawLogger.SetOutput(&logBuf)
	rawLogger.SetLevel(sirupsenlogrus.TraceLevel)
	l := logrus.New(rawLogger).WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	// nil start condition → emission starts as soon as all streams have ≥1 item.
	k := NewReorderMonotonicDTS(ctx, nil, 100, 250*time.Millisecond, true)
	chOut := make(chan packetorframe.OutputUnion, 100)

	out, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: globaltypes.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer out.Close(ctx)

	stream0 := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	stream0.SetIndex(0)
	stream0.SetTimeBase(astiav.NewRational(1, 1000))
	stream1 := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	stream1.SetIndex(1)
	stream1.SetTimeBase(astiav.NewRational(1, 1000))
	packetSource := &dummySource{FormatContext: out.FormatContext}
	require.NoError(t, k.NotifyAboutPacketSource(ctx, packetSource))

	// Send a few normal packets on stream 0 so PrevDTS advances.
	for _, dts := range []int64{0, 33, 66} {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(stream0.Index())
		pkt.SetDts(dts)
		pkt.SetPts(dts)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream0, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, chOut))
	}

	// Normal packets on stream 1 to allow emission.
	for _, dts := range []int64{0, 33, 66} {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(stream1.Index())
		pkt.SetDts(dts)
		pkt.SetPts(dts)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream1, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, chOut))
	}

	// Now send a NoPTS packet on stream 0.
	noptsPkt := packet.Pool.Get()
	noptsPkt.SetStreamIndex(stream0.Index())
	noptsPkt.SetDts(astiav.NoPtsValue)
	noptsPkt.SetPts(astiav.NoPtsValue)
	noptsInput := packet.BuildInput(noptsPkt, &packet.StreamInfo{Stream: stream0, Source: packetSource})
	require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &noptsInput}, chOut))

	// Send one more normal packet on stream 1 to flush items through the reorder buffer.
	flushPkt := packet.Pool.Get()
	flushPkt.SetStreamIndex(stream1.Index())
	flushPkt.SetDts(99)
	flushPkt.SetPts(99)
	flushInput := packet.BuildInput(flushPkt, &packet.StreamInfo{Stream: stream1, Source: packetSource})
	require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &flushInput}, chOut))

	// Drain output and look for the NoPTS packet.
	sawNoPTS := false
drain:
	for {
		select {
		case item := <-chOut:
			if item.GetDTS() == astiav.NoPtsValue {
				sawNoPTS = true
			}
		default:
			break drain
		}
	}

	belt.Flush(ctx)
	logs := logBuf.String()

	require.True(t, sawNoPTS,
		"NoPTS packet must pass through, not be discarded")
	require.False(t, strings.Contains(logs, "discarding"),
		"no 'discarding' warning expected for NoPTS packets, but found one in logs:\n%s", logs)
	require.False(t, strings.Contains(logs, "went far backwards"),
		"no backward-DTS warning expected for NoPTS packets, but found one in logs:\n%s", logs)
}

// TestReorderMonotonicDTS_NoPTSDTSWithValidPTS verifies that a packet with
// DTS=NoPtsValue but valid PTS (common from HW decoders like hevc_cuvid) gets
// its DTS corrected to PTS before entering the reorder heap, participates in
// normal cross-stream ordering, and is emitted without blocking other streams.
func TestReorderMonotonicDTS_NoPTSDTSWithValidPTS(t *testing.T) {
	var logBuf bytes.Buffer
	rawLogger := sirupsenlogrus.New()
	rawLogger.SetOutput(&logBuf)
	rawLogger.SetLevel(sirupsenlogrus.TraceLevel)
	l := logrus.New(rawLogger).WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	k := NewReorderMonotonicDTS(ctx, nil, 100, 250*time.Millisecond, true)
	chOut := make(chan packetorframe.OutputUnion, 100)

	out, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: globaltypes.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer out.Close(ctx)

	// Two streams: audio (stream 0) and video (stream 1).
	streamAudio := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDAac))
	streamAudio.SetIndex(0)
	streamAudio.SetTimeBase(astiav.NewRational(1, 1000))
	streamVideo := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	streamVideo.SetIndex(1)
	streamVideo.SetTimeBase(astiav.NewRational(1, 1000))
	packetSource := &dummySource{FormatContext: out.FormatContext}
	require.NoError(t, k.NotifyAboutPacketSource(ctx, packetSource))

	sendAudio := func(dts int64) {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(streamAudio.Index())
		pkt.SetDts(dts)
		pkt.SetPts(dts)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: streamAudio, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, chOut))
	}

	sendVideo := func(dts, pts int64) {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(streamVideo.Index())
		pkt.SetDts(dts)
		pkt.SetPts(pts)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: streamVideo, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, chOut))
	}

	// Audio: normal DTS.
	sendAudio(100)
	// Video: DTS=NoPtsValue, PTS=150. Simulates hevc_cuvid first packet.
	sendVideo(astiav.NoPtsValue, 150)
	// Interleave more packets so both streams stay non-empty long enough
	// for the emit loop to drain video items too.
	sendAudio(121)
	sendVideo(183, 183)
	sendAudio(142)
	sendVideo(216, 216)
	sendAudio(163)
	sendVideo(249, 249)
	sendAudio(184)

	// Drain output.
	var emitted []int64
drain:
	for {
		select {
		case item := <-chOut:
			emitted = append(emitted, item.GetDTS())
		default:
			break drain
		}
	}

	belt.Flush(ctx)
	logs := logBuf.String()

	// Video packet with NoPTS DTS must have been corrected to PTS=150
	// and emitted in DTS order alongside audio.
	require.Contains(t, emitted, int64(150),
		"video packet with NoPTS DTS should be emitted with DTS=PTS=150 (got %v)", emitted)
	require.GreaterOrEqual(t, len(emitted), 3,
		"at least 3 items should be emitted (got %d: %v)", len(emitted), emitted)

	// No discards, no unexpected NoPTS warnings.
	require.False(t, strings.Contains(logs, "discarding"),
		"no discard expected; logs:\n%s", logs)
	require.False(t, strings.Contains(logs, "unexpected NoPTS"),
		"no unexpected NoPTS in doSendItem; logs:\n%s", logs)
	require.False(t, strings.Contains(logs, "skipping reorder"),
		"item should not bypass reorder (PTS is valid); logs:\n%s", logs)
}

// TestReorderMonotonicDTS_PerStreamIsolation verifies that two streams whose
// DTS epochs differ by more than MaxDTSDifference do not trigger cross-stream
// warnings. Each stream's DTS reference must be tracked independently.
//
// Bug: doSendItem uses a single global PrevDTS for emission ordering. When
// items from different streams are emitted in interleaved order and one
// stream's DTS epoch is far from the other, the global PrevDTS comparison
// can produce spurious "resetting reference" or "discarding" warnings.
func TestReorderMonotonicDTS_PerStreamIsolation(t *testing.T) {
	var logBuf bytes.Buffer
	rawLogger := sirupsenlogrus.New()
	rawLogger.SetOutput(&logBuf)
	rawLogger.SetLevel(sirupsenlogrus.TraceLevel)
	l := logrus.New(rawLogger).WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	k := NewReorderMonotonicDTS(ctx, nil, 100, 250*time.Millisecond, true)
	chOut := make(chan packetorframe.OutputUnion, 100)

	out, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: globaltypes.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer out.Close(ctx)

	stream0 := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	stream0.SetIndex(0)
	stream0.SetTimeBase(astiav.NewRational(1, 1000))
	stream1 := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	stream1.SetIndex(1)
	stream1.SetTimeBase(astiav.NewRational(1, 1000))
	packetSource := &dummySource{FormatContext: out.FormatContext}
	require.NoError(t, k.NotifyAboutPacketSource(ctx, packetSource))

	// Stream 0 starts at DTS=0ms, stream 1 starts at DTS=500ms (offset > 250ms threshold).
	// Send interleaved packets.
	s0DTS := []int64{0, 21, 42, 63, 84}
	s1DTS := []int64{500, 521, 542, 563, 584}

	for i := range s0DTS {
		pkt0 := packet.Pool.Get()
		pkt0.SetStreamIndex(stream0.Index())
		pkt0.SetDts(s0DTS[i])
		pkt0.SetPts(s0DTS[i])
		input0 := packet.BuildInput(pkt0, &packet.StreamInfo{Stream: stream0, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input0}, chOut))

		pkt1 := packet.Pool.Get()
		pkt1.SetStreamIndex(stream1.Index())
		pkt1.SetDts(s1DTS[i])
		pkt1.SetPts(s1DTS[i])
		input1 := packet.BuildInput(pkt1, &packet.StreamInfo{Stream: stream1, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input1}, chOut))
	}

	// Drain all output.
drain:
	for {
		select {
		case <-chOut:
		default:
			break drain
		}
	}

	belt.Flush(ctx)
	logs := logBuf.String()

	require.False(t, strings.Contains(logs, "behind reference"),
		"no 'behind reference' warning expected for independent streams, but found:\n%s", logs)
	require.False(t, strings.Contains(logs, "large forward DTS gap"),
		"no 'large forward DTS gap' warning expected for independent streams, but found:\n%s", logs)
	require.False(t, strings.Contains(logs, "resetting reference"),
		"no 'resetting reference' warning expected for independent streams, but found:\n%s", logs)
	require.False(t, strings.Contains(logs, "went far backwards"),
		"no 'went far backwards' warning expected for independent streams, but found:\n%s", logs)
	require.False(t, strings.Contains(logs, "discarding"),
		"no 'discarding' warning expected for independent streams, but found:\n%s", logs)
}

// TestReorderMonotonicDTS_StreamRestartResetsReference verifies that when a
// single stream jumps backwards in DTS by more than MaxDTSDifference (genuine
// restart), the kernel warns once and resets its reference so that subsequent
// monotonic packets flow without further warnings.
func TestReorderMonotonicDTS_StreamRestartResetsReference(t *testing.T) {
	var logBuf bytes.Buffer
	rawLogger := sirupsenlogrus.New()
	rawLogger.SetOutput(&logBuf)
	rawLogger.SetLevel(sirupsenlogrus.TraceLevel)
	l := logrus.New(rawLogger).WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	k := NewReorderMonotonicDTS(ctx, nil, 100, 250*time.Millisecond, true)
	chOut := make(chan packetorframe.OutputUnion, 100)

	out, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: globaltypes.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer out.Close(ctx)

	// Single stream.
	stream := out.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	stream.SetIndex(0)
	stream.SetTimeBase(astiav.NewRational(1, 1000))
	packetSource := &dummySource{FormatContext: out.FormatContext}
	require.NoError(t, k.NotifyAboutPacketSource(ctx, packetSource))

	send := func(dts int64) {
		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(stream.Index())
		pkt.SetDts(dts)
		pkt.SetPts(dts)
		input := packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream, Source: packetSource})
		require.NoError(t, k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, chOut))
	}

	// Normal ascending packets (DTS in ms with 1/1000 timebase).
	// Accumulate enough DTS to produce a backward jump > MaxDTSDifference (250ms).
	send(0)
	send(100)
	send(200)
	send(300)
	send(400)
	send(500)

	// Stream restart: DTS jumps back to 0. Backward gap = 500ms > 250ms.
	send(0)

	// Continue from the restart with ascending DTS.
	send(100)
	send(200)

	// Drain output and collect emitted DTS values.
	var emitted []int64
drain:
	for {
		select {
		case item := <-chOut:
			emitted = append(emitted, item.GetDTS())
		default:
			break drain
		}
	}

	belt.Flush(ctx)
	logs := logBuf.String()

	// The restart packet (DTS=0 after DTS=500) must be emitted, not discarded.
	require.Contains(t, emitted, int64(0),
		"restart packet at DTS=0 must appear in output (got emitted: %v)", emitted)
	require.GreaterOrEqual(t, len(emitted), 7,
		"at least the 6 pre-restart + 1 restart packet should be emitted (got %d: %v)", len(emitted), emitted)

	// Two restart warnings expected: one from updateStreamDTSReference (ingress)
	// and one from doSendItem (egress). Both correctly detect the backward jump.
	restartCount := strings.Count(logs, "resetting reference")
	require.Equal(t, 2, restartCount,
		"expected exactly 2 'resetting reference' warnings (ingress + egress), got %d; logs:\n%s", restartCount, logs)

	// No discard warnings: the restart packet should not be silently dropped.
	require.False(t, strings.Contains(logs, "discarding"),
		"restart packet must not be discarded; logs:\n%s", logs)

	// Post-restart packets (DTS 100, 200) must not trigger any warnings.
	// Find the position after the last "resetting reference" to skip both
	// the ingress and egress restart warnings.
	lastResetIdx := strings.LastIndex(logs, "resetting reference")
	afterRestart := logs[lastResetIdx+len("resetting reference"):]
	require.False(t, strings.Contains(afterRestart, "went far backwards"),
		"no backward warnings expected after restart; remaining logs:\n%s", afterRestart)
	require.False(t, strings.Contains(afterRestart, "discarding"),
		"no discard warnings expected after restart; remaining logs:\n%s", afterRestart)
}

