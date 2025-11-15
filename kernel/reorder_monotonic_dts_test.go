package kernel

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
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

	chPktOut := make(chan packet.Output, 100)
	chFrameOut := make(chan frame.Output, 100)

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

	for i := 0; i < 10; i++ {
		pkt := packet.Pool.Get()
		pkt.SetDts(5 + int64(i))
		err = k.SendInputPacket(ctx,
			packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream0, Source: packetSource}),
			chPktOut, chFrameOut,
		)
		require.NoError(t, err)
	}

	for i := 0; i < 10; i++ {
		pkt := packet.Pool.Get()
		pkt.SetDts(0 + int64(i))
		err = k.SendInputPacket(ctx,
			packet.BuildInput(pkt, &packet.StreamInfo{Stream: stream1, Source: packetSource}),
			chPktOut, chFrameOut,
		)
		require.NoError(t, err)
	}

	pktCount := 0
	for {
		select {
		case outPkt := <-chPktOut:
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
	case outPkt := <-chFrameOut:
		t.Fatalf("unexpected frame output: %v", outPkt)
	default:
	}
}
