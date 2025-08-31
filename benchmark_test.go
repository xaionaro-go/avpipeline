package avpipeline

import (
	"context"
	"fmt"
	"runtime"
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

/*
streaming@void:~/go/src/github.com/xaionaro-go/avpipeline$ go test ./ -bench=. -memprofile /tmp/mem.out
goos: linux
goarch: amd64
pkg: github.com/xaionaro-go/avpipeline
cpu: AMD Ryzen 9 5900X 12-Core Processor
BenchmarkPipeline/connections=1-24         	 238009	     5141 ns/op	389.00 MB/s	    370 B/op	     13 allocs/op
BenchmarkPipeline/connections=2-24         	 164480	     7414 ns/op	269.77 MB/s	    722 B/op	     25 allocs/op
BenchmarkPipeline/connections=4-24         	 122740	     9751 ns/op	205.11 MB/s	   1428 B/op	     50 allocs/op
BenchmarkPipeline/connections=8-24         	  72518	    14592 ns/op	137.06 MB/s	   2846 B/op	     99 allocs/op
BenchmarkPipeline/connections=16-24        	  52832	    25968 ns/op	 77.02 MB/s	   5713 B/op	    197 allocs/op
BenchmarkPipeline/connections=32-24        	  29403	    49245 ns/op	 40.61 MB/s	  11496 B/op	    395 allocs/op
BenchmarkPipeline/connections=64-24        	  16123	    86001 ns/op	 23.26 MB/s	  23011 B/op	    791 allocs/op
BenchmarkPipeline/connections=128-24       	   7092	   172285 ns/op	 11.61 MB/s	  46035 B/op	   1583 allocs/op
BenchmarkPipeline/connections=256-24       	   3273	   400394 ns/op	  5.00 MB/s	  93062 B/op	   3183 allocs/op
BenchmarkPipeline/connections=512-24       	   1051	  1115880 ns/op	  1.79 MB/s	 193321 B/op	   6484 allocs/op
BenchmarkPipeline/connections=1024-24      	    258	  4119088 ns/op	  0.49 MB/s	 436327 B/op	  13765 allocs/op
PASS
ok  	github.com/xaionaro-go/avpipeline	19.859s
*/

func benchmarkPipeline(ctx context.Context, b *testing.B, connsCount int) {
	first := node.NewFromKernel(ctx, kernel.Passthrough{})

	ctx, cancelFn := context.WithCancel(ctx)

	n := first
	errCh := make(chan node.Error, 10000)
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
	pkt.Packet.SetSize(0)

	b.SetBytes(2000) // approx size of one packet (usually 200-20000 bytes; with ~20000 being on the rarer side)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		inCh <- pkt
		counters.Increment(globaltypes.MediaTypeUnknown, 2000)
	}
	runtime.KeepAlive(pkt)
}
