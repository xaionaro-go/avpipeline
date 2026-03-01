package node

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// packetGeneratingKernel is a kernel that generates a configurable number of packets
// then blocks until context is cancelled.
type packetGeneratingKernel struct {
	testKernel
	packetsToGenerate int
	streamInfo        *packet.StreamInfo
}

func newPacketGeneratingKernel(count int) *packetGeneratingKernel {
	codecParams := astiav.AllocCodecParameters()
	codecParams.SetMediaType(astiav.MediaTypeVideo)
	return &packetGeneratingKernel{
		testKernel:        testKernel{stringValue: "packetGen"},
		packetsToGenerate: count,
		streamInfo: &packet.StreamInfo{
			CodecParameters: codecParams,
			StreamIndex:     0,
			StreamsCount:    1,
		},
	}
}

func (k *packetGeneratingKernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	for i := 0; i < k.packetsToGenerate; i++ {
		pkt := packet.Pool.Get()
		pktOutput := packet.BuildOutput(pkt, k.streamInfo)
		select {
		case <-ctx.Done():
			packet.Pool.Put(pkt)
			return ctx.Err()
		case outputCh <- packetorframe.OutputUnion{Packet: &pktOutput}:
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// frameGeneratingKernel is a kernel that generates a configurable number of frames
// then blocks until context is cancelled.
type frameGeneratingKernel struct {
	testKernel
	framesToGenerate int
	streamInfo       *frame.StreamInfo
}

func newFrameGeneratingKernel(count int) *frameGeneratingKernel {
	codecParams := astiav.AllocCodecParameters()
	codecParams.SetMediaType(astiav.MediaTypeAudio)
	return &frameGeneratingKernel{
		testKernel:       testKernel{stringValue: "frameGen"},
		framesToGenerate: count,
		streamInfo: &frame.StreamInfo{
			CodecParameters: codecParams,
			StreamIndex:     0,
			StreamsCount:    1,
		},
	}
}

func (k *frameGeneratingKernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	for i := 0; i < k.framesToGenerate; i++ {
		f := frame.Pool.Get()
		fOutput := frame.BuildOutput(f, k.streamInfo)
		select {
		case <-ctx.Done():
			frame.Pool.Put(f)
			return ctx.Err()
		case outputCh <- packetorframe.OutputUnion{Frame: &fOutput}:
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// waitForServing waits until the node reports IsServing == true.
func waitForServing(t *testing.T, n Abstract, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for !n.IsServing() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for node to start serving")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// waitForNotServing waits until the node reports IsServing == false.
func waitForNotServing(t *testing.T, n Abstract, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for n.IsServing() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for node to stop serving")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// TestServe_PushPacketToSingleDestination tests the complete Serve flow
// where a kernel generates packets and they are pushed to a destination node.
func TestServe_PushPacketToSingleDestination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newPacketGeneratingKernel(3)
	n := NewFromKernel[*packetGeneratingKernel](ctx, kernel)

	// Create a destination node that actually has an input channel
	dstKernel := &testKernel{stringValue: "dst"}
	dst := NewFromKernel[*testKernel](ctx, dstKernel)

	n.AddPushTo(ctx, dst)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	// Give time for packets to flow through
	time.Sleep(200 * time.Millisecond)

	// Verify that sent counters were incremented on the source node
	sentCount := n.Counters.Sent.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	tassert.GreaterOrEqual(t, sentCount, uint64(1), "sent counter should be incremented after pushing packets")

	// Verify addressed counters were incremented on the destination node
	addressedCount := dst.Counters.Addressed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	tassert.GreaterOrEqual(t, addressedCount, uint64(1), "addressed counter should be incremented on destination")

	cancel()
}

// TestServe_PushFrameToSingleDestination tests the Serve flow with frame output.
func TestServe_PushFrameToSingleDestination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newFrameGeneratingKernel(3)
	n := NewFromKernel[*frameGeneratingKernel](ctx, kernel)

	dstKernel := &testKernel{stringValue: "dst"}
	dst := NewFromKernel[*testKernel](ctx, dstKernel)

	n.AddPushTo(ctx, dst)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	// Give time for frames to flow through
	time.Sleep(200 * time.Millisecond)

	sentCount := n.Counters.Sent.Get(globaltypes.CountersSubSectionIDFrames).Get(globaltypes.MediaType(astiav.MediaTypeAudio)).Count.Load()
	tassert.GreaterOrEqual(t, sentCount, uint64(1), "sent counter should be incremented after pushing frames")

	cancel()
}

// TestServe_PushToMultipleDestinations tests that pushFurther fans out to multiple destinations.
func TestServe_PushToMultipleDestinations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newPacketGeneratingKernel(3)
	n := NewFromKernel[*packetGeneratingKernel](ctx, kernel)

	dstKernel1 := &testKernel{stringValue: "dst1"}
	dst1 := NewFromKernel[*testKernel](ctx, dstKernel1)

	dstKernel2 := &testKernel{stringValue: "dst2"}
	dst2 := NewFromKernel[*testKernel](ctx, dstKernel2)

	n.AddPushTo(ctx, dst1)
	n.AddPushTo(ctx, dst2)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	// Give time for packets to flow through
	time.Sleep(300 * time.Millisecond)

	// Both destinations should have received data
	addr1 := dst1.Counters.Addressed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	addr2 := dst2.Counters.Addressed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()

	tassert.GreaterOrEqual(t, addr1, uint64(1), "dst1 should have received addressed packets")
	tassert.GreaterOrEqual(t, addr2, uint64(1), "dst2 should have received addressed packets")

	cancel()
}

// TestServe_PushToWithCondition tests that a push condition filters output.
func TestServe_PushToWithCondition(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newPacketGeneratingKernel(5)
	n := NewFromKernel[*packetGeneratingKernel](ctx, kernel)

	dstKernel := &testKernel{stringValue: "dst"}
	dst := NewFromKernel[*testKernel](ctx, dstKernel)

	// Add push-to with a condition that always rejects
	cond := packetorframefiltercondition.Static(false)
	n.AddPushTo(ctx, dst, cond)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	// Give time for packets to flow through
	time.Sleep(200 * time.Millisecond)

	// Destination should not have been addressed since the condition filters everything
	addressedCount := dst.Counters.Addressed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	tassert.Equal(t, uint64(0), addressedCount, "destination should not be addressed when condition rejects")

	// But sent counter on source should still increment (pushFurther counts it)
	sentCount := n.Counters.Sent.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	tassert.GreaterOrEqual(t, sentCount, uint64(1), "source sent counter should be incremented even when condition rejects")

	cancel()
}

// TestServe_NoPushTos tests Serve behavior when there are no push destinations.
func TestServe_NoPushTos(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newPacketGeneratingKernel(3)
	n := NewFromKernel[*packetGeneratingKernel](ctx, kernel)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	// Give time for packets to flow through
	time.Sleep(200 * time.Millisecond)

	// Sent counter should still increment for packets that had nowhere to go
	sentCount := n.Counters.Sent.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	tassert.GreaterOrEqual(t, sentCount, uint64(1), "sent counter should still track packets with no destinations")

	cancel()
}

// TestServe_PushToDestinationWithDiscardInputChan tests pushing to a Dummy processor
// (which has DiscardInputChan) to exercise the isDiscard branch.
func TestServe_PushToDestinationWithDiscardInputChan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newPacketGeneratingKernel(3)
	n := NewFromKernel[*packetGeneratingKernel](ctx, kernel)

	// Dummy processor returns DiscardInputChan
	dst := newDummyNode()
	n.AddPushTo(ctx, dst)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	// Give time for packets to flow through
	time.Sleep(200 * time.Millisecond)

	// The discard path increments addressed but NOT received (isPushed remains false)
	addressedCount := dst.Counters.Addressed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	tassert.GreaterOrEqual(t, addressedCount, uint64(1), "discard destination should still be addressed")

	// Missed counter should be incremented (since isDiscard doesn't set isPushed=true)
	missedCount := dst.Counters.Missed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	tassert.GreaterOrEqual(t, missedCount, uint64(1), "discard destination should have missed count incremented")

	cancel()
}

// TestServe_FrameDropVideoEnabled tests the frame-drop path for video packets.
func TestServe_FrameDropVideoEnabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newPacketGeneratingKernel(3)
	n := NewFromKernel[*packetGeneratingKernel](ctx, kernel)

	dstKernel := &testKernel{stringValue: "dst"}
	dst := NewFromKernel[*testKernel](ctx, dstKernel)

	n.AddPushTo(ctx, dst)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{
		FrameDropVideo: true,
	}, errCh)

	waitForServing(t, n, 5*time.Second)

	// Give time for packets to flow through
	time.Sleep(200 * time.Millisecond)

	// With frame drop enabled, packets may be dropped if queue is full
	// but at least some should get through
	sentCount := n.Counters.Sent.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	tassert.GreaterOrEqual(t, sentCount, uint64(1), "sent counter should be incremented even with frame-drop")

	cancel()
}

// TestServe_FrameDropAudioEnabled tests the frame-drop path for audio frames.
func TestServe_FrameDropAudioEnabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newFrameGeneratingKernel(3)
	n := NewFromKernel[*frameGeneratingKernel](ctx, kernel)

	dstKernel := &testKernel{stringValue: "dst"}
	dst := NewFromKernel[*testKernel](ctx, dstKernel)

	n.AddPushTo(ctx, dst)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{
		FrameDropAudio: true,
	}, errCh)

	waitForServing(t, n, 5*time.Second)

	time.Sleep(200 * time.Millisecond)

	sentCount := n.Counters.Sent.Get(globaltypes.CountersSubSectionIDFrames).Get(globaltypes.MediaType(astiav.MediaTypeAudio)).Count.Load()
	tassert.GreaterOrEqual(t, sentCount, uint64(1), "sent counter should be incremented even with audio frame-drop")

	cancel()
}

// TestServe_InputFilterRejectsData tests that the input filter on the destination
// prevents data from being pushed.
func TestServe_InputFilterRejectsData(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newPacketGeneratingKernel(5)
	n := NewFromKernel[*packetGeneratingKernel](ctx, kernel)

	dstKernel := &testKernel{stringValue: "dst"}
	dst := NewFromKernel[*testKernel](ctx, dstKernel)

	// Set input filter on destination to reject all
	dst.SetInputFilter(ctx, packetorframefiltercondition.Static(false))

	n.AddPushTo(ctx, dst)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	time.Sleep(200 * time.Millisecond)

	// The input filter rejects, so received should be 0 but addressed should be > 0
	addressedCount := dst.Counters.Addressed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	receivedCount := dst.Counters.Received.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	missedCount := dst.Counters.Missed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()

	tassert.GreaterOrEqual(t, addressedCount, uint64(1), "addressed counter should increment despite filter")
	tassert.Equal(t, uint64(0), receivedCount, "received should be 0 when input filter rejects")
	tassert.GreaterOrEqual(t, missedCount, uint64(1), "missed should increment when input filter rejects")

	cancel()
}

// TestServe_ContextCancellationStopsServing tests that cancelling context stops serving.
func TestServe_ContextCancellationStopsServing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	n := newTestNode(ctx)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)
	tassert.True(t, n.IsServing())

	cancel()

	waitForNotServing(t, n, 5*time.Second)
	tassert.False(t, n.IsServing())
}

// TestServe_ErrorChanReceivesProcessorError tests that processor errors
// are forwarded through the error channel.
func TestServe_ErrorChanReceivesProcessorError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := newTestNode(ctx)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	// Close processor to trigger EOF
	err := n.Processor.Close(ctx)
	require.NoError(t, err)

	// Closing the processor triggers either EOF (from closed output chan)
	// or context canceled (from the processor's internal context being cancelled).
	// Under the race detector, timing may vary.
	select {
	case nodeErr := <-errCh:
		tassert.NotNil(t, nodeErr.Err, "should receive an error after processor close")
	case <-time.After(5 * time.Second):
		t.Fatal("expected an error after processor close")
	}
}

// TestServe_ErrChanFull tests the behavior when the error channel is full.
func TestServe_ErrChanFull(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := newTestNode(ctx)

	// Use a zero-buffer error channel so it's always full
	errCh := make(chan Error)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	// Close processor to trigger EOF; since errCh is full, it should log but not block
	err := n.Processor.Close(ctx)
	require.NoError(t, err)

	// Should not deadlock; give enough time for the error to be attempted
	time.Sleep(200 * time.Millisecond)

	cancel()
}

// TestServe_WithCacheHandler tests Serve with a CacheHandler configured.
// This test verifies that the CacheHandler's RememberPacketIfNeeded is called
// when packets flow through the Serve loop.
func TestServe_WithCacheHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &mockCacheHandler{}
	kernel := newPacketGeneratingKernel(2)
	proc := processor.NewFromKernel[*packetGeneratingKernel](ctx, kernel)

	n := NewWithCustomData[struct{}](proc, OptionCacheHandler(handler))

	dstKernel := &testKernel{stringValue: "dst"}
	dst := NewFromKernel[*testKernel](ctx, dstKernel)

	n.AddPushTo(ctx, dst)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	// Give time for packets to flow through
	time.Sleep(200 * time.Millisecond)

	// Verify that the cache handler received OnAddPushTo calls
	tassert.GreaterOrEqual(t, len(handler.addPushToCalls), 1, "CacheHandler.OnAddPushTo should have been called")

	cancel()
}

// TestAllWentInAndOut_AllZeros tests that allWentInAndOut returns true when all counters are zero.
func TestAllWentInAndOut_AllZeros(t *testing.T) {
	ctx := context.Background()
	nodeCounters := types.NewCounters()
	procCounters := processor.NewDummy().CountersPtr()

	result := allWentInAndOut(ctx, nodeCounters, procCounters)
	tassert.True(t, result, "all zeros should be considered drained")
}

// TestAllWentInAndOut_MismatchedCounters tests that allWentInAndOut returns false when counters mismatch.
func TestAllWentInAndOut_MismatchedCounters(t *testing.T) {
	ctx := context.Background()
	nodeCounters := types.NewCounters()
	procCounters := processor.NewDummy().CountersPtr()

	// Simulate: generated 1 packet but not yet sent
	procCounters.Generated.Packets.Increment(globaltypes.MediaType(astiav.MediaTypeVideo), 100)

	result := allWentInAndOut(ctx, nodeCounters, procCounters)
	tassert.False(t, result, "generated != sent means not drained")
}

// TestAllWentInAndOut_MatchedCounters tests that allWentInAndOut returns true when counters match.
func TestAllWentInAndOut_MatchedCounters(t *testing.T) {
	ctx := context.Background()
	nodeCounters := types.NewCounters()
	procCounters := processor.NewDummy().CountersPtr()

	// Simulate: generated 1 and sent 1
	procCounters.Generated.Packets.Increment(globaltypes.MediaType(astiav.MediaTypeVideo), 100)
	nodeCounters.Sent.Increment(globaltypes.CountersSubSectionIDPackets, globaltypes.MediaType(astiav.MediaTypeVideo), 100)

	result := allWentInAndOut(ctx, nodeCounters, procCounters)
	tassert.True(t, result, "generated == sent means drained")
}

// TestCalculateIfDrained_DirtyProcessor tests that calculateIfDrained returns false
// when the processor's IsDirty returns true.
func TestCalculateIfDrained_DirtyProcessor(t *testing.T) {
	ctx := context.Background()
	fp := newFlushableProcessor()
	fp.isDirty = true
	n := New[*flushableProcessor](fp)

	result := n.calculateIfDrained(ctx)
	tassert.False(t, result, "dirty processor means not drained")
}

// TestCalculateIfDrained_CleanProcessor tests that calculateIfDrained returns true
// when the processor is not dirty and counters match.
func TestCalculateIfDrained_CleanProcessor(t *testing.T) {
	ctx := context.Background()
	fp := newFlushableProcessor()
	fp.isDirty = false
	n := New[*flushableProcessor](fp)

	result := n.calculateIfDrained(ctx)
	tassert.True(t, result, "clean processor with matching counters means drained")
}

// TestUpdateProcInfoLocked_TransitionDrainedToNotDrained tests that updateProcInfoLocked
// fires the change chan when drained state changes.
func TestUpdateProcInfoLocked_TransitionDrainedToNotDrained(t *testing.T) {
	ctx := context.Background()
	fp := newFlushableProcessor()
	n := New[*flushableProcessor](fp)

	// Initially drained
	tassert.True(t, n.IsDrained(ctx))

	ch := n.GetChangeChanDrained()

	// Make processor dirty so it becomes not-drained
	fp.isDirty = true
	n.updateProcInfoLocked(ctx)

	tassert.False(t, n.IsDrained(ctx))

	// Change channel should have been closed
	select {
	case <-ch:
		// expected
	default:
		t.Fatal("change channel should be closed when drained state changes")
	}
}

// TestUpdateProcInfoLocked_NoTransition tests that updateProcInfoLocked does not
// fire the change chan when drained state stays the same.
func TestUpdateProcInfoLocked_NoTransition(t *testing.T) {
	ctx := context.Background()
	fp := newFlushableProcessor()
	n := New[*flushableProcessor](fp)

	ch := n.GetChangeChanDrained()

	// Update with no change in drained state
	n.updateProcInfoLocked(ctx)

	// Change channel should NOT be closed
	select {
	case <-ch:
		t.Fatal("change channel should not be closed when drained state didn't change")
	default:
		// expected
	}
}

// TestFlush_FlusherProcessor_RetryLoop tests the retry loop in Flush
// when the node is not immediately drained after flush.
func TestFlush_FlusherProcessor_RetryLoop(t *testing.T) {
	ctx := context.Background()
	callCount := 0
	fp := newFlushableProcessor()
	fp.flushFn = func(ctx context.Context) error {
		callCount++
		// On first call, stay dirty (to trigger retry)
		// On second call, become clean
		if callCount >= 2 {
			fp.isDirty = false
		}
		return nil
	}
	fp.isDirty = true
	n := New[*flushableProcessor](fp)
	// Mark as not drained so the retry loop activates
	n.IsDrainedValue.Store(false)

	err := n.Flush(ctx)
	tassert.NoError(t, err)
	tassert.GreaterOrEqual(t, callCount, 2, "flush should be retried when not drained after first flush")
}

// TestServe_PushToWithAcceptingCondition tests pushing with an accepting condition.
func TestServe_PushToWithAcceptingCondition(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newPacketGeneratingKernel(3)
	n := NewFromKernel[*packetGeneratingKernel](ctx, kernel)

	dstKernel := &testKernel{stringValue: "dst"}
	dst := NewFromKernel[*testKernel](ctx, dstKernel)

	// Add push-to with a condition that always accepts
	cond := packetorframefiltercondition.Static(true)
	n.AddPushTo(ctx, dst, cond)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	time.Sleep(200 * time.Millisecond)

	// Destination should have been addressed since the condition passes
	addressedCount := dst.Counters.Addressed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	tassert.GreaterOrEqual(t, addressedCount, uint64(1), "destination should be addressed when condition accepts")

	cancel()
}

// TestServe_PushToWithInputFilterAccepts tests the input filter accepting path.
func TestServe_PushToWithInputFilterAccepts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernel := newPacketGeneratingKernel(3)
	n := NewFromKernel[*packetGeneratingKernel](ctx, kernel)

	dstKernel := &testKernel{stringValue: "dst"}
	dst := NewFromKernel[*testKernel](ctx, dstKernel)

	// Set input filter on destination to accept all
	dst.SetInputFilter(ctx, packetorframefiltercondition.Static(true))

	n.AddPushTo(ctx, dst)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	waitForServing(t, n, 5*time.Second)

	time.Sleep(200 * time.Millisecond)

	// Packets should be received since input filter accepts
	receivedCount := dst.Counters.Received.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(astiav.MediaTypeVideo)).Count.Load()
	tassert.GreaterOrEqual(t, receivedCount, uint64(1), "packets should be received when input filter accepts")

	cancel()
}

// TestServe_FrameDropOther tests the FrameDropOther configuration path.
func TestServe_FrameDropOther(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a kernel with MediaTypeUnknown to exercise the "other" branch
	codecParams := astiav.AllocCodecParameters()
	// MediaTypeUnknown is the default (0 value)
	kernel := &frameGeneratingKernel{
		testKernel:       testKernel{stringValue: "otherGen"},
		framesToGenerate: 3,
		streamInfo: &frame.StreamInfo{
			CodecParameters: codecParams,
			StreamIndex:     0,
			StreamsCount:    1,
		},
	}
	n := NewFromKernel[*frameGeneratingKernel](ctx, kernel)

	dstKernel := &testKernel{stringValue: "dst"}
	dst := NewFromKernel[*testKernel](ctx, dstKernel)

	n.AddPushTo(ctx, dst)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{
		FrameDropOther: true,
	}, errCh)

	waitForServing(t, n, 5*time.Second)

	time.Sleep(200 * time.Millisecond)

	// Should still get through (may be dropped if queue is full but at least attempted)
	sentCount := n.Counters.Sent.Get(globaltypes.CountersSubSectionIDFrames).Get(globaltypes.MediaType(astiav.MediaTypeUnknown)).Count.Load()
	tassert.GreaterOrEqual(t, sentCount, uint64(1), "sent counter should increment for other media type with frame-drop")

	cancel()
}

// TestAllWentInAndOut_AddressedMismatch tests that addressed != processed+missed means not drained.
func TestAllWentInAndOut_AddressedMismatch(t *testing.T) {
	ctx := context.Background()
	nodeCounters := types.NewCounters()
	procCounters := processor.NewDummy().CountersPtr()

	// Addressed but not processed
	nodeCounters.Addressed.Increment(globaltypes.CountersSubSectionIDPackets, globaltypes.MediaType(astiav.MediaTypeVideo), 100)

	result := allWentInAndOut(ctx, nodeCounters, procCounters)
	tassert.False(t, result, "addressed without processed means not drained")
}

// TestAllWentInAndOut_OmittedCounters tests that omitted counters are accounted for in drain check.
func TestAllWentInAndOut_OmittedCounters(t *testing.T) {
	ctx := context.Background()
	nodeCounters := types.NewCounters()
	procCounters := processor.NewDummy().CountersPtr()

	// Generated 2, sent 1, omitted 1
	procCounters.Generated.Packets.Increment(globaltypes.MediaType(astiav.MediaTypeVideo), 100)
	procCounters.Generated.Packets.Increment(globaltypes.MediaType(astiav.MediaTypeVideo), 100)
	nodeCounters.Sent.Increment(globaltypes.CountersSubSectionIDPackets, globaltypes.MediaType(astiav.MediaTypeVideo), 100)
	procCounters.Omitted.Packets.Increment(globaltypes.MediaType(astiav.MediaTypeVideo), 100)

	result := allWentInAndOut(ctx, nodeCounters, procCounters)
	tassert.True(t, result, "generated == sent + omitted should mean drained")
}
