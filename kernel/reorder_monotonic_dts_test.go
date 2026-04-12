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

