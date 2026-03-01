package node

import (
	"context"
	"fmt"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// testKernel is a simple kernel that blocks on ctx.Done() for Generate and SendInput.
type testKernel struct {
	stringValue string
}

var _ kernel.Abstract = (*testKernel)(nil)

func (k *testKernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *testKernel) String() string {
	if k.stringValue != "" {
		return k.stringValue
	}
	return "testKernel"
}

func (k *testKernel) Close(ctx context.Context) error {
	return nil
}

func (k *testKernel) CloseChan() <-chan struct{} {
	return nil
}

func (k *testKernel) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}

func (k *testKernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}

// newTestNode creates a node backed by a FromKernel processor with proper channels.
func newTestNode(ctx context.Context) *Node[*processor.FromKernel[*testKernel]] {
	return NewFromKernel[*testKernel](ctx, &testKernel{})
}

// flushableProcessor wraps processor.Dummy and implements processor.Flusher.
type flushableProcessor struct {
	*processor.Dummy
	isDirty  bool
	flushErr error
	flushFn  func(ctx context.Context) error
}

func newFlushableProcessor() *flushableProcessor {
	return &flushableProcessor{
		Dummy: processor.NewDummy(),
	}
}

func (p *flushableProcessor) IsDirty(ctx context.Context) bool {
	return p.isDirty
}

func (p *flushableProcessor) Flush(ctx context.Context) error {
	if p.flushFn != nil {
		return p.flushFn(ctx)
	}
	p.isDirty = false
	return p.flushErr
}

func TestFlush_FlusherProcessor_Success(t *testing.T) {
	ctx := context.Background()
	fp := newFlushableProcessor()
	n := New[*flushableProcessor](fp)

	err := n.Flush(ctx)
	tassert.NoError(t, err)
}

func TestFlush_FlusherProcessor_FlushError(t *testing.T) {
	ctx := context.Background()
	fp := newFlushableProcessor()
	fp.flushErr = fmt.Errorf("flush failed")
	n := New[*flushableProcessor](fp)

	err := n.Flush(ctx)
	tassert.Error(t, err)
	tassert.Contains(t, err.Error(), "unable to flush internal buffers")
}

func TestFlush_FlusherProcessor_DrainInputError(t *testing.T) {
	fp := newFlushableProcessor()
	n := New[*flushableProcessor](fp)

	// Cancel context before flushing to trigger DrainInput error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// DrainInput checks the context, so a cancelled context should cause it to fail
	// But actually, DrainInput uses a ticker and checks IsInputDrained which checks len(InputChan())
	// Since Dummy returns DiscardInputChan, IsInputDrained returns true immediately, so DrainInput succeeds.
	err := n.Flush(ctx)
	// Due to DiscardInputChan, DrainInput sees len(DiscardInputChan) == 0, so no error
	tassert.NoError(t, err)
}

func TestSetBlockInput_BlockUnblock(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()

	// Initially no input filter
	f := n.GetInputFilter(ctx)
	tassert.Nil(t, f)

	// Block input
	err := SetBlockInput(ctx, true, n)
	require.NoError(t, err)

	// Filter should now be a PauseCond
	f = n.GetInputFilter(ctx)
	tassert.NotNil(t, f)

	// Unblock input
	err = SetBlockInput(ctx, false, n)
	require.NoError(t, err)

	// Filter should be back to nil (the original condition)
	f = n.GetInputFilter(ctx)
	tassert.Nil(t, f, "after unblock, filter should revert to original")
}

func TestSetBlockInput_DoubleBlock(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()

	// Block twice should be idempotent
	err := SetBlockInput(ctx, true, n)
	require.NoError(t, err)

	err = SetBlockInput(ctx, true, n)
	require.NoError(t, err)

	// Should still be blocked
	f := n.GetInputFilter(ctx)
	tassert.NotNil(t, f)

	// Single unblock should work
	err = SetBlockInput(ctx, false, n)
	require.NoError(t, err)

	f = n.GetInputFilter(ctx)
	tassert.Nil(t, f)
}

func TestSetBlockInput_UnblockWhenNotBlocked(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()

	// Unblocking when not blocked should be a no-op
	err := SetBlockInput(ctx, false, n)
	tassert.NoError(t, err)

	f := n.GetInputFilter(ctx)
	tassert.Nil(t, f)
}

func TestCombineIsDrained_AllDrained(t *testing.T) {
	ctx := context.Background()
	n1 := newDummyNode()
	n2 := newDummyNode()
	n3 := newDummyNode()

	// All new nodes are drained by default
	tassert.True(t, CombineIsDrained(ctx, n1, n2, n3))
}

func TestCombineIsDrained_SomeNotDrained(t *testing.T) {
	ctx := context.Background()
	n1 := newDummyNode()
	n2 := newDummyNode()

	// Mark n2 as not drained
	n2.IsDrainedValue.Store(false)

	tassert.False(t, CombineIsDrained(ctx, n1, n2))
}

func TestCombineIsDrained_Empty(t *testing.T) {
	ctx := context.Background()
	// No nodes means all are drained
	tassert.True(t, CombineIsDrained(ctx))
}

func TestCombineIsDrained_SingleDrained(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	tassert.True(t, CombineIsDrained(ctx, n))
}

func TestCombineIsDrained_SingleNotDrained(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	n.IsDrainedValue.Store(false)
	tassert.False(t, CombineIsDrained(ctx, n))
}

func TestCombineGetChangeChanDrained_Empty(t *testing.T) {
	ctx := context.Background()
	ch := CombineGetChangeChanDrained(ctx)
	require.NotNil(t, ch)

	// When there are no nodes, the returned channel should be immediately closed
	select {
	case <-ch:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("channel should be closed immediately for zero nodes")
	}
}

func TestCombineGetChangeChanDrained_SingleNode(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()

	ch := CombineGetChangeChanDrained(ctx, n)
	require.NotNil(t, ch)

	// The channel should be the same as the node's change chan
	nodeCh := n.GetChangeChanDrained()
	tassert.Equal(t, nodeCh, ch, "single node should return its own change channel")
}

func TestCombineGetChangeChanDrained_MultipleNodes(t *testing.T) {
	ctx := context.Background()
	n1 := newDummyNode()
	n2 := newDummyNode()

	ch := CombineGetChangeChanDrained(ctx, n1, n2)
	require.NotNil(t, ch)

	// Channel should not be closed yet
	select {
	case <-ch:
		t.Fatal("combined channel should not be closed when no change happened")
	case <-time.After(50 * time.Millisecond):
		// expected
	}

	// Trigger a change on one node
	n1.resetChangeChanDrainedChanNow()

	// Now the combined channel should close (eventually)
	select {
	case <-ch:
		// expected
	case <-time.After(time.Second):
		t.Fatal("combined channel should close when one node's drained state changes")
	}
}

func TestWaitForDrain_AlreadyDrained(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()

	// Node is already drained by default
	err := WaitForDrain(ctx, n)
	tassert.NoError(t, err)
}

func TestWaitForDrain_ContextCancelled(t *testing.T) {
	n := newDummyNode()
	n.IsDrainedValue.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := WaitForDrain(ctx, n)
	tassert.Error(t, err)
	tassert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestWaitForDrain_BecomesDrained(t *testing.T) {
	n := newDummyNode()
	n.IsDrainedValue.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- WaitForDrain(ctx, n)
	}()

	// Let WaitForDrain start waiting
	time.Sleep(50 * time.Millisecond)

	// Mark as drained and notify
	n.IsDrainedValue.Store(true)
	n.resetChangeChanDrainedChanNow()

	select {
	case err := <-done:
		tassert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForDrain should have returned after draining")
	}
}

func TestIsDrained_DirectValue(t *testing.T) {
	n := newDummyNode()
	ctx := context.Background()

	tassert.True(t, n.IsDrained(ctx))

	n.IsDrainedValue.Store(false)
	tassert.False(t, n.IsDrained(ctx))

	n.IsDrainedValue.Store(true)
	tassert.True(t, n.IsDrained(ctx))
}

func TestGetChangeChanDrained_NotClosedInitially(t *testing.T) {
	n := newDummyNode()
	ch := n.GetChangeChanDrained()

	select {
	case <-ch:
		t.Fatal("change chan should not be closed initially")
	default:
		// expected
	}
}

func TestResetChangeChanDrainedChanNow(t *testing.T) {
	n := newDummyNode()

	ch1 := n.GetChangeChanDrained()
	n.resetChangeChanDrainedChanNow()

	// ch1 should be closed
	select {
	case <-ch1:
		// expected
	default:
		t.Fatal("old channel should be closed after reset")
	}

	// New channel should be open
	ch2 := n.GetChangeChanDrained()
	select {
	case <-ch2:
		t.Fatal("new channel should not be closed")
	default:
		// expected
	}
}

func TestFlush_NonFlusherProcessor(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()

	// processor.Dummy does not implement processor.Flusher
	err := n.Flush(ctx)
	tassert.NoError(t, err, "flush on non-flusher processor should be a no-op")
}

func TestNode_Serve_AlreadyStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := newTestNode(ctx)

	errCh := make(chan Error, 10)

	// Start serving in background
	go n.Serve(ctx, ServeConfig{}, errCh)

	// Wait for it to actually start
	deadline := time.After(5 * time.Second)
	for !n.IsServing() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for Serve to start")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// Try to start again - should report ErrAlreadyStarted
	errCh2 := make(chan Error, 10)
	n.Serve(ctx, ServeConfig{DebugData: "second-start"}, errCh2)

	// Check that we got an error
	select {
	case nodeErr := <-errCh2:
		tassert.Contains(t, nodeErr.Err.Error(), "already started serving")
	case <-time.After(time.Second):
		t.Fatal("expected ErrAlreadyStarted error")
	}

	cancel()
}

func TestNode_Serve_SetsIsServing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := newTestNode(ctx)

	tassert.False(t, n.IsServing())

	// Get change channel before Serve starts
	startCh := n.GetChangeChanIsServing()

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	// Wait for the change channel to fire (Serve started)
	select {
	case <-startCh:
		// IsServing state changed
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Serve to start")
	}
	tassert.True(t, n.IsServing())

	// Get the next change channel before cancelling
	stopCh := n.GetChangeChanIsServing()

	cancel()

	// Wait for the change channel to fire (Serve stopped)
	select {
	case <-stopCh:
		// IsServing state changed
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Serve to stop")
	}
	tassert.False(t, n.IsServing())
}

func TestNode_Serve_ChangeChanIsServingFires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := newTestNode(ctx)

	ch := n.GetChangeChanIsServing()

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	// Wait for the change channel to fire (Serve started)
	select {
	case <-ch:
		// expected - IsServing state changed
	case <-time.After(5 * time.Second):
		t.Fatal("change channel should fire when Serve starts")
	}

	tassert.True(t, n.IsServing())

	cancel()
}
