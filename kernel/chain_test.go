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
)

func TestChain(t *testing.T) {
	ctx := context.Background()
	d0 := &Dummy{
		SendInputPacketFn: func(ctx context.Context, input packet.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputPacketsCh <- packet.Output(input)
			return nil
		},
		SendInputFrameFn: func(ctx context.Context, input frame.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputFramesCh <- frame.Output(input)
			return nil
		},
	}
	d1 := &Dummy{
		SendInputPacketFn: func(ctx context.Context, input packet.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			if d0.SendInputPacketCallCount == 0 {
				return fmt.Errorf("invalid order d1 before d0")
			}
			outputPacketsCh <- packet.Output(input)
			return nil
		},
		SendInputFrameFn: func(ctx context.Context, input frame.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputFramesCh <- frame.Output(input)
			return nil
		},
	}
	d2 := &Dummy{
		SendInputPacketFn: func(ctx context.Context, input packet.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			if d1.SendInputPacketCallCount == 0 {
				return fmt.Errorf("invalid order d2 before d1")
			}
			outputPacketsCh <- packet.Output(input)
			return nil
		},
		SendInputFrameFn: func(ctx context.Context, input frame.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputFramesCh <- frame.Output(input)
			return nil
		},
	}
	s := NewChain(d0, d1, d2)
	err := s.Generate(ctx, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, d0.GenerateCallCount)
	require.Equal(t, 1, d1.GenerateCallCount)
	require.Equal(t, 1, d2.GenerateCallCount)

	outPktCh := make(chan packet.Output, 10)
	outFrameCh := make(chan frame.Output, 10)

	err = s.SendInputPacket(ctx, packet.Input{
		Packet: &astiav.Packet{},
	}, outPktCh, outFrameCh)
	require.NoError(t, err)

	err = s.SendInputFrame(ctx, frame.Input{
		Frame: &astiav.Frame{},
	}, outPktCh, outFrameCh)
	require.NoError(t, err)

	<-outPktCh
	<-outFrameCh

	select {
	case pkt := <-outPktCh:
		t.Errorf("receive an extra packet: %#+v", pkt)
	default:
	}
	select {
	case frame := <-outFrameCh:
		t.Errorf("receive an extra frame: %#+v", frame)
	default:
	}

	assertT.Equal(t, 1, d0.SendInputPacketCallCount)
	assertT.Equal(t, 1, d1.SendInputPacketCallCount)
	assertT.Equal(t, 1, d2.SendInputPacketCallCount)
	assertT.Equal(t, 1, d0.SendInputFrameCallCount)
	assertT.Equal(t, 1, d1.SendInputFrameCallCount)
	assertT.Equal(t, 1, d2.SendInputFrameCallCount)
}
