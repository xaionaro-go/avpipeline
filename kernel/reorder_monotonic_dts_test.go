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

	k := NewReorderMonotonicDTS(ctx, nil, 10, 1000)

	chPktOut := make(chan packet.Output, 10)
	chFrameOut := make(chan frame.Output, 10)

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

	pkt0 := packet.Pool.Get()
	pkt0.SetDts(1)
	err = k.SendInputPacket(ctx,
		packet.BuildInput(pkt0, &packet.StreamInfo{Stream: stream0, Source: packetSource}),
		chPktOut, chFrameOut,
	)
	require.NoError(t, err)

	pkt1 := packet.Pool.Get()
	pkt1.SetDts(0)
	err = k.SendInputPacket(ctx,
		packet.BuildInput(pkt1, &packet.StreamInfo{Stream: stream1, Source: packetSource}),
		chPktOut, chFrameOut,
	)
	require.NoError(t, err)

	pktCount := 0
	for {
		select {
		case outPkt := <-chPktOut:
			pktCount++
			require.Equal(t, int64(0), outPkt.Packet.Dts())
			continue
		default:
		}
		break
	}
	require.Equal(t, 1, pktCount)

	select {
	case outPkt := <-chFrameOut:
		t.Fatalf("unexpected frame output: %v", outPkt)
	default:
	}
}
