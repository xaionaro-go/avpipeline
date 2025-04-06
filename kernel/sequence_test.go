package kernel

import (
	"context"
	"testing"

	assertT "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

func TestSequence(t *testing.T) {
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
			outputPacketsCh <- packet.Output(input)
			return nil
		},
		SendInputFrameFn: func(ctx context.Context, input frame.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputFramesCh <- frame.Output(input)
			return nil
		},
	}
	s := NewSequence(d0, d1)
	err := s.Generate(ctx, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, d0.GenerateCallCount)
	require.Equal(t, 1, d1.GenerateCallCount)

	outPktCh := make(chan packet.Output, 10)
	outFrameCh := make(chan frame.Output, 10)

	err = s.SendInputPacket(ctx, packet.Input{}, outPktCh, outFrameCh)
	require.NoError(t, err)

	err = s.SendInputFrame(ctx, frame.Input{}, outPktCh, outFrameCh)
	require.NoError(t, err)

	<-outPktCh
	<-outFrameCh

	select {
	case <-outPktCh:
		t.Errorf("receive an extra packet")
	default:
	}
	select {
	case <-outFrameCh:
		t.Errorf("receive an extra frame")
	default:
	}

	assertT.Equal(t, 1, d0.SendInputPacketCallCount)
	assertT.Equal(t, 1, d1.SendInputPacketCallCount)
	assertT.Equal(t, 1, d0.SendInputFrameCallCount)
	assertT.Equal(t, 1, d1.SendInputFrameCallCount)
}
