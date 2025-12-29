package kernel

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestChainOfTwo_GenerateForwarding(t *testing.T) {
	ctx := context.Background()

	k0 := &Dummy{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(&packet.Input{Packet: &astiav.Packet{}})}
			outputCh <- packetorframe.OutputUnion{Frame: (*frame.Output)(&frame.Input{Frame: &astiav.Frame{}})}
			return nil
		},
	}
	k1 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			switch {
			case input.Packet != nil:
				outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			case input.Frame != nil:
				outputCh <- packetorframe.OutputUnion{Frame: (*frame.Output)(input.Frame)}
			}
			return nil
		},
	}

	chain := NewChainOfTwo[Abstract, Abstract](k0, k1)
	outCh := make(chan packetorframe.OutputUnion, 10)

	err := chain.Generate(ctx, outCh)
	require.NoError(t, err)

	require.Equal(t, 1, k0.GenerateCallCount)
	require.Equal(t, 1, k1.SendPacketCallCount)
	require.Equal(t, 1, k1.SendFrameCallCount)

	select {
	case <-outCh:
	default:
		t.Fatal("expected at least one output packet")
	}
	select {
	case <-outCh:
	default:
		t.Fatal("expected at least one output frame")
	}
}
