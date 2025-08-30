package avpipeline

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

type receiverHandler struct {
	Received   int
	Expected   int
	FinishedCh chan struct{}
}

func (h *receiverHandler) String() string {
	return fmt.Sprintf("receiverHandler{Received:%d, Expected:%d}", h.Received, h.Expected)
}

var _ kerneltypes.SendInputPacketer = (*receiverHandler)(nil)

func (h *receiverHandler) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	h.Received++
	if h.Received >= h.Expected {
		close(h.FinishedCh)
	}
	return nil
}

func newReceiverNode(
	ctx context.Context,
	expected int,
) *node.NodeWithCustomData[
	struct{},
	*processor.FromKernel[*boilerplate.Base[*receiverHandler]],
] {
	return node.NewWithCustomData[struct{}](
		processor.NewFromKernel[*boilerplate.Base[*receiverHandler]](ctx,
			boilerplate.NewBasicKernel(ctx, &receiverHandler{
				Expected:   expected,
				FinishedCh: make(chan struct{}),
			}),
		),
	)
}

func BenchmarkPipeline(b *testing.B) {
	ctx := context.Background()
	for l := 1; l <= 1024; l *= 2 {
		b.Run(fmt.Sprintf("connections=%d", l), func(b *testing.B) {
			benchmarkPipeline(ctx, b, l)
		})
	}
}

func benchmarkPipeline(ctx context.Context, b *testing.B, connsCount int) {
	first := node.NewFromKernel(ctx, kernel.Passthrough{})

	ctx, cancelFn := context.WithCancel(ctx)

	n := first
	errCh := make(chan node.Error, 1000)
	for range connsCount - 1 {
		next := node.NewFromKernel(ctx, kernel.Passthrough{})
		n.AddPushPacketsTo(next)
		n = next
	}
	recvNode := newReceiverNode(ctx, b.N)
	n.AddPushPacketsTo(recvNode)

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-errCh:
				b.Error(err)
			}
		}
	})

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		Serve(ctx, ServeConfig{}, errCh, first)
	})

	defer cancelFn()
	defer Drain(ctx, ptr(true), first)
	defer func() {
		<-recvNode.Processor.Kernel.Handler.FinishedCh
	}()
	inCh := first.Processor.InputPacketChan()
	counters := first.GetCountersPtr().Packets.Received
	pkt := packet.Input{
		StreamInfo: &packet.StreamInfo{},
	}
	pkt.Packet = packet.Pool.Get()
	pkt.Packet.MakeWritable()
	pkt.Packet.FromData(make([]byte, 2000))
	pkt.Packet.SetSize(2000)

	b.SetBytes(2000) // approx size of one packet (usually 200-20000 bytes; with ~20000 being on the rarer side)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		inCh <- pkt
		counters.Increment(globaltypes.MediaTypeUnknown, 2000)
	}
}
