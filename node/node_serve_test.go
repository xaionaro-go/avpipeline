package node

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func TestIncrementReceived_IsPushed(t *testing.T) {
	counters := types.NewCounters()
	isPushed := true

	incrementReceived(
		counters,
		&isPushed,
		globaltypes.CountersSubSectionIDPackets,
		globaltypes.MediaType(0),
		100,
	)

	received := counters.Received.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(0)).Count.Load()
	tassert.Equal(t, uint64(1), received, "received counter should be incremented when isPushed is true")

	missed := counters.Missed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(0)).Count.Load()
	tassert.Equal(t, uint64(0), missed, "missed counter should not be incremented when isPushed is true")
}

func TestIncrementReceived_NotPushed(t *testing.T) {
	counters := types.NewCounters()
	isPushed := false

	incrementReceived(
		counters,
		&isPushed,
		globaltypes.CountersSubSectionIDPackets,
		globaltypes.MediaType(0),
		100,
	)

	received := counters.Received.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(0)).Count.Load()
	tassert.Equal(t, uint64(0), received, "received counter should not be incremented when isPushed is false")

	missed := counters.Missed.Get(globaltypes.CountersSubSectionIDPackets).Get(globaltypes.MediaType(0)).Count.Load()
	tassert.Equal(t, uint64(1), missed, "missed counter should be incremented when isPushed is false")
}

func TestNode_Serve_SendsEOFOnClosedOutputChan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := newTestNode(ctx)

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	// Wait for it to start serving
	deadline := time.After(5 * time.Second)
	for !n.IsServing(ctx) {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for Serve to start")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// Close the processor to trigger EOF on the output channel
	err := n.Processor.Close(ctx)
	require.NoError(t, err)

	// Should receive an EOF or context.Canceled error (under -race,
	// goroutine scheduling may cause the context cancellation path to
	// fire before the output channel closure is observed).
	select {
	case nodeErr := <-errCh:
		tassert.True(t, errors.Is(nodeErr.Err, io.EOF) || errors.Is(nodeErr.Err, context.Canceled),
			"expected EOF or context.Canceled, got: %v", nodeErr.Err)
	case <-time.After(5 * time.Second):
		t.Fatal("expected EOF error after processor close")
	}

	cancel()
}

// TestNode_Serve_EOFErrorContainsProcessorIdentity verifies the cascade-EOF
// identity wrap — when a processor's output channel closes (the textbook
// cause of cascade-EOF wedges), the error reported on errCh must be
// annotated with n.Processor.String() so log readers can see which
// processor in the chain triggered the cascade. Without identity, multiple
// FromKernel[...] processors all reported a bare io.EOF, making it
// impossible to distinguish camera vs mic vs barrier vs decoder cascades
// in production logs.
//
// Determinism: the parent test ctx stays alive for the entire wait
// — Processor.Close is the ONLY trigger. The Serve loop's
// procNodeEndCtx.Done() branch can therefore not fire before the
// outputCh-closed branch, so the EOF wrap path is the deterministic
// outcome.
//
// Falsifier: revert node_serve.go's sendErr(io.EOF) wrap back to bare
// io.EOF — this test must fail (Contains check on processor name).
func TestNode_Serve_EOFErrorContainsProcessorIdentity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const procName = "test-cascade-source-kernel"
	n := NewFromKernel[*testKernel](ctx, &testKernel{stringValue: procName})

	errCh := make(chan Error, 10)
	go n.Serve(ctx, ServeConfig{}, errCh)

	deadline := time.After(5 * time.Second)
	for !n.IsServing(ctx) {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for Serve to start")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// Close the processor WITHOUT cancelling the outer ctx. This makes
	// the OutputCh-closed branch the deterministic trigger; the Serve
	// loop's procNodeEndCtx.Done() branch stays blocked because the
	// parent ctx stays alive.
	require.NoError(t, n.Processor.Close(ctx))

	select {
	case nodeErr := <-errCh:
		// EOF path is the only deterministic outcome here. If we ever
		// observe a different error, the falsifier guarantee is gone
		// and the test must fail loudly rather than silently pass.
		if !errors.Is(nodeErr.Err, io.EOF) {
			t.Fatalf("EOF path didn't fire — falsifier not exercised; got: %v", nodeErr.Err)
		}
		tassert.Contains(t, nodeErr.Err.Error(), procName,
			"EOF error must include processor identity; got: %v", nodeErr.Err)
		tassert.Contains(t, nodeErr.Err.Error(), "output channel closed",
			"EOF error must explain cause; got: %v", nodeErr.Err)
	case <-time.After(5 * time.Second):
		t.Fatal("expected error after processor close")
	}
}

func TestNode_Serve_NilErrCh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := newTestNode(ctx)

	// Serve with nil errCh should not panic
	go n.Serve(ctx, ServeConfig{}, nil)

	deadline := time.After(5 * time.Second)
	for !n.IsServing(ctx) {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for Serve to start")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	tassert.True(t, n.IsServing(ctx))
	cancel()
}

func TestNode_Serve_DebugDataPreserved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := newTestNode(ctx)

	errCh := make(chan Error, 10)
	debugData := "test-debug-data-123"
	go n.Serve(ctx, ServeConfig{DebugData: debugData}, errCh)

	// Wait for it to start
	deadline := time.After(5 * time.Second)
	for !n.IsServing(ctx) {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for Serve to start")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// Try to start a second time, which should fail with ErrAlreadyStarted
	// that contains the first Serve's debug data
	errCh2 := make(chan Error, 10)
	n.Serve(ctx, ServeConfig{DebugData: "second"}, errCh2)

	select {
	case nodeErr := <-errCh2:
		var alreadyStarted ErrAlreadyStarted
		if tassert.ErrorAs(t, nodeErr.Err, &alreadyStarted) {
			tassert.Equal(t, debugData, alreadyStarted.PreviousDebugData)
		}
	case <-time.After(time.Second):
		t.Fatal("expected ErrAlreadyStarted error")
	}

	cancel()
}

func TestDotBlockContentStringWriteTo_NilNode(t *testing.T) {
	var n *Dummy
	// Calling dotBlockContentStringWriteTo on nil should not panic
	alreadyPrinted := map[processor.Abstract]struct{}{}
	n.dotBlockContentStringWriteTo(io.Discard, alreadyPrinted)
}

func TestDotBlockContentStringWriteTo_NilPushToNode(t *testing.T) {
	n := newDummyNode()

	// Add a push to with nil node
	n.PushTos = append(n.PushTos, PushTo{Node: nil})

	alreadyPrinted := map[processor.Abstract]struct{}{}
	// Should not panic
	n.DotBlockContentStringWriteTo(io.Discard, alreadyPrinted)
}

func TestDotBlockContentStringWriteTo_WithCondition(t *testing.T) {
	n := newDummyNode()
	dst := newDummyNode()

	cond := packetorframefiltercondition.Static(true)
	n.PushTos = append(n.PushTos, PushTo{
		Node:      dst,
		Condition: cond,
	})

	var buf testWriter
	alreadyPrinted := map[processor.Abstract]struct{}{}
	n.DotBlockContentStringWriteTo(&buf, alreadyPrinted)

	s := buf.String()
	tassert.Contains(t, s, "->")
	tassert.Contains(t, s, "Dummy")
}

func TestDotBlockContentStringWriteTo_AlreadyPrinted(t *testing.T) {
	n := newDummyNode()

	// Mark as already printed
	alreadyPrinted := map[processor.Abstract]struct{}{
		n.Processor: {},
	}

	var buf testWriter
	n.DotBlockContentStringWriteTo(&buf, alreadyPrinted)

	// Should not print the node label again (only connections if any)
	s := buf.String()
	tassert.NotContains(t, s, "label=")
}

// testWriter is a simple io.Writer that collects written bytes.
type testWriter struct {
	buf []byte
}

func (w *testWriter) Write(p []byte) (n int, err error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func (w *testWriter) String() string {
	return string(w.buf)
}
