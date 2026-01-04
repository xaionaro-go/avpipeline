// benchmark_test.go provides benchmarks for the media pipeline.

package avpipeline

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
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

func (h *receiverHandler) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	switch {
	case input.Packet != nil:
		h.Received++
		if h.Received >= h.Expected {
			close(h.FinishedCh)
		}
		return nil
	default:
		return kerneltypes.ErrUnexpectedInputType{}
	}
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
BenchmarkPipeline/connections=1-24         	 257566	     4833 ns/op	413.81 MB/s	    182 B/op	      9 allocs/op
BenchmarkPipeline/connections=2-24         	 163645	     7209 ns/op	277.45 MB/s	    344 B/op	     17 allocs/op
BenchmarkPipeline/connections=4-24         	 133818	     9845 ns/op	203.15 MB/s	    669 B/op	     34 allocs/op
BenchmarkPipeline/connections=8-24         	  90391	    13693 ns/op	146.06 MB/s	   1317 B/op	     67 allocs/op
BenchmarkPipeline/connections=16-24        	  44365	    23109 ns/op	 86.55 MB/s	   2625 B/op	    133 allocs/op
BenchmarkPipeline/connections=32-24        	  34048	    40733 ns/op	 49.10 MB/s	   5363 B/op	    268 allocs/op
BenchmarkPipeline/connections=64-24        	  15618	    78987 ns/op	 25.32 MB/s	  10895 B/op	    539 allocs/op
BenchmarkPipeline/connections=128-24       	   7459	   163309 ns/op	 12.25 MB/s	  21278 B/op	   1069 allocs/op
BenchmarkPipeline/connections=256-24       	   3307	   380568 ns/op	  5.26 MB/s	  43963 B/op	   2161 allocs/op
BenchmarkPipeline/connections=512-24       	   1263	  1038802 ns/op	  1.93 MB/s	  93726 B/op	   4418 allocs/op
BenchmarkPipeline/connections=1024-24      	    351	  3456949 ns/op	  0.58 MB/s	 217818 B/op	   9330 allocs/op
PASS
ok  	github.com/xaionaro-go/avpipeline	20.299s
*/

func benchmarkPipeline(ctx context.Context, b *testing.B, connsCount int) {
	first := node.NewFromKernel(ctx, &kernel.Passthrough{})

	ctx, cancelFn := context.WithCancel(ctx)

	n := first
	errCh := make(chan node.Error, 10000)
	for range connsCount - 1 {
		next := node.NewFromKernel(ctx, &kernel.Passthrough{})
		n.AddPushTo(ctx, next)
		n = next
	}
	recvNode := newReceiverNode(ctx, b.N)
	n.AddPushTo(ctx, recvNode)

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
	inCh := first.Processor.InputChan()
	counters := first.GetCountersPtr().Received.Packets
	pkt := packetorframe.InputUnion{
		Packet: &packet.Input{
			StreamInfo: &packet.StreamInfo{},
		},
	}
	pkt.Packet.Packet = packet.Pool.Get()
	pkt.Packet.Packet.MakeWritable()
	pkt.Packet.Packet.SetSize(0)

	b.SetBytes(2000) // approx size of one packet (usually 200-20000 bytes; with ~20000 being on the rarer side)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		inCh <- pkt
		counters.Increment(globaltypes.MediaTypeUnknown, 2000)
	}
	runtime.KeepAlive(pkt)
}
