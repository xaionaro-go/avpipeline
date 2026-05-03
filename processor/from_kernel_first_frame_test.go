// from_kernel_first_frame_test.go pins down the per-node first-frame
// timestamp contract added for #350 debugging-gaps Item 1.
//
// Why this matters: during the #350 cascade-EOF investigation, six
// distinct architectural root causes (retryable getKernel ctx-leak,
// retryable retry() ErrKernelNotSet fatal, retryable Unpause
// ctx-leak, retryable NewRetryable StartOnInit ctx-leak, ffstream
// chainPreExisted kick race, FromKernel startProcessing ctx-leak)
// each presented identically: Input.Video=0, ffprobe EOF on the
// merged output. Per-layer first-frame timing surfaced via the
// pipeline-stats RPC would have walked the chain to the exact node
// where flow stalled in seconds. The atomic-int64 firstFrameUnixNano
// field on FromKernel is the data point.
//
// Falsifier intent: removing the recordFirstOutputTimestamp() call
// from shouldDebugLogTracker.logFirstOutputPacket /
// logFirstOutputFrame must make
// TestFromKernel_FirstFrameUnixNano_SetOnFirstPacket fail
// (FirstFrameUnixNano stays at 0 even after a packet flowed).

package processor

import (
	"context"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// TestFromKernel_FirstFrameUnixNano_ZeroBeforeAnyOutput verifies the
// "no output yet" sentinel — FirstFrameUnixNano returns 0 when no
// packet/frame has been emitted.
func TestFromKernel_FirstFrameUnixNano_ZeroBeforeAnyOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)
	require.NotNil(t, p)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	tassert.Equal(t, int64(0), p.FirstFrameUnixNano(),
		"FirstFrameUnixNano must be 0 before any output is emitted")
}

// TestFromKernel_FirstFrameUnixNano_SetOnFirstPacket verifies the
// positive case: a packet flowing through preOutputCh records a
// non-zero timestamp within a sanity window of time.Now().
//
// Falsifier: remove the `t.recordFirstOutputTimestamp()` call from
// shouldDebugLogTracker.logFirstOutputPacket — this assertion must
// fail (FirstFrameUnixNano stays at 0 even after the packet was
// forwarded).
func TestFromKernel_FirstFrameUnixNano_SetOnFirstPacket(t *testing.T) {
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
	// recordFirstFrameTimestamp gets called.
	select {
	case <-p.OutputChan():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first output packet")
	}
	afterTs := time.Now().UnixNano()

	got := p.FirstFrameUnixNano()
	require.NotEqual(t, int64(0), got,
		"FirstFrameUnixNano must be non-zero after first packet — recordFirstFrameTimestamp() is missing or broken")
	tassert.GreaterOrEqual(t, got, beforeTs,
		"FirstFrameUnixNano must be >= timestamp captured before NewFromKernel; got=%d before=%d", got, beforeTs)
	tassert.LessOrEqual(t, got, afterTs,
		"FirstFrameUnixNano must be <= timestamp after first output drain; got=%d after=%d", got, afterTs)
}

// TestFromKernel_FirstFrameUnixNano_NotOverwrittenOnSecondPacket
// guards the write-once contract: the timestamp is set ONCE, on the
// first packet, and never updated on subsequent packets.
func TestFromKernel_FirstFrameUnixNano_NotOverwrittenOnSecondPacket(t *testing.T) {
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
	first := p.FirstFrameUnixNano()
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
	second := p.FirstFrameUnixNano()
	tassert.Equal(t, first, second,
		"FirstFrameUnixNano must NOT be overwritten on the second packet — write-once CAS contract violated")
}
