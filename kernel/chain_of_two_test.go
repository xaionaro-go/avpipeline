package kernel

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

func TestChainOfTwo_GenerateForwarding(t *testing.T) {
	ctx := context.Background()

	k0 := &Dummy{
		GenerateFn: func(ctx context.Context, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputPacketsCh <- packet.Output(packet.Input{Packet: &astiav.Packet{}})
			outputFramesCh <- frame.Output(frame.Input{Frame: &astiav.Frame{}})
			return nil
		},
	}
	k1 := &Dummy{
		SendInputPacketFn: func(ctx context.Context, input packet.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputPacketsCh <- packet.Output(input)
			return nil
		},
		SendInputFrameFn: func(ctx context.Context, input frame.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputFramesCh <- frame.Output(input)
			return nil
		},
	}

	chain := NewChainOfTwo[Abstract, Abstract](k0, k1)
	outPktCh := make(chan packet.Output, 10)
	outFrameCh := make(chan frame.Output, 10)

	err := chain.Generate(ctx, outPktCh, outFrameCh)
	require.NoError(t, err)

	require.Equal(t, 1, k0.GenerateCallCount)
	require.Equal(t, 1, k1.SendInputPacketCallCount)
	require.Equal(t, 1, k1.SendInputFrameCallCount)

	select {
	case <-outPktCh:
	default:
		t.Fatal("expected at least one output packet")
	}
	select {
	case <-outFrameCh:
	default:
		t.Fatal("expected at least one output frame")
	}
}
