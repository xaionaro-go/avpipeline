// chain_test.go contains tests for the chain kernel.

package kernel

import (
	"context"
	"fmt"
	"testing"

	"github.com/asticode/go-astiav"
	assertT "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestChain(t *testing.T) {
	ctx := context.Background()
	d0 := &Dummy{
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
	d1 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			if d0.SendInputCallCount == 0 {
				return fmt.Errorf("invalid order d1 before d0")
			}
			switch {
			case input.Packet != nil:
				outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			case input.Frame != nil:
				outputCh <- packetorframe.OutputUnion{Frame: (*frame.Output)(input.Frame)}
			}
			return nil
		},
	}
	d2 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			if d1.SendInputCallCount == 0 {
				return fmt.Errorf("invalid order d2 before d1")
			}
			switch {
			case input.Packet != nil:
				outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			case input.Frame != nil:
				outputCh <- packetorframe.OutputUnion{Frame: (*frame.Output)(input.Frame)}
			}
			return nil
		},
	}
	s := NewChain(d0, d1, d2)
	outCh := make(chan packetorframe.OutputUnion, 10)

	err := s.SendInput(ctx, packetorframe.InputUnion{Packet: &packet.Input{
		Packet: &astiav.Packet{},
	}}, outCh)
	require.NoError(t, err)

	err = s.SendInput(ctx, packetorframe.InputUnion{Frame: &frame.Input{
		Frame: &astiav.Frame{},
	}}, outCh)
	require.NoError(t, err)

	<-outCh
	<-outCh

	select {
	case pkt := <-outCh:
		t.Errorf("receive an extra packet: %#+v", pkt)
	default:
	}

	assertT.Equal(t, 1, d0.SendPacketCallCount)
	assertT.Equal(t, 1, d1.SendPacketCallCount)
	assertT.Equal(t, 1, d2.SendPacketCallCount)
	assertT.Equal(t, 1, d0.SendFrameCallCount)
	assertT.Equal(t, 1, d1.SendFrameCallCount)
	assertT.Equal(t, 1, d2.SendFrameCallCount)
}

func TestChain_GenerateForwarding(t *testing.T) {
	ctx := context.Background()

	d0 := &Dummy{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(&packet.Input{Packet: &astiav.Packet{}})}
			outputCh <- packetorframe.OutputUnion{Frame: (*frame.Output)(&frame.Input{Frame: &astiav.Frame{}})}
			return nil
		},
	}
	d1 := &Dummy{
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

	s := NewChain(d0, d1)
	outCh := make(chan packetorframe.OutputUnion, 10)

	err := s.Generate(ctx, outCh)
	require.NoError(t, err)

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

	require.Equal(t, 1, d0.GenerateCallCount)
	require.Equal(t, 1, d1.SendPacketCallCount)
	require.Equal(t, 1, d1.SendFrameCallCount)
}
