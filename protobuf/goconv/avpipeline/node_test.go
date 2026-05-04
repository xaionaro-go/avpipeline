// node_test.go pins down the contract of NodeToGRPC populating
// FirstFrameUnixNs only for processor types that implement
// firstFrameUnixNanoer.

package avpipeline

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// firstFrameTestSource implements packet.Source — required by
// packetorframetypes.StreamInfo. The implementation matches the
// processor package's mockSource (kept private there).
type firstFrameTestSource struct{}

func (s *firstFrameTestSource) String() string { return "firstFrameTestSource" }
func (s *firstFrameTestSource) WithOutputFormatContext(
	_ context.Context,
	_ func(*astiav.FormatContext),
) {
}

// firstFrameTestKernel is a minimal kernel that emits one packet on
// Generate when emit is signalled, then blocks on ctx.Done(). Used to
// drive a real FromKernel processor end-to-end so its
// FirstFrameUnixNano() goes from zero to a non-zero timestamp.
type firstFrameTestKernel struct {
	emit chan struct{}
}

func (k *firstFrameTestKernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *firstFrameTestKernel) String() string                { return "firstFrameTestKernel" }
func (k *firstFrameTestKernel) Close(_ context.Context) error { return nil }
func (k *firstFrameTestKernel) CloseChan() <-chan struct{}    { return nil }
func (k *firstFrameTestKernel) SendInput(
	_ context.Context,
	_ packetorframe.InputUnion,
	_ chan<- packetorframe.OutputUnion,
) error {
	return nil
}

func (k *firstFrameTestKernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	select {
	case <-k.emit:
		si := &packetorframetypes.StreamInfo{
			Source:      &firstFrameTestSource{},
			StreamIndex: 0,
		}
		pktOutput := packet.BuildOutput(nil, si)
		select {
		case outputCh <- packetorframe.OutputUnion{Packet: &pktOutput}:
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}

// TestNodeToGRPC_FromKernel_PopulatesFirstFrameUnixNs verifies the
// positive end-to-end path: a real FromKernel-backed node, after
// emitting a packet, surfaces a non-zero FirstFrameUnixNs in the
// gRPC representation.
//
// Falsifier intent: rename FirstFrameUnixNano on FromKernel without
// updating firstFrameUnixNanoer here; the type assertion silently
// fails and the field stays absent. This test catches that.
func TestNodeToGRPC_FromKernel_PopulatesFirstFrameUnixNs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	emit := make(chan struct{}, 1)
	emit <- struct{}{}
	k := &firstFrameTestKernel{emit: emit}

	beforeTs := time.Now().UnixNano()
	n := node.NewFromKernel[*firstFrameTestKernel](ctx, k)
	require.NotNil(t, n)
	t.Cleanup(func() { _ = n.Processor.Close(context.Background()) })

	// Drain the output so the forwarder records the first-output
	// timestamp.
	select {
	case <-n.Processor.OutputChan():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first output packet")
	}
	afterTs := time.Now().UnixNano()

	got := NodeToGRPC(ctx, n)
	require.NotNil(t, got)
	require.NotNil(t, got.FirstFrameUnixNs,
		"FirstFrameUnixNs must be set (proto3-optional present) for FromKernel processors")
	tassert.GreaterOrEqual(t, *got.FirstFrameUnixNs, beforeTs)
	tassert.LessOrEqual(t, *got.FirstFrameUnixNs, afterTs)
}

// TestNodeToGRPC_FromKernel_PresentBeforeOutput verifies that
// FromKernel-backed nodes, even before any output, surface
// FirstFrameUnixNs as present-with-value-0 — not absent. Operators
// must distinguish "tracked but no output yet" (STALLED candidate)
// from "type does not record this fact" (N/A).
func TestNodeToGRPC_FromKernel_PresentBeforeOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &firstFrameTestKernel{emit: make(chan struct{})}
	n := node.NewFromKernel[*firstFrameTestKernel](ctx, k)
	require.NotNil(t, n)
	t.Cleanup(func() { _ = n.Processor.Close(context.Background()) })

	got := NodeToGRPC(ctx, n)
	require.NotNil(t, got)
	require.NotNil(t, got.FirstFrameUnixNs,
		"FromKernel must surface FirstFrameUnixNs as present (pointer non-nil) even when value is 0")
	tassert.Equal(t, int64(0), *got.FirstFrameUnixNs)
}

// TestNodeToGRPC_NonFirstFrameProcessor_FieldAbsent verifies the
// negative path: processors that do NOT implement firstFrameUnixNanoer
// (Dummy here) leave FirstFrameUnixNs absent (nil pointer). This is
// the distinguish-not-tracked-from-no-output-yet contract.
func TestNodeToGRPC_NonFirstFrameProcessor_FieldAbsent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := processor.NewDummy()
	n := node.New[*processor.Dummy](d)
	require.NotNil(t, n)

	got := NodeToGRPC(ctx, n)
	require.NotNil(t, got)
	tassert.Nil(t, got.FirstFrameUnixNs,
		"FirstFrameUnixNs must be absent (nil) for processors that do not implement firstFrameUnixNanoer")
}
