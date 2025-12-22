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

func TestChainOfThree(t *testing.T) {
	ctx := context.Background()

	pkt := astiav.AllocPacket()
	defer pkt.Free()
	frm := astiav.AllocFrame()
	defer frm.Free()

	frmStreamInfo := &frame.StreamInfo{
		StreamIndex: 0,
		TimeBase:    astiav.NewRational(1, 1000),
	}

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
	chain := NewChainOfThree[Abstract, Abstract, Abstract](d0, d1, d2)
	outPktCh := make(chan packet.Output, 10)
	outFrameCh := make(chan frame.Output, 10)

	err := chain.SendInputPacket(ctx, packet.Input{
		Packet: pkt,
	}, outPktCh, outFrameCh)
	require.NoError(t, err)

	err = chain.SendInputFrame(ctx, frame.Input{
		Frame:      frm,
		StreamInfo: frmStreamInfo,
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

func TestChainOfThree_GenerateForwarding(t *testing.T) {
	ctx := context.Background()

	pkt := astiav.AllocPacket()
	defer pkt.Free()
	frm := astiav.AllocFrame()
	defer frm.Free()
	frmStreamInfo := &frame.StreamInfo{
		StreamIndex: 0,
		TimeBase:    astiav.NewRational(1, 1000),
	}

	k0 := &Dummy{
		GenerateFn: func(ctx context.Context, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputPacketsCh <- packet.Output(packet.Input{Packet: pkt})
			outputFramesCh <- frame.Output(frame.Input{Frame: frm, StreamInfo: frmStreamInfo})
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
	k2 := &Dummy{
		SendInputPacketFn: func(ctx context.Context, input packet.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputPacketsCh <- packet.Output(input)
			return nil
		},
		SendInputFrameFn: func(ctx context.Context, input frame.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			outputFramesCh <- frame.Output(input)
			return nil
		},
	}

	chain := NewChainOfThree[Abstract, Abstract, Abstract](k0, k1, k2)
	outPktCh := make(chan packet.Output, 10)
	outFrameCh := make(chan frame.Output, 10)

	err := chain.Generate(ctx, outPktCh, outFrameCh)
	require.NoError(t, err)

	require.Equal(t, 1, k0.GenerateCallCount)
	require.Equal(t, 1, k1.SendInputPacketCallCount)
	require.Equal(t, 1, k1.SendInputFrameCallCount)
	require.Equal(t, 1, k2.SendInputPacketCallCount)
	require.Equal(t, 1, k2.SendInputFrameCallCount)

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

func TestChainOfThree_Close(t *testing.T) {
	ctx := context.Background()

	var closedOrder []int
	d0 := &Dummy{
		CloseFn: func(ctx context.Context) error {
			closedOrder = append(closedOrder, 0)
			return nil
		},
	}
	d1 := &Dummy{
		CloseFn: func(ctx context.Context) error {
			closedOrder = append(closedOrder, 1)
			return nil
		},
	}
	d2 := &Dummy{
		CloseFn: func(ctx context.Context) error {
			closedOrder = append(closedOrder, 2)
			return nil
		},
	}

	chain := NewChainOfThree[Abstract, Abstract, Abstract](d0, d1, d2)
	err := chain.Close(ctx)
	require.NoError(t, err)

	require.Equal(t, []int{0, 1, 2}, closedOrder)
	require.Equal(t, 1, d0.CloseCallCount)
	require.Equal(t, 1, d1.CloseCallCount)
	require.Equal(t, 1, d2.CloseCallCount)
}

func TestChainOfThree_CloseError(t *testing.T) {
	ctx := context.Background()

	d0 := &Dummy{
		CloseFn: func(ctx context.Context) error {
			return fmt.Errorf("d0 close error")
		},
	}
	d1 := &Dummy{
		CloseFn: func(ctx context.Context) error {
			return nil
		},
	}
	d2 := &Dummy{
		CloseFn: func(ctx context.Context) error {
			return fmt.Errorf("d2 close error")
		},
	}

	chain := NewChainOfThree[Abstract, Abstract, Abstract](d0, d1, d2)
	err := chain.Close(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "d0 close error")
	require.Contains(t, err.Error(), "d2 close error")
}

func TestChainOfThree_String(t *testing.T) {
	d0 := &Dummy{}
	d1 := &Dummy{}
	d2 := &Dummy{}

	chain := NewChainOfThree[Abstract, Abstract, Abstract](d0, d1, d2)
	s := chain.String()
	require.Contains(t, s, "ChainOfThree")
	require.Contains(t, s, "Dummy")
}

func TestChainOfThree_GetKernels(t *testing.T) {
	d0 := &Dummy{}
	d1 := &Dummy{}
	d2 := &Dummy{}

	chain := NewChainOfThree[Abstract, Abstract, Abstract](d0, d1, d2)
	kernels := chain.GetKernels()
	require.Len(t, kernels, 3)
	require.Same(t, d0, kernels[0])
	require.Same(t, d1, kernels[1])
	require.Same(t, d2, kernels[2])
}

func TestChainOfThree_SendInputPacketError(t *testing.T) {
	ctx := context.Background()

	pkt := astiav.AllocPacket()
	defer pkt.Free()

	d0 := &Dummy{
		SendInputPacketFn: func(ctx context.Context, input packet.Input, outputPacketsCh chan<- packet.Output, outputFramesCh chan<- frame.Output) error {
			return fmt.Errorf("d0 error")
		},
	}
	d1 := &Dummy{}
	d2 := &Dummy{}

	chain := NewChainOfThree[Abstract, Abstract, Abstract](d0, d1, d2)
	outPktCh := make(chan packet.Output, 10)
	outFrameCh := make(chan frame.Output, 10)

	err := chain.SendInputPacket(ctx, packet.Input{Packet: pkt}, outPktCh, outFrameCh)
	require.Error(t, err)
	require.Contains(t, err.Error(), "d0 error")
}
