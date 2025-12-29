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
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			switch {
			case input.Packet != nil:
				outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)((*packet.Commons)(input.Packet))}
			case input.Frame != nil:
				outputCh <- packetorframe.OutputUnion{Frame: (*frame.Output)((*frame.Commons)(input.Frame))}
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
				outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)((*packet.Commons)(input.Packet))}
			case input.Frame != nil:
				outputCh <- packetorframe.OutputUnion{Frame: (*frame.Output)((*frame.Commons)(input.Frame))}
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
				outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)((*packet.Commons)(input.Packet))}
			case input.Frame != nil:
				outputCh <- packetorframe.OutputUnion{Frame: (*frame.Output)((*frame.Commons)(input.Frame))}
			}
			return nil
		},
	}
	chain := NewChainOfThree[Abstract, Abstract, Abstract](d0, d1, d2)
	outCh := make(chan packetorframe.OutputUnion, 10)

	err := chain.SendInput(ctx, packetorframe.InputUnion{
		Packet: &packet.Input{
			Packet: pkt,
		},
	}, outCh)
	require.NoError(t, err)

	err = chain.SendInput(ctx, packetorframe.InputUnion{
		Frame: &frame.Input{
			Frame:      frm,
			StreamInfo: frmStreamInfo,
		},
	}, outCh)
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
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(&packet.Input{Packet: pkt})}
			outputCh <- packetorframe.OutputUnion{Frame: (*frame.Output)(&frame.Input{Frame: frm, StreamInfo: frmStreamInfo})}
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
	k2 := &Dummy{
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

	chain := NewChainOfThree[Abstract, Abstract, Abstract](k0, k1, k2)
	outCh := make(chan packetorframe.OutputUnion, 10)

	err := chain.Generate(ctx, outCh)
	require.NoError(t, err)

	require.Equal(t, 1, k0.GenerateCallCount)
	require.Equal(t, 1, k1.SendPacketCallCount)
	require.Equal(t, 1, k1.SendFrameCallCount)
	require.Equal(t, 1, k2.SendPacketCallCount)
	require.Equal(t, 1, k2.SendFrameCallCount)

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

func TestChainOfThree_SendInput_PacketError(t *testing.T) {
	ctx := context.Background()

	pkt := astiav.AllocPacket()
	defer pkt.Free()

	d0 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			return fmt.Errorf("d0 error")
		},
	}
	d1 := &Dummy{}
	d2 := &Dummy{}

	chain := NewChainOfThree[Abstract, Abstract, Abstract](d0, d1, d2)
	outCh := make(chan packetorframe.OutputUnion, 10)

	err := chain.SendInput(ctx, packetorframe.InputUnion{
		Packet: &packet.Input{
			Packet: pkt,
		},
	}, outCh)
	require.Error(t, err)
	require.Contains(t, err.Error(), "d0 error")
}
