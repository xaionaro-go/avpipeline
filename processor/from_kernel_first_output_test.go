// from_kernel_first_output_test.go pins down the per-node
// first-output timestamp contract on FromKernel.
//
// Why this matters: a class of cascade-EOF wedges presents identically
// at the input layer (counters at zero, ffprobe EOF on the merged
// output). Per-layer first-output timing surfaced via the pipeline-
// stats RPC walks the chain to the exact node where flow stalled —
// the atomic-int64 firstOutputUnixNano on FromKernel is the data
// point.
//
// Falsifier intent: removing the recordFirstOutputTimestamp() call
// from firstObservationTracker.logFirstOutputPacket /
// logFirstOutputFrame must make
// TestFromKernel_FirstOutputUnixNano_SetOnFirstPacket fail
// (FirstOutputUnixNano stays at 0 even after a packet flowed).

package processor

import (
	"context"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// TestFromKernel_FirstOutputUnixNano_ZeroBeforeAnyOutput verifies the
// "no output yet" sentinel — FirstOutputUnixNano returns 0 when no
// packet/frame has been emitted.
func TestFromKernel_FirstOutputUnixNano_ZeroBeforeAnyOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)
	require.NotNil(t, p)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	tassert.Equal(t, int64(0), p.FirstOutputUnixNano(),
		"FirstOutputUnixNano must be 0 before any output is emitted")
}

// TestFromKernel_FirstOutputUnixNano_SetOnFirstPacket verifies the
// positive case: a packet flowing through preOutputCh records a
// non-zero timestamp within a sanity window of time.Now().
//
// Falsifier: remove the `t.recordFirstOutputTimestamp()` call from
// firstObservationTracker.logFirstOutputPacket — this assertion must
// fail (FirstOutputUnixNano stays at 0 even after the packet was
// forwarded).
func TestFromKernel_FirstOutputUnixNano_SetOnFirstPacket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	emitOnce := make(chan struct{}, 1)
	emitOnce <- struct{}{}
	k := &mockKernel{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			select {
			case <-emitOnce:
				select {
				case outputCh <- buildTestPacketOutput():
				case <-ctx.Done():
					return ctx.Err()
				}
			case <-ctx.Done():
				return ctx.Err()
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}

	beforeTs := time.Now().UnixNano()
	p := NewFromKernel[*mockKernel](ctx, k)
	require.NotNil(t, p)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	// Drain the output to free up the preOutputCh forwarder so
	// recordFirstOutputTimestamp gets called.
	select {
	case <-p.OutputChan():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first output packet")
	}
	afterTs := time.Now().UnixNano()

	got := p.FirstOutputUnixNano()
	require.NotEqual(t, int64(0), got,
		"FirstOutputUnixNano must be non-zero after first packet — recordFirstOutputTimestamp() is missing or broken")
	tassert.GreaterOrEqual(t, got, beforeTs,
		"FirstOutputUnixNano must be >= timestamp captured before NewFromKernel; got=%d before=%d", got, beforeTs)
	tassert.LessOrEqual(t, got, afterTs,
		"FirstOutputUnixNano must be <= timestamp after first output drain; got=%d after=%d", got, afterTs)
}

// TestFromKernel_FirstOutputUnixNano_NotOverwrittenOnSecondPacket
// guards the write-once contract: the timestamp is set ONCE, on the
// first packet, and never updated on subsequent packets.
func TestFromKernel_FirstOutputUnixNano_NotOverwrittenOnSecondPacket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	emitTwice := make(chan struct{}, 2)
	emitTwice <- struct{}{}
	emitTwice <- struct{}{}
	k := &mockKernel{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			for {
				select {
				case <-emitTwice:
					select {
					case outputCh <- buildTestPacketOutput():
					case <-ctx.Done():
						return ctx.Err()
					}
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		},
	}

	p := NewFromKernel[*mockKernel](ctx, k)
	require.NotNil(t, p)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	select {
	case <-p.OutputChan():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first output packet")
	}
	first := p.FirstOutputUnixNano()
	require.NotEqual(t, int64(0), first)

	// Single fixed delay (not polling) to guarantee monotonic-clock
	// advance so a non-write-once bug would observe a different
	// timestamp on the second packet.
	time.Sleep(2 * time.Millisecond)
	select {
	case <-p.OutputChan():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second output packet")
	}
	second := p.FirstOutputUnixNano()
	tassert.Equal(t, first, second,
		"FirstOutputUnixNano must NOT be overwritten on the second packet — write-once CAS contract violated")
}
